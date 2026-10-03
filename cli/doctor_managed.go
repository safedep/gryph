package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/selfprotect"
)

// managedDoctorSchemaVersion is the version of the JSON contract of gryph
// doctor --managed. An MDM tool reads the report as a compliance
// attribute, so a field never changes meaning. A new field raises nothing.
// A removed or renamed field raises the version.
const managedDoctorSchemaVersion = 1

// The fixed facts of this phase. The supervisor and the off-host
// collection do not exist yet, and the receipt key stays in the user's
// home.
const (
	managedKeyScopeUser          = "user"
	managedKeyScopeSupervisor    = "supervisor"
	managedKeySupervisorSummary  = "Key: supervisor-owned (protected)"
	managedKeySupervisorExposed  = "Key: supervisor key readable by others (not protected)"
	managedSupervisorAbsent      = "absent"
	managedCollectionNone        = "none"
	managedLockedSummary         = "Locked (hook entry and policy files, decision and audit trail not protected)"
	managedLockedServiceSummary  = "Locked (hook entry and policy files, decision and audit trail in the decision service)"
	managedKeySummary            = "Key: user-owned (not protected)"
	managedChainOK               = "ok"
	managedChainMissing          = "missing"
	managedChainUntrusted        = "untrusted"
	managedChainPresentUntrusted = "present, untrusted"
	managedChainAbsent           = "absent"
)

// managedDoctorReport is the JSON contract of gryph doctor --managed.
type managedDoctorReport struct {
	SchemaVersion int `json:"schema_version"`
	// Profile is locked when every part of a managed install is in place,
	// none otherwise. Issues says what is missing.
	Profile string   `json:"profile"`
	Summary string   `json:"summary"`
	Issues  []string `json:"issues"`

	Config managedFileReport   `json:"config"`
	Policy managedPolicyReport `json:"policy"`
	// TrustStore is the managed receipt trust store. It is optional: a
	// missing one does not lower the profile.
	TrustStore managedTrustStoreReport `json:"trust_store"`
	Binary     managedFileReport       `json:"binary"`
	Agents     []managedAgentState     `json:"agents"`

	Key        managedKeyReport        `json:"key"`
	Supervisor managedSupervisorReport `json:"supervisor"`
	Collection managedCollectionReport `json:"collection"`
}

// managedFileReport is the state of one root-owned file: present, and
// trusted along its whole path chain.
type managedFileReport struct {
	Path string `json:"path"`
	// Chain is ok, missing, or untrusted.
	Chain string `json:"chain"`
	Error string `json:"error,omitempty"`
}

type managedPolicyReport struct {
	managedFileReport
	// Version is the version field of the policy file.
	Version string `json:"version,omitempty"`
	// SHA256 is the digest of the policy file, so a fleet tool can compare
	// hosts against the policy it shipped.
	SHA256 string `json:"sha256,omitempty"`
	// AllowUserPolicy is the managed configuration's policy.allow_user_policy.
	AllowUserPolicy bool `json:"allow_user_policy"`
}

// managedAgentState is the managed entry of one agent in the allowlist.
type managedTrustStoreReport struct {
	managedFileReport
	// Keys is the count of public keys in the store.
	Keys int `json:"keys"`
}

type managedAgentState struct {
	Name  string `json:"name"`
	Class string `json:"class"`
	Path  string `json:"path"`
	// Level is the self-protection level the managed entry earns:
	// prevent_same_user for a locked entry that matches, detect for a
	// system path entry that matches, none otherwise.
	Level  string `json:"level"`
	Match  bool   `json:"match"`
	Locked bool   `json:"locked"`
	Error  string `json:"error,omitempty"`
}

type managedKeyReport struct {
	// Scope is user while the hook signs with a key in the user's home,
	// and supervisor once the decision service signs with the machine
	// key. Protected is true when only a system account reads that key.
	Scope     string `json:"scope"`
	Protected bool   `json:"protected"`
	Path      string `json:"path,omitempty"`
	Owner     string `json:"owner,omitempty"`
}

type managedSupervisorReport struct {
	State string `json:"state"`
	// Profile is the profile in force: enforce or pilot. PilotUntil is
	// the end of the pilot as the managed file sets it, and
	// PilotRemaining the time left, in seconds.
	Profile        string `json:"profile,omitempty"`
	PilotUntil     string `json:"pilot_until,omitempty"`
	PilotRemaining int64  `json:"pilot_remaining_seconds,omitempty"`
	// ApprovalChannels are the channels that answer an escalation, and
	// ApprovalGroup the group of the local-admin channel. With no channel
	// above the terminal that has anyone behind it, every escalated action
	// above the floor expires.
	ApprovalChannels []string `json:"approval_channels"`
	ApprovalGroup    string   `json:"approval_group,omitempty"`
}

