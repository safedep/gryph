// Package agent provides the adapter pattern for agent integrations.
package agent

import (
	"bytes"
	"context"
	"path"
	"slices"

	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/core/events"
)

// Standard agent identifiers.
const (
	AgentClaudeCode  = "claude-code"
	AgentCursor      = "cursor"
	AgentGemini      = "gemini"
	AgentOpenCode    = "opencode"
	AgentOpenClaw    = "openclaw"
	AgentWindsurf    = "windsurf"
	AgentPiAgent     = "pi-agent"
	AgentCodex       = "codex"
	AgentDevin       = "devin"
	AgentCommandCode = "command-code"
)

// DetectionResult contains information about a detected agent.
type DetectionResult struct {
	// Installed indicates if the agent is installed.
	Installed bool
	// Version is the detected version of the agent.
	Version string
	// Path is the installation path of the agent.
	Path string
	// ConfigPath is the configuration directory path.
	ConfigPath string
	// HooksPath is the hooks directory path.
	HooksPath string
	// Message provides additional context (e.g., why not installed).
	Message string
}

// InstallOptions configures hook installation.
type InstallOptions struct {
	// DryRun shows what would be installed without making changes.
	DryRun bool
	// Force overwrites existing hooks without prompting.
	Force bool
	// Backup creates backups of existing hooks.
	Backup bool
	// BackupDir is the directory to store backups.
	BackupDir string
	// Command is the program that the hook entries name. Empty names the
	// running binary by its absolute path.
	Command string
	// Repair marks an unattended rewrite of the Gryph entries. The adapter
	// then refuses a symbolic link in the path of the file, keeps a file
	// that does not parse, and makes no backup. Read and write the file with
	// ReadHookFile and WriteHookFile, which apply these rules.
	Repair bool
}

// InstallResult contains the result of hook installation.
type InstallResult struct {
	// Success indicates if installation was successful.
	Success bool
	// HooksInstalled is the list of hooks that were installed.
	HooksInstalled []string
	// BackupPaths maps hook names to their backup paths.
	BackupPaths map[string]string
	// Warnings contains non-fatal warnings.
	Warnings []string
	// Error contains the error if installation failed.
	Error error
}

// UninstallOptions configures hook removal.
type UninstallOptions struct {
	// DryRun shows what would be removed without making changes.
	DryRun bool
	// RestoreBackup restores backed-up hooks if available.
	RestoreBackup bool
	// BackupDir is the directory containing backups.
	BackupDir string
}

// UninstallResult contains the result of hook removal.
type UninstallResult struct {
	// Success indicates if uninstallation was successful.
	Success bool
	// HooksRemoved is the list of hooks that were removed.
	HooksRemoved []string
	// BackupsRestored indicates if backups were restored.
	BackupsRestored bool
	// Error contains the error if uninstallation failed.
	Error error
}

// HookStatus contains the status of installed hooks.
type HookStatus struct {
	// Installed indicates if hooks are installed.
	Installed bool
	// Hooks is the list of installed hook names.
	Hooks []string
	// Valid indicates if all hooks are valid (not corrupted).
	Valid bool
	// Issues lists any problems with the hooks.
	Issues []string
}

// HookDecision is the three-valued outcome the CLI maps from the security
// evaluator before it renders a response.
type HookDecision int

const (
	// DecisionAllow lets the action proceed.
	DecisionAllow HookDecision = iota
	// DecisionBlock stops the action with a reason.
	DecisionBlock
	// DecisionGuidance lets the action proceed with advisory text.
	DecisionGuidance
)

// HookResponse is the transport-neutral result of a hook decision. The CLI
// performs the IO: it writes Stdout, and it routes Stderr on one channel
// only. A non-zero exit carries the text in the exit error, which main
// writes. A zero exit writes the text directly.
type HookResponse interface {
	// Stdout returns the bytes to write to stdout, or nil.
	Stdout() []byte
	// Stderr returns the block reason or advisory text, or "".
	Stderr() string
	// ExitCode returns 0 for allow, 1 for a non-blocking error, 2 for block.
	ExitCode() int
}

// RenderedResponse is a plain HookResponse value adapters return from
// RenderResponse.
type RenderedResponse struct {
	Out  []byte
	Err  string
	Code int
}

func (r RenderedResponse) Stdout() []byte { return r.Out }

func (r RenderedResponse) Stderr() string { return r.Err }

func (r RenderedResponse) ExitCode() int { return r.Code }

// Adapter defines the interface for agent integrations.
type Adapter interface {
	// Name returns the machine identifier (e.g., "claude-code").
	Name() string

	// DisplayName returns the human-readable name (e.g., "Claude Code").
	DisplayName() string

	// Detect determines if the agent is installed.
	Detect(ctx context.Context) (*DetectionResult, error)

	// Install installs hooks for this agent.
	Install(ctx context.Context, opts InstallOptions) (*InstallResult, error)

	// Uninstall removes hooks from this agent.
	Uninstall(ctx context.Context, opts UninstallOptions) (*UninstallResult, error)

	// Status checks the current hook state.
	Status(ctx context.Context) (*HookStatus, error)

	// ParseEvent converts an agent-specific event to the common format.
	ParseEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error)

	// RenderResponse maps a decision to this agent's wire response for the
	// given hook type. The adapter owns the per-hook-type knowledge: whether
	// stdout carries JSON, what the exit code is, and when text routes to
	// stderr.
	RenderResponse(hookType string, decision HookDecision, detail string) HookResponse

	// HookConfigPaths returns doublestar globs for the files that hold this
	// agent's Gryph hook configuration. Self-protection blocks agent changes
	// to these files, so a governed agent cannot remove its own hooks.
	HookConfigPaths() []string

	// Hooks declares every hook this adapter installs and parses, with its
	// phase. The decision service reads the phase from here.
	Hooks() []events.HookSpec
}

