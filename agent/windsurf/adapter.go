package windsurf

import (
	"context"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

const (
	AgentName   = agent.AgentWindsurf
	DisplayName = "Windsurf"
)

type Adapter struct {
	privacyChecker *events.PrivacyChecker
	loggingLevel   config.LoggingLevel
	contentHash    bool
}

func New(privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) *Adapter {
	return &Adapter{privacyChecker: privacyChecker, loggingLevel: loggingLevel, contentHash: contentHash}
}

func (a *Adapter) Name() string {
	return AgentName
}

func (a *Adapter) DisplayName() string {
	return DisplayName
}

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

// RenderResponse maps a decision to the Windsurf wire response. Windsurf has
// no JSON channel. Block is exit 2 with the reason on stderr. Guidance is
// exit 0 with advisory text on stderr.
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

func Register(registry *agent.Registry, privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) {
	registry.Register(New(privacyChecker, loggingLevel, contentHash))
}

var _ agent.Adapter = (*Adapter)(nil)