// hasApprover reports whether a channel above the developer's terminal
// has someone behind it.
func (r managedSupervisorReport) hasApprover() bool {
	for _, c := range r.ApprovalChannels {
		switch c {
		case config.ApprovalChannelLocalAdmin, config.ApprovalChannelSelfElevated:
			if r.ApprovalGroup != "" {
				return true
			}
		case config.ApprovalChannelLocalAuth, config.ApprovalChannelOutOfBand:
			return true
		}
	}
	return false
}

type managedCollectionReport struct {
	Level string `json:"level"`
}

// runManagedDoctor is gryph doctor --managed: the compliance report of a
// managed install, for an MDM tool. It reads the managed configuration,
// the managed policy, the binary and every managed hook entry, and reads
// no per-user state. The exit code is 0 for the locked profile and 1
// otherwise, so a script can gate on it.
func runManagedDoctor(ctx context.Context, w io.Writer, asJSON bool) error {
	report := buildManagedDoctorReport(utils.WithoutProgramExecution(ctx))
	if err := renderManagedDoctor(w, report, asJSON); err != nil {
		return err
	}
	if report.Profile != string(selfprotect.ProfileLocked) {
		return &exitError{code: ExitGeneral, message: "doctor --managed: profile " + report.Profile}
	}
	return nil
}

func buildManagedDoctorReport(ctx context.Context) *managedDoctorReport {
	report := &managedDoctorReport{
		SchemaVersion: managedDoctorSchemaVersion,
		Issues:        []string{},
		Agents:        []managedAgentState{},
		Key:           managedKeyReport{Scope: managedKeyScopeUser},
		Supervisor:    managedSupervisorReport{State: managedSupervisorAbsent},
		Collection:    managedCollectionReport{Level: managedCollectionNone},
	}

	cfg := report.readConfig()
	report.readSupervisor(cfg)
	report.readKey(cfg)
	report.readPolicy(cfg)
	report.readTrustStore()
	report.readBinary(cfg)
	report.readAgents(ctx, cfg)

	if len(report.Issues) == 0 {
		report.Profile = string(selfprotect.ProfileLocked)
		report.Summary = managedLockedSummary
		// A protected machine key means the service holds the decision
		// and the trail. The command asks no socket, so it reports the
		// configuration, not whether the service runs now.
		if report.Key.Scope == managedKeyScopeSupervisor && report.Key.Protected {
			report.Summary = managedLockedServiceSummary
		}
	} else {
		report.Profile = string(selfprotect.ProfileNone)
		report.Summary = "None (" + report.Issues[0] + ")"
	}
	return report
}

// readConfig reads the managed configuration through the same trust check
// as the running Gryph, and from no other source.
// readKey reports the receipt signing key: the machine key of the decision
// service when the service is on and the key exists, else the user key.
// The machine key is protected when a system account owns it and nobody
// else can read it. The command reads its owner and mode, never the key.
func (r *managedDoctorReport) readKey(cfg *config.Config) {
	if cfg == nil || !cfg.Supervisor.Enabled {
		return
	}
	path := cfg.Supervisor.ReceiptKeyPath()
	info, err := os.Lstat(path)
	if err != nil {
		return
	}
	r.Key = managedKeyReport{Scope: managedKeyScopeSupervisor, Path: path}
	uid, _, ok := ownerOf(info)
	if !ok {
		return
	}
	r.Key.Owner = accountName(uid)
	r.Key.Protected = info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0 && account.IsSystemID(strconv.FormatUint(uint64(uid), 10))
	if !r.Key.Protected {
		r.Issues = append(r.Issues, "machine key "+path+" is readable by others or owned by a human account")
	}
}

// readSupervisor fills the profile of the decision service from the
// managed configuration.
func (r *managedDoctorReport) readSupervisor(cfg *config.Config) {
	if cfg == nil || !cfg.Supervisor.Enabled {
		return
	}
	r.Supervisor.Profile = cfg.Supervisor.EffectiveProfile()
	r.Supervisor.PilotUntil = cfg.Supervisor.PilotUntil
	r.Supervisor.ApprovalChannels = append([]string{}, cfg.Policy.Approval.Channels...)
	r.Supervisor.ApprovalGroup = cfg.Policy.Approval.LocalAdmin.Group
	if left, ok := cfg.Supervisor.PilotRemaining(); ok {
		r.Supervisor.PilotRemaining = int64(left.Seconds())
	}
}

