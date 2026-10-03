package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/safedep/gryph/platform/localauth"
	"github.com/safedep/gryph/selfprotect"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/hookside"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/platform/service"
	"github.com/safedep/gryph/spool"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// managedInstallReport is the JSON contract of gryph install --managed. An
// MDM script reads it.
type managedInstallReport struct {
	Status string `json:"status"`
	Config string `json:"config"`
	Policy string `json:"policy,omitempty"`
	// TrustStore is the managed receipt trust store, when --trust-store
	// gave one.
	TrustStore string `json:"trust_store,omitempty"`
	Binary     string `json:"binary"`
	// Changed is true when any file changed, or would change in a dry run.
	Changed bool                 `json:"changed"`
	Agents  []managedAgentReport `json:"agents"`
	// Timer is the system-wide job that runs the reconcile pass as each
	// account. Absent in a dry run.
	Timer *managedTimerReport `json:"timer,omitempty"`
	// Keys are the machine keys of the decision service. Present when the
	// managed configuration turns the service on, absent in a dry run.
	Keys *managedKeysReport `json:"keys,omitempty"`
	// Service is the decision service: its account, its units and the
	// switch. Present when the managed configuration turns the service on,
	// absent in a dry run.
	Service *managedServiceReport `json:"service,omitempty"`
	// Fanotify is the kernel watcher: its unit and whether it reported.
	// Present when the managed configuration turns it on, absent in a dry
	// run.
	Fanotify *managedServiceReport `json:"fanotify,omitempty"`
}

// managedServiceReport is the outcome of the decision service install:
// the units, whether the service manager took them, whether the service
// answered, and the switch the configuration ends with.
type managedServiceReport struct {
	Account string   `json:"account"`
	Units   []string `json:"units"`
	Changed bool     `json:"changed"`
	// Enabled is true when the service manager took the units. Otherwise
	// Next names the command to finish by hand.
	Enabled bool   `json:"enabled"`
	Next    string `json:"next,omitempty"`
	// Running is true when the service answered the health check over
	// the socket. Switch is on only then: a hook never takes the service
	// fallback before the service answers.
	Running bool   `json:"running"`
	Switch  string `json:"switch"`
	// AuthPolicy is the file that declares the approval action to the
	// authority of the platform, when the platform has one.
	AuthPolicy string `json:"auth_policy,omitempty"`
	Error      string `json:"error,omitempty"`
}

const (
	managedSwitchOn  = "on"
	managedSwitchOff = "off"
	// serviceHealthTimeout bounds the wait for the first welcome of the
	// service after its units start.
	serviceHealthTimeout = 10 * time.Second
	// supervisorServiceName is the unit name of the decision service, and
	// fanotifyServiceName the unit name of the kernel watcher.
	supervisorServiceName = "gryph-supervisor"
	fanotifyServiceName   = "gryph-fanotify"
)

// managedKeysReport is the outcome of the machine keys of the decision
// service: the receipt signing key under the state directory, its public
// half in the managed trust store, and the export key.
type managedKeysReport struct {
	ReceiptKey string `json:"receipt_key"`
	KeyID      string `json:"key_id,omitempty"`
	ExportKey  string `json:"export_key"`
	TrustStore string `json:"trust_store"`
	// Owner is the account that owns the keys: the service account when
	// it exists on the host, else root until it does.
	Owner   string `json:"owner,omitempty"`
	Changed bool   `json:"changed"`
	Error   string `json:"error,omitempty"`
}

// managedTimerReport is the outcome of the system-wide reconcile job.
type managedTimerReport struct {
	Paths   []string `json:"paths"`
	Changed bool     `json:"changed"`
	// Enabled is true when the scheduler took the job. Otherwise Next
	// names the command to finish by hand.
	Enabled bool   `json:"enabled"`
	Next    string `json:"next,omitempty"`
	Error   string `json:"error,omitempty"`
}

func timerReport(v *tui.RepairTimerView) *managedTimerReport {
	r := &managedTimerReport{Paths: v.Paths, Changed: v.Changed, Enabled: v.Enabled, Next: v.Next, Error: v.Error}
	if r.Paths == nil {
		r.Paths = []string{}
	}
	return r
}

