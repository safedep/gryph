// Package agent provides the adapter pattern for agent integrations.
package agent

import (
	"context"
	"path"

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

// HomeConfigGlob returns a glob for a path under the user's home directory.
// The "**/" anchor matches any home location and forward-slash paths.
func HomeConfigGlob(elem ...string) string {
	return "**/" + path.Join(elem...)
}