func (r *managedDoctorReport) readConfig() *config.Config {
	state := config.ManagedConfigStatus()
	r.Config = managedFileReport{Path: state.Path, Chain: managedChainMissing}
	switch {
	case state.Path == "":
		r.Config.Chain = managedChainAbsent
		r.Issues = append(r.Issues, "this platform has no system managed location")
		return nil
	case !state.Exists:
		r.Issues = append(r.Issues, "no managed configuration at "+state.Path)
		return nil
	case state.Err != nil:
		r.Config.Chain = managedChainUntrusted
		r.Config.Error = state.Err.Error()
		r.Issues = append(r.Issues, "managed configuration not trusted: "+state.Err.Error())
		return nil
	}
	data, err := config.ReadTrustedFile(state.Path)
	if err == nil {
		var cfg *config.Config
		cfg, err = config.Parse(data)
		if err == nil {
			r.Config.Chain = managedChainOK
			return cfg
		}
	}
	r.Config.Chain = managedChainUntrusted
	r.Config.Error = err.Error()
	r.Issues = append(r.Issues, "managed configuration unreadable: "+err.Error())
	return nil
}

// readPolicy reads the managed policy file. A managed install without it
// leaves the user's policy in force, so it is an issue.
func (r *managedDoctorReport) readPolicy(cfg *config.Config) {
	state := config.ManagedPolicyState()
	r.Policy = managedPolicyReport{managedFileReport: managedFileReport{Path: state.File, Chain: managedChainMissing}}
	if cfg != nil {
		r.Policy.AllowUserPolicy = cfg.Policy.AllowUserPolicy
	}
	if state.File == "" {
		r.Policy.Chain = managedChainAbsent
		return
	}
	data, err := config.ReadTrustedFile(state.File)
	switch {
	case errors.Is(err, os.ErrNotExist):
		r.Issues = append(r.Issues, "no managed policy at "+state.File)
		return
	case err != nil:
		r.Policy.Chain = managedChainUntrusted
		r.Policy.Error = err.Error()
		r.Issues = append(r.Issues, "managed policy not trusted: "+err.Error())
		return
	}
	sum := sha256.Sum256(data)
	r.Policy.SHA256 = hex.EncodeToString(sum[:])
	policy, err := pdp.ParsePolicy(data)
	if err != nil {
		r.Policy.Chain = managedChainUntrusted
		r.Policy.Error = err.Error()
		r.Issues = append(r.Issues, "managed policy does not parse: "+err.Error())
		return
	}
	r.Policy.Chain = managedChainOK
	r.Policy.Version = policy.Version
}

// readTrustStore reads the managed trust store. It is optional, so a
// missing file is a fact and not an issue. A file that another user can
// write, or that does not parse, is an issue: a verifier would trust its
// keys.
func (r *managedDoctorReport) readTrustStore() {
	path := config.ManagedTrustStorePath()
	r.TrustStore = managedTrustStoreReport{managedFileReport: managedFileReport{Path: path, Chain: managedChainMissing}}
	if path == "" {
		r.TrustStore.Chain = managedChainAbsent
		return
	}
	data, err := config.ReadTrustedFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return
	case err != nil:
		r.TrustStore.Chain = managedChainUntrusted
		r.TrustStore.Error = err.Error()
		r.Issues = append(r.Issues, "managed trust store not trusted: "+err.Error())
		return
	}
	ts, err := receipt.ParseTrustStore(data)
	if err != nil {
		r.TrustStore.Chain = managedChainUntrusted
		r.TrustStore.Error = err.Error()
		r.Issues = append(r.Issues, "managed trust store does not parse: "+err.Error())
		return
	}
	r.TrustStore.Chain = managedChainOK
	r.TrustStore.Keys = len(ts.Keys)
}

// readBinary checks the binary that the managed hook entries name. A
// binary that root does not own, or that sits below a directory another
// user can write, is refused: the user could replace what every hook runs.
func (r *managedDoctorReport) readBinary(cfg *config.Config) {
	path := config.ManagedBinaryPath(cfg)
	r.Binary = managedFileReport{Path: path, Chain: managedChainMissing}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		r.Issues = append(r.Issues, "managed binary missing at "+path)
		return
	case err != nil:
		r.Binary.Error = err.Error()
		r.Issues = append(r.Issues, "managed binary unreadable: "+err.Error())
		return
	case !info.Mode().IsRegular():
		r.Binary.Chain = managedChainUntrusted
		r.Binary.Error = path + " is not a regular file"
		r.Issues = append(r.Issues, r.Binary.Error)
		return
	}
	if err := config.VerifyManagedPath(path); err != nil {
		r.Binary.Chain = managedChainUntrusted
		r.Binary.Error = err.Error()
		r.Issues = append(r.Issues, "managed binary not trusted, a user could replace it: "+err.Error())
		return
	}
	r.Binary.Chain = managedChainOK
}