type managedAgentReport struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Class says how far the managed file resists the user: locked or
	// system_path.
	Class string `json:"class"`
	// Action is install for an agent in the allowlist and remove for one
	// that left it.
	Action  string `json:"action"`
	Changed bool   `json:"changed"`
	Locked  bool   `json:"locked"`
	Error   string `json:"error,omitempty"`
}

const (
	managedStatusOK      = "ok"
	managedStatusPartial = "partial"
	managedStatusDryRun  = "dry-run"
)

// managedInstallInput is the validated input of a managed install. Nothing
// is written before it is complete, so an invalid input changes nothing.
type managedInstallInput struct {
	configData     []byte
	cfg            *config.Config
	policyData     []byte
	trustStoreData []byte
	binary         string
	installers     map[string]agent.ManagedInstaller
}

// managedInstallArgs are the flags of gryph install --managed.
type managedInstallArgs struct {
	configPath     string
	policyPath     string
	trustStorePath string
	dryRun         bool
	asJSON         bool
}

// runManagedInstall is gryph install --managed: the command an MDM script
// runs as root on every host. It validates the whole input first and exits
// 3 with nothing written when it is invalid. It then writes the managed
// configuration, the managed policy, and the managed hook entry of every
// agent in the allowlist, and removes the entry of every agent that left
// it. It reads no environment variable and runs no program: every path
// comes from the input and the platform.
func runManagedInstall(cmd *cobra.Command, args managedInstallArgs) error {
	configPath, policyPath, dryRun, asJSON := args.configPath, args.policyPath, args.dryRun, args.asJSON
	if !utils.IsPrivileged() {
		return NewCLIError(ExitGeneral, "install --managed needs an administrator: root on Linux and macOS, an elevated prompt on Windows")
	}
	if config.ManagedConfigPath() == "" {
		return NewCLIError(ExitGeneral, "this platform has no system managed location")
	}
	if configPath == "" {
		return NewCLIError(ExitManagedInvalidConfig, "install --managed needs --config <file>")
	}

	in, err := readManagedInput(configPath, policyPath, args.trustStorePath)
	if err != nil {
		return WrapError(ExitManagedInvalidConfig, "invalid managed input, nothing changed", err)
	}

	report := &managedInstallReport{Status: managedStatusOK, Config: config.ManagedConfigPath(), Binary: in.binary}
	if !dryRun {
		// With the service on, the configuration goes in with the switch
		// off first. The hooks keep deciding in process until the service
		// answers, and the switch turns on at the end of the run.
		configData := in.configData
		if in.cfg.Supervisor.Enabled {
			configData, err = config.SetSupervisorEnabled(in.configData, false)
			if err != nil {
				return WrapError(ExitGeneral, "write the managed configuration", err)
			}
		}
		changed, err := config.WriteManagedFile(report.Config, configData)
		if err != nil {
			return WrapError(ExitGeneral, "write the managed configuration", err)
		}
		report.Changed = report.Changed || changed
		if in.policyData != nil {
			report.Policy = config.ManagedPolicyState().File
			changed, err := config.WriteManagedFile(report.Policy, in.policyData)
			if err != nil {
				return WrapError(ExitGeneral, "write the managed policy", err)
			}
			report.Changed = report.Changed || changed
		}
		if in.trustStoreData != nil {
			report.TrustStore = config.ManagedTrustStorePath()
			changed, err := config.WriteManagedFile(report.TrustStore, in.trustStoreData)
			if err != nil {
				return WrapError(ExitGeneral, "write the managed trust store", err)
			}
			report.Changed = report.Changed || changed
		}
	} else {
		changed, err := config.ManagedFileChanged(report.Config, in.configData)
		if err != nil {
			return WrapError(ExitGeneral, "read the managed configuration", err)
		}
		report.Changed = report.Changed || changed
		if in.policyData != nil {
			report.Policy = config.ManagedPolicyState().File
			changed, err := config.ManagedFileChanged(report.Policy, in.policyData)
			if err != nil {
				return WrapError(ExitGeneral, "read the managed policy", err)
			}
			report.Changed = report.Changed || changed
		}
		if in.trustStoreData != nil {
			report.TrustStore = config.ManagedTrustStorePath()
			changed, err := config.ManagedFileChanged(report.TrustStore, in.trustStoreData)
			if err != nil {
				return WrapError(ExitGeneral, "read the managed trust store", err)
			}
			report.Changed = report.Changed || changed
		}
	}

	ctx := utils.WithoutProgramExecution(context.Background())
	degraded := 0
	for _, name := range sortedKeys(in.installers) {
		row := applyManagedAgent(ctx, name, in.installers[name], in, dryRun)
		if row.Error != "" {
			degraded++
		}
		report.Changed = report.Changed || row.Changed
		report.Agents = append(report.Agents, row)
	}
	if !dryRun {
		report.Timer = timerReport(installSystemRepairTimer(ctx, in.binary))
		if report.Timer.Error != "" {
			degraded++
		}
		report.Changed = report.Changed || report.Timer.Changed
		if in.cfg.Supervisor.Enabled {
			report.Service = &managedServiceReport{Account: in.cfg.Supervisor.ServerAccount(), Units: []string{}, Switch: managedSwitchOff}
			if err := ensureServiceAccount(ctx, in.cfg); err != nil {
				report.Service.Error = err.Error()
			}
			report.Keys = installMachineKeys(in.cfg)
			if report.Keys.Error != "" {
				degraded++
			}
			report.Changed = report.Changed || report.Keys.Changed
			installSupervisorService(ctx, in, report)
			if report.Service.Error != "" {
				degraded++
			}
			report.Changed = report.Changed || report.Service.Changed
			if in.cfg.Supervisor.Fanotify.Enabled {
				report.Fanotify = installFanotifyService(ctx, in)
				if report.Fanotify.Error != "" {
					degraded++
				}
				report.Changed = report.Changed || report.Fanotify.Changed
			}
		}
	}

	switch {
	case dryRun:
		report.Status = managedStatusDryRun
	case degraded > 0:
		report.Status = managedStatusPartial
	}

	if err := renderManagedReport(cmd.OutOrStdout(), report, asJSON); err != nil {
		return err
	}
	if degraded > 0 {
		return &exitError{code: ExitManagedPartial, message: fmt.Sprintf("install --managed: %d agent(s) degraded", degraded)}
	}
	return nil
}

