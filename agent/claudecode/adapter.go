// Package claudecode provides the adapter for Claude Code integration.
package claudecode

import (
	"context"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

const (
	// AgentName is the machine identifier for Claude Code.
	AgentName = agent.AgentClaudeCode
	// DisplayName is the human-readable name for Claude Code.
	DisplayName = "Claude Code"
)

// Adapter implements the agent.Adapter interface for Claude Code.
type Adapter struct {
	privacyChecker *events.PrivacyChecker
	loggingLevel   config.LoggingLevel
	contentHash    bool
}

// New creates a new Claude Code adapter.
func New(privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) *Adapter {
	return &Adapter{privacyChecker: privacyChecker, loggingLevel: loggingLevel, contentHash: contentHash}
}

// Name returns the machine identifier.
func (a *Adapter) Name() string {
	return AgentName
}

// DisplayName returns the human-readable name.
func (a *Adapter) DisplayName() string {
	return DisplayName
}

// Detect determines if Claude Code is installed.
func (a *Adapter) Detect(ctx context.Context) (*agent.DetectionResult, error) {
	return Detect(ctx)
}

// Install installs hooks for Claude Code.
func (a *Adapter) Install(ctx context.Context, opts agent.InstallOptions) (*agent.InstallResult, error) {
	return InstallHooks(ctx, opts)
}

// Uninstall removes hooks from Claude Code.
func (a *Adapter) Uninstall(ctx context.Context, opts agent.UninstallOptions) (*agent.UninstallResult, error) {
	return UninstallHooks(ctx, opts)
}

// Status checks the current hook state.
func (a *Adapter) Status(ctx context.Context) (*agent.HookStatus, error) {
	return GetHookStatus(ctx)
}

// ParseEvent converts a Claude Code event to the common format.
func (a *Adapter) ParseEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error) {
	return a.parseHookEvent(hookType, rawData)
}

// RenderResponse maps a decision to the Claude Code wire response. Claude
// Code has no JSON channel. Block is exit 2 with the reason on stderr,
// shown to Claude. Guidance is exit 0 with advisory text on stderr.
func (a *Adapter) RenderResponse(hookType string, decision agent.HookDecision, detail string) agent.HookResponse {
	var r *HookResponse
	switch decision {
	case agent.DecisionBlock:
		r = NewBlockResponse(detail)
	case agent.DecisionGuidance:
		r = NewGuidanceResponse(detail)
	default:
		r = NewAllowResponse()
	}
	return agent.RenderedResponse{Err: r.Stderr(), Code: r.ExitCode()}
}

// Register adds this adapter to the given registry.
func Register(registry *agent.Registry, privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) {
	registry.Register(New(privacyChecker, loggingLevel, contentHash))
}

// Ensure Adapter implements agent.Adapter
var _ agent.Adapter = (*Adapter)(nil)
