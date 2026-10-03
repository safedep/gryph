package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/spf13/cobra"
)

// managedInstallReport is the JSON contract of gryph install --managed. An
// MDM script reads it.
type managedInstallReport struct {
	Status string `json:"status"`
	Config string `json:"config"`
	Policy string `json:"policy,omitempty"`
	Binary string `json:"binary"`
	// Changed is true when any file changed, or would change in a dry run.
	Changed bool                 `json:"changed"`
	Agents  []managedAgentReport `json:"agents"`
}

type managedAgentReport struct {
	Name string `json:"name"`
	Path string `json:"path"`
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
	configData []byte
	cfg        *config.Config
	policyData []byte
	binary     string
	installers map[string]agent.ManagedInstaller
}

// runManagedInstall is gryph install --managed: the command an MDM script
// runs as root on every host. It validates the whole input first and exits
// 3 with nothing written when it is invalid. It then writes the managed
// configuration, the managed policy, and the managed hook entry of every
// agent in the allowlist, and removes the entry of every agent that left
// it. It reads no environment variable and runs no program: every path
// comes from the input and the platform.
func runManagedInstall(cmd *cobra.Command, configPath, policyPath string, dryRun, asJSON bool) error {
	if !utils.IsPrivileged() {
		return NewCLIError(ExitGeneral, "install --managed must run as root")
	}
	if config.ManagedConfigPath() == "" {
		return NewCLIError(ExitGeneral, "this platform has no system managed location")
	}
	if configPath == "" {
		return NewCLIError(ExitManagedInvalidConfig, "install --managed needs --config <file>")
	}

	in, err := readManagedInput(configPath, policyPath)
	if err != nil {
		return WrapError(ExitManagedInvalidConfig, "invalid managed input, nothing changed", err)
	}

	report := &managedInstallReport{Status: managedStatusOK, Config: config.ManagedConfigPath(), Binary: in.binary}
	if !dryRun {
		changed, err := config.WriteManagedFile(report.Config, in.configData)
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
	} else if in.policyData != nil {
		report.Policy = config.ManagedPolicyState().File
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

// readManagedInput reads and validates every input file. Each file must
// pass the trust check of the managed configuration.
func readManagedInput(configPath, policyPath string) (*managedInstallInput, error) {
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

	if policyPath != "" {
		in.policyData, err = config.ReadTrustedFile(policyPath)
		if err != nil {
			return nil, fmt.Errorf("policy: %w", err)
		}
		if _, err := pdp.ParsePolicy(in.policyData); err != nil {
			return nil, fmt.Errorf("policy: %w", err)
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
	row := managedAgentReport{Name: name, Path: installer.ManagedHookPath(), Action: "remove"}
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
	for _, a := range report.Agents {
		state := "unchanged"
		if a.Changed {
			state = "changed"
		}
		line := fmt.Sprintf("  %-12s %-8s %-9s %s", a.Name, a.Action, state, a.Path)
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