// installMachineKeys makes the machine keys of the decision service when
// they are missing, hands them to the service account, and puts the public
// half of the receipt key in the managed trust store. The service account
// is the owner of the state directory when it exists, else the account
// named by the configuration when the host has it, else root: the next
// run hands the keys over once the account exists.
func installMachineKeys(cfg *config.Config) *managedKeysReport {
	sup := cfg.Supervisor
	report := &managedKeysReport{ReceiptKey: sup.ReceiptKeyPath(), ExportKey: sup.ExportKeyPath(), TrustStore: config.ManagedTrustStorePath()}
	if err := os.MkdirAll(sup.StatePath(), 0o750); err != nil {
		report.Error = err.Error()
		return report
	}
	keys, err := engine.EnsureMachineKeys(cfg)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.KeyID = keys.KeyID
	report.Changed = keys.Created
	owner, err := handStateToServiceAccount(cfg)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.Owner = owner
	_, changed, err := engine.TrustMachineKey(cfg, config.ManagedTrustStorePath())
	if err != nil {
		report.Error = "trust store: " + err.Error()
		return report
	}
	report.Changed = report.Changed || changed
	return report
}

// handStateToServiceAccount gives the state directory and the keys to the
// service account. The owner of an existing state directory stays, so a
// host that runs the service under another account keeps it.
func handStateToServiceAccount(cfg *config.Config) (string, error) {
	info, err := os.Stat(cfg.Supervisor.StatePath())
	if err != nil {
		return "", err
	}
	uid, _, ok := ownerOf(info)
	if ok && uid != 0 {
		return handMachineKeysToServiceAccount(cfg)
	}
	acct, err := account.Lookup(cfg.Supervisor.ServerAccount())
	if err != nil {
		return "root", nil
	}
	if err := acct.Chown(cfg.Supervisor.StatePath()); err != nil {
		return "", fmt.Errorf("hand %s to %s: %w", cfg.Supervisor.StatePath(), acct.Name, err)
	}
	return handMachineKeysToServiceAccount(cfg)
}