// ManagedInstaller is an optional interface of an adapter whose agent reads
// a machine-wide hook configuration that an administrator owns. gryph
// install --managed writes it once for every user of the host, so a hook
// entry exists before the agent is installed and the user cannot remove it.
type ManagedInstaller interface {
	// ManagedHookPath returns the file that holds the Gryph entries on this
	// platform.
	ManagedHookPath() string
	// ManagedClass says how far the managed file resists the user.
	ManagedClass() ManagedClass
	// InstallManaged writes the Gryph entries, and the agent's own lock
	// when opts.Lock is set. It changes nothing when the file already holds
	// them, so a repeated run is safe.
	InstallManaged(ctx context.Context, opts ManagedInstallOptions) (*ManagedInstallResult, error)
	// UninstallManaged removes the Gryph entries and the lock.
	UninstallManaged(ctx context.Context, opts ManagedInstallOptions) (*ManagedInstallResult, error)
}

// ManagedClass says how far an agent's managed hook file resists the user
// of the host. The vendor documentation decides the class, and each class
// must be checked again for every agent release.
type ManagedClass string

const (
	// ManagedClassLocked: the vendor documents that a user cannot turn the
	// managed hooks off, and offers a lock that lets only managed hooks run.
	ManagedClassLocked ManagedClass = "locked"
	// ManagedClassSystemPath: the agent reads a system file that root owns,
	// but the vendor does not document that the user cannot override or
	// disable its hooks. The reconcile pass keeps checking the user scope.
	ManagedClassSystemPath ManagedClass = "system_path"
)

// ManagedInstallOptions configures a managed install.
type ManagedInstallOptions struct {
	// Command is the absolute path of the root-owned gryph binary.
	Command string
	// Lock turns on the agent's own switch that lets only managed hooks run.
	Lock bool
	// DryRun reports the change without making it.
	DryRun bool
}

// ManagedInstallResult is the outcome of a managed install or uninstall.
type ManagedInstallResult struct {
	// Path is the managed file.
	Path string
	// Changed is true when the call wrote or removed the file, or would
	// have in a dry run.
	Changed bool
	// Locked is true when the file turns on the agent's lock.
	Locked bool
}

// ProcessNamer is an optional interface of an adapter. It names the
// programs of the agent as the kernel reports them, so a census can match
// a live agent process to the adapter.
type ProcessNamer interface {
	ProcessNames() []string
}

// FailModeReporter is an optional interface of an adapter whose agent can
// block the action when the hook fails. FailClosed reports whether every
// Gryph entry asks for that.
type FailModeReporter interface {
	FailClosed(ctx context.Context) (bool, error)
}

// HookTypeNames returns the hook type names of specs, in order.
func HookTypeNames(specs []events.HookSpec) []string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, string(s.Type))
	}
	return names
}

// RequiredHookTypeNames returns the hook type names that a valid install must
// hold. A prompt hook is not required. An install from before prompt capture
// lacks it, and doctor reports that as a warning.
func RequiredHookTypeNames(specs []events.HookSpec) []string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		if !s.Prompt {
			names = append(names, string(s.Type))
		}
	}
	return names
}

// MissingPromptHooks returns the prompt hooks in specs that installed does
// not hold. An install from before prompt capture lacks them, and the
// session context then has no intent.
func MissingPromptHooks(specs []events.HookSpec, installed []string) []string {
	var missing []string
	for _, s := range specs {
		if s.Prompt && !slices.Contains(installed, string(s.Type)) {
			missing = append(missing, string(s.Type))
		}
	}
	return missing
}

// SetPluginStatus fills status for a plugin file that Gryph generates.
// marker returns the text that the plugin holds for one hook type. A plugin
// is valid only when it equals expected, or when its SHA-256 digest is in
// legacyDigests. A legacy plugin can lack the prompt hooks, so doctor warns
// about them. Any other content can skip a block, so the plugin is invalid,
// with the issue text stale.
func SetPluginStatus(status *HookStatus, content, expected []byte, legacyDigests []string, specs []events.HookSpec, marker func(hookType string) string, stale string) {
	status.Installed = true
	for _, s := range specs {
		if bytes.Contains(content, []byte(marker(string(s.Type)))) {
			status.Hooks = append(status.Hooks, string(s.Type))
		}
	}
	status.Valid = bytes.Equal(content, expected) || slices.Contains(legacyDigests, utils.HashContent(string(content)))
	if !status.Valid {
		status.Issues = append(status.Issues, stale)
	}
}

// HomeConfigGlob returns a glob for a path under the user's home directory.
// The "**/" anchor matches any home location and forward-slash paths.
func HomeConfigGlob(elem ...string) string {
	return "**/" + path.Join(elem...)
}
