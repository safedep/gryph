// Package claudecode provides the adapter for Claude Code integration.
package claudecode

import (
	"context"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/core/events"
)

const (
	// AgentName is the machine identifier for Claude Code.
	AgentName = agent.AgentClaudeCode
	// DisplayName is the human-readable name for Claude Code.
	DisplayName = agent.DisplayClaudeCode
)

// Adapter implements the agent.Adapter interface for Claude Code.
type Adapter struct {
	privacyChecker *events.PrivacyChecker
}

// New creates a new Claude Code adapter.
func New(privacyChecker *events.PrivacyChecker) *Adapter {
	return &Adapter{privacyChecker: privacyChecker}
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
	return ParseHookEvent(ctx, hookType, rawData, a.privacyChecker)
}

// Register adds this adapter to the given registry.
func Register(registry *agent.Registry, privacyChecker *events.PrivacyChecker) {
	registry.Register(New(privacyChecker))
}

// Ensure Adapter implements agent.Adapter
var _ agent.Adapter = (*Adapter)(nil)