// ensureServiceAccount makes the service account when the host does not
// have it. The package of the platform makes it too, so a host that got
// the package has it already.
func ensureServiceAccount(ctx context.Context, cfg *config.Config) error {
	name := cfg.Supervisor.ServerAccount()
	if _, err := account.Lookup(name); err == nil {
		return nil
	}
	if err := account.CreateSystem(ctx, name, cfg.Supervisor.StatePath()); err != nil {
		return fmt.Errorf("account %s: %w", name, err)
	}
	return nil
}

// installSupervisorService makes the spool root, writes the units, starts
// the socket, waits for the service to answer, and only then writes the
// configuration with the switch on. A service that does not answer leaves
// the switch off and degrades the install, so a rollout cannot block the
// agents of a host by mistake.
func installSupervisorService(ctx context.Context, in *managedInstallInput, report *managedInstallReport) {
	sup := in.cfg.Supervisor
	svc := report.Service
	if err := spool.EnsureRoot(sup.SpoolPath()); err != nil {
		svc.Error = err.Error()
		return
	}
	if acct, err := account.Lookup(sup.ServerAccount()); err == nil {
		if err := acct.Chown(sup.SpoolPath()); err != nil {
			svc.Error = fmt.Sprintf("hand %s to %s: %v", sup.SpoolPath(), acct.Name, err)
			return
		}
	}
	if path, content := localauth.PolicyFile(); path != "" {
		changed, err := writeRootFile(path, content)
		if err != nil {
			svc.Error = "write the authentication policy: " + err.Error()
			return
		}
		svc.AuthPolicy = path
		svc.Changed = svc.Changed || changed
	}
	res, err := service.Install(ctx, service.Spec{
		Name:        supervisorServiceName,
		Description: "Gryph decision service",
		Command:     []string{in.binary, "supervisor", "run"},
		Socket:      sup.SocketPath(),
		User:        sup.ServerAccount(),
		StateDir:    sup.StatePath(),
		SpoolDir:    sup.SpoolPath(),
	})
	if err != nil {
		svc.Error = err.Error()
		return
	}
	svc.Units = res.Paths
	svc.Changed = svc.Changed || res.Changed
	svc.Enabled = res.Enabled
	svc.Next = res.Next
	if err := waitForService(ctx, in.cfg); err != nil {
		svc.Error = "the service did not answer: " + err.Error() + ". The switch stays off until a later run finds it running"
		return
	}
	svc.Running = true
	changed, err := config.WriteManagedFile(report.Config, in.configData)
	if err != nil {
		svc.Error = "write the managed configuration with the switch on: " + err.Error()
		return
	}
	report.Changed = report.Changed || changed
	svc.Switch = managedSwitchOn
	// The service read its configuration at start and a partition its
	// policy at open, and a unit that did not change restarts nothing. A
	// reload makes a running service read both again.
	if report.Changed {
		if _, reloaded, note := reloadService(sup.PIDFile()); !reloaded {
			svc.Next = "systemctl reload " + supervisorServiceName + ".service (" + note + ")"
		}
	}
}

