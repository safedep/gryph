// Package devin provides the adapter for Devin CLI integration. Devin CLI
// uses the Claude Code compatible hook protocol, like Codex. Its hooks live
// in the hooks section of ~/.config/devin/config.json.
package devin

import (
	"context"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

const (
	AgentName   = agent.AgentDevin
	DisplayName = "Devin"
)

var _ agent.Adapter = (*Adapter)(nil)

type Adapter struct {
	privacyChecker *events.PrivacyChecker
	loggingLevel   config.LoggingLevel
	contentHash    bool
}

func New(privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) *Adapter {
	return &Adapter{privacyChecker: privacyChecker, loggingLevel: loggingLevel, contentHash: contentHash}
}

func (a *Adapter) Name() string        { return AgentName }
func (a *Adapter) DisplayName() string { return DisplayName }

func (a *Adapter) Detect(ctx context.Context) (*agent.DetectionResult, error) {
	return Detect(ctx)
}

func (a *Adapter) Install(ctx context.Context, opts agent.InstallOptions) (*agent.InstallResult, error) {
	return InstallHooks(ctx, opts)
}

func (a *Adapter) Uninstall(ctx context.Context, opts agent.UninstallOptions) (*agent.UninstallResult, error) {
	return UninstallHooks(ctx, opts)
}

func (a *Adapter) Status(ctx context.Context) (*agent.HookStatus, error) {
	return GetHookStatus(ctx)
}

func (a *Adapter) ParseEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error) {
	return a.parseHookEvent(hookType, rawData)
}

func Register(registry *agent.Registry, privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) {
	registry.Register(New(privacyChecker, loggingLevel, contentHash))
}

// blockingHookType is the one hook with a JSON decision channel.
const blockingHookType = "PreToolUse"

// RenderResponse maps a decision to the wire response. The blocking hook
// carries a JSON decision on stdout. Allow on other hooks emits nothing.
// Guidance on other hooks routes advisory text to stderr at exit 0. Block
// is exit 2 with the reason on stderr.
func (a *Adapter) RenderResponse(hookType string, decision agent.HookDecision, detail string) agent.HookResponse {
	jsonHook := hookType == blockingHookType
	switch decision {
	case agent.DecisionBlock:
		r := NewBlockResponse(detail)
		resp := agent.RenderedResponse{Err: r.Stderr(), Code: r.ExitCode()}
		if jsonHook {
			resp.Out = r.JSON()
		}
		return resp
	case agent.DecisionGuidance:
		r := NewGuidanceResponse(detail)
		if jsonHook {
			return agent.RenderedResponse{Out: r.JSON()}
		}
		return agent.RenderedResponse{Err: r.Stderr()}
	default:
		return agent.RenderedResponse{}
	}
}