// readAgents compares the managed entry of every agent in the allowlist
// with the managed configuration, through a dry run of the managed
// install. An empty allowlist protects no agent, so it is an issue.
func (r *managedDoctorReport) readAgents(ctx context.Context, cfg *config.Config) {
	if cfg == nil {
		return
	}
	if len(cfg.Managed.Agents) == 0 {
		r.Issues = append(r.Issues, "managed.agents names no agent")
		return
	}
	registry := agent.NewRegistry()
	engine.RegisterAdapters(registry, nil, config.Default())
	for _, name := range cfg.Managed.Agents {
		state := managedAgentState{Name: name, Level: selfprotect.LevelNone.String()}
		adapter, found := registry.Get(name)
		installer, ok := adapter.(agent.ManagedInstaller)
		if !found || !ok {
			state.Error = "no managed hook entry for this agent"
			r.Issues = append(r.Issues, name+": "+state.Error)
			r.Agents = append(r.Agents, state)
			continue
		}
		state.Class = string(installer.ManagedClass())
		state.Path = installer.ManagedHookPath()
		state.Locked = cfg.Managed.Locked(name)
		res, err := installer.InstallManaged(ctx, agent.ManagedInstallOptions{Command: r.Binary.Path, Lock: state.Locked, DryRun: true})
		switch {
		case err != nil:
			state.Error = err.Error()
			r.Issues = append(r.Issues, name+": cannot read the managed entry: "+err.Error())
		case res.Changed:
			state.Error = "the managed entry differs from the managed configuration"
			r.Issues = append(r.Issues, name+": "+state.Error)
		case installer.ManagedClass() == agent.ManagedClassLocked:
			state.Match = true
			state.Level = selfprotect.LevelPreventSameUser.String()
		default:
			state.Match = true
			state.Level = selfprotect.LevelDetect.String()
		}
		r.Agents = append(r.Agents, state)
	}
}

func renderManagedDoctor(w io.Writer, report *managedDoctorReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	lines := []string{
		"Managed doctor: " + report.Summary,
		fmt.Sprintf("  %-11s %s  chain %s", "Config", report.Config.Path, report.Config.Chain),
		fmt.Sprintf("  %-11s %s  chain %s", "Policy", report.Policy.Path, report.Policy.Chain),
	}
	if report.Policy.Version != "" || report.Policy.SHA256 != "" {
		lines = append(lines, fmt.Sprintf("  %-11s version %s  sha256 %s", "", report.Policy.Version, report.Policy.SHA256))
	}
	lines = append(lines,
		fmt.Sprintf("  %-11s %s  chain %s  keys %d", "Keys", report.TrustStore.Path, report.TrustStore.Chain, report.TrustStore.Keys),
		fmt.Sprintf("  %-11s %s  chain %s", "Binary", report.Binary.Path, report.Binary.Chain),
		"  "+keySummary(report.Key),
		fmt.Sprintf("  %-11s %s%s", "Supervisor", report.Supervisor.State, supervisorProfileSuffix(report.Supervisor)),
		fmt.Sprintf("  %-11s %s", "Collection", report.Collection.Level),
	)
	for _, a := range report.Agents {
		line := fmt.Sprintf("  %-12s %-11s %-18s %s", a.Name, a.Class, a.Level, a.Path)
		if a.Locked {
			line += "  (locked)"
		}
		if a.Error != "" {
			line += "  error: " + a.Error
		}
		lines = append(lines, line)
	}
	if len(report.Issues) > 0 {
		lines = append(lines, "Issues:")
		for _, issue := range report.Issues {
			lines = append(lines, "  - "+issue)
		}
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func supervisorProfileSuffix(r managedSupervisorReport) string {
	if r.Profile == "" {
		return ""
	}
	out := "  profile " + r.Profile
	if r.PilotRemaining > 0 {
		out += fmt.Sprintf("  pilot ends in %s (%s)", humanizeDuration(time.Duration(r.PilotRemaining)*time.Second), r.PilotUntil)
	}
	if !r.hasApprover() {
		out += "  no approver channel"
	} else {
		out += "  approvers " + strings.Join(r.ApprovalChannels, ", ")
		if r.ApprovalGroup != "" {
			out += " (group " + r.ApprovalGroup + ")"
		}
	}
	return out
}

func keySummary(k managedKeyReport) string {
	switch {
	case k.Scope != managedKeyScopeSupervisor:
		return managedKeySummary
	case k.Protected:
		return managedKeySupervisorSummary + " " + k.Path + " owner " + k.Owner
	default:
		return managedKeySupervisorExposed + " " + k.Path + " owner " + k.Owner
	}
}