// installFanotifyService writes the unit of the kernel watcher and starts
// it. The watcher runs as the service account with the two capabilities
// the fanotify API needs, and reports in the runtime directory next to
// the socket. A watcher that does not report within the wait degrades the
// install and names the next step.
func installFanotifyService(ctx context.Context, in *managedInstallInput) *managedServiceReport {
	sup := in.cfg.Supervisor
	svc := &managedServiceReport{Account: sup.ServerAccount(), Units: []string{}, Switch: managedSwitchOn}
	res, err := service.Install(ctx, service.Spec{
		Name:         fanotifyServiceName,
		Description:  "Gryph kernel watcher",
		Command:      []string{in.binary, "supervisor", "fanotify"},
		User:         sup.ServerAccount(),
		StateDir:     sup.StatePath(),
		Capabilities: []string{"CAP_SYS_ADMIN", "CAP_SYS_PTRACE"},
		RuntimeDir:   strings.TrimPrefix(filepath.Dir(sup.FanotifyStatePath()), "/run/"),
		SyscallAllow: []string{"fanotify_init", "fanotify_mark", "name_to_handle_at"},
	})
	if err != nil {
		svc.Error = err.Error()
		return svc
	}
	svc.Units = res.Paths
	svc.Changed = res.Changed
	svc.Enabled = res.Enabled
	svc.Next = res.Next
	if !res.Enabled {
		return svc
	}
	deadline := time.Now().Add(serviceHealthTimeout)
	for time.Now().Before(deadline) {
		if state, err := selfprotect.ReadFanotifyState(sup.FanotifyStatePath()); err == nil && state.PID > 0 {
			svc.Running = true
			return svc
		}
		time.Sleep(100 * time.Millisecond)
	}
	svc.Error = "the watcher did not report within " + serviceHealthTimeout.String() + ": see journalctl -u " + fanotifyServiceName
	return svc
}

// writeRootFile writes a root-owned file readable by every account, and
// reports whether the content changed. The parent directory must exist:
// the platform package of the authority owns it.
func writeRootFile(path string, content []byte) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, content) {
		return false, nil
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// waitForService connects to the socket of the service and waits for its
// welcome, with the identity check of a hook, until the health timeout.
func waitForService(ctx context.Context, cfg *config.Config) error {
	socket := cfg.Supervisor.SocketPath()
	deadline := time.Now().Add(serviceHealthTimeout)
	var last error
	for time.Now().Before(deadline) {
		attempt, cancel := context.WithTimeout(ctx, time.Second)
		client, err := ipc.Dial(attempt, socket, ipc.DialOptions{Version: version.Version, VerifyServer: func(conn net.Conn) error {
			return hookside.VerifyServer(conn, socket, cfg.Supervisor.ServerAccount())
		}})
		cancel()
		if err == nil {
			_ = client.Close()
			return nil
		}
		if errors.Is(err, ipc.ErrServerIdentity) {
			return err
		}
		last = err
		time.Sleep(200 * time.Millisecond)
	}
	return last
}

// readManagedInput reads and validates every input file. Each file must
// pass the trust check of the managed configuration.
func readManagedInput(configPath, policyPath, trustStorePath string) (*managedInstallInput, error) {
	configData, err := config.ReadTrustedFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	cfg, err := config.Parse(configData)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	in := &managedInstallInput{configData: configData, cfg: cfg, installers: map[string]agent.ManagedInstaller{}}
	registry := agent.NewRegistry()
	engine.RegisterAdapters(registry, nil, config.Default())
	for _, a := range registry.All() {
		if m, ok := a.(agent.ManagedInstaller); ok {
			in.installers[a.Name()] = m
		}
	}
	for _, name := range cfg.Managed.Agents {
		if _, ok := in.installers[name]; !ok {
			return nil, fmt.Errorf("managed.agents names %s, which has no managed hook entry", name)
		}
	}
	for _, name := range cfg.Managed.LockHooks {
		if in.installers[name].ManagedClass() != agent.ManagedClassLocked {
			return nil, fmt.Errorf("managed.lock_hooks names %s, which has no lock switch", name)
		}
	}

	if policyPath != "" {
		in.policyData, err = config.ReadTrustedFile(policyPath)
		if err != nil {
			return nil, fmt.Errorf("policy: %w", err)
		}
		if _, err := pdp.ParsePolicy(in.policyData); err != nil {
			return nil, fmt.Errorf("policy: %w", err)
		}
	}

	if trustStorePath != "" {
		in.trustStoreData, err = config.ReadTrustedFile(trustStorePath)
		if err != nil {
			return nil, fmt.Errorf("trust store: %w", err)
		}
		ts, err := receipt.ParseTrustStore(in.trustStoreData)
		if err != nil {
			return nil, fmt.Errorf("trust store: %w", err)
		}
		for _, entry := range ts.Keys {
			if _, err := receipt.ValidateTrustEntry(entry); err != nil {
				return nil, fmt.Errorf("trust store: key %s: %w", entry.KeyID, err)
			}
		}
	}

	in.binary = config.ManagedBinaryPath(cfg)
	info, err := os.Stat(in.binary)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("binary %s does not exist. Place the root-owned gryph binary there or set managed.binary", in.binary)
	case err != nil:
		return nil, fmt.Errorf("binary %s: %w", in.binary, err)
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("binary %s is not a regular file", in.binary)
	}
	if err := config.VerifyManagedPath(in.binary); err != nil {
		return nil, fmt.Errorf("binary %s: %w. A hook entry must name a binary that only root can replace", in.binary, err)
	}
	return in, nil
}

// applyManagedAgent installs or removes the managed entry of one agent. A
// failure degrades that agent only.
func applyManagedAgent(ctx context.Context, name string, installer agent.ManagedInstaller, in *managedInstallInput, dryRun bool) managedAgentReport {
	row := managedAgentReport{Name: name, Path: installer.ManagedHookPath(), Class: string(installer.ManagedClass()), Action: "remove"}
	opts := agent.ManagedInstallOptions{Command: in.binary, DryRun: dryRun}
	var (
		res *agent.ManagedInstallResult
		err error
	)
	if contains(in.cfg.Managed.Agents, name) {
		row.Action = "install"
		opts.Lock = in.cfg.Managed.Locked(name)
		res, err = installer.InstallManaged(ctx, opts)
	} else {
		res, err = installer.UninstallManaged(ctx, opts)
	}
	if err != nil {
		row.Error = err.Error()
		return row
	}
	row.Changed = res.Changed
	row.Locked = res.Locked
	return row
}

func renderManagedReport(w io.Writer, report *managedInstallReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if _, err := fmt.Fprintf(w, "Managed install: %s\n  %-8s %s\n  %-8s %s\n", report.Status, "Config", report.Config, "Binary", report.Binary); err != nil {
		return err
	}
	if report.Policy != "" {
		if _, err := fmt.Fprintf(w, "  %-8s %s\n", "Policy", report.Policy); err != nil {
			return err
		}
	}
	if report.TrustStore != "" {
		if _, err := fmt.Fprintf(w, "  %-8s %s\n", "Keys", report.TrustStore); err != nil {
			return err
		}
	}
	for _, a := range report.Agents {
		state := "unchanged"
		if a.Changed {
			state = "changed"
		}
		line := fmt.Sprintf("  %-12s %-11s %-8s %-9s %s", a.Name, a.Class, a.Action, state, a.Path)
		if a.Locked {
			line += "  (locked)"
		}
		if a.Error != "" {
			line += "  error: " + a.Error
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if report.Timer != nil {
		if err := renderTimerLines(w, "Reconcile job", report.Timer); err != nil {
			return err
		}
	}
	if svc := report.Service; svc != nil {
		line := fmt.Sprintf("  %-8s account %s  switch %s", "Service", svc.Account, svc.Switch)
		switch {
		case svc.Error != "":
			line += "  error: " + svc.Error
		case svc.Running:
			line += "  running"
		case svc.Next != "":
			line += "  next: " + svc.Next
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if k := report.Keys; k != nil {
		line := fmt.Sprintf("  %-8s %s  key %s  owner %s  trust store %s", "Keys", k.ReceiptKey, k.KeyID, k.Owner, k.TrustStore)
		if k.Error != "" {
			line = fmt.Sprintf("  %-8s %s  error: %s", "Keys", k.ReceiptKey, k.Error)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// renderTimerLines prints the system-wide reconcile job: its files, and
// the command to finish by hand when the scheduler did not take it.
func renderTimerLines(w io.Writer, label string, t *managedTimerReport) error {
	state := "enabled for every account"
	switch {
	case t.Error != "":
		state = "error: " + t.Error
	case !t.Enabled:
		state = "files written, run by hand: " + t.Next
	}
	if _, err := fmt.Fprintf(w, "  %-12s %s\n", label, state); err != nil {
		return err
	}
	for _, p := range t.Paths {
		if _, err := fmt.Fprintf(w, "  %-12s %s\n", "", p); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
