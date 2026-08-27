package gemini

import (
	"context"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

const (
	AgentName   = agent.AgentGemini
	DisplayName = "Gemini CLI"
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

func Register(registry *agent.Registry, privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) {
	registry.Register(New(privacyChecker, loggingLevel, contentHash))
}

var _ agent.Adapter = (*Adapter)(nil)

// blockingHookType is the one Gemini CLI hook with a JSON decision channel.
const blockingHookType = "BeforeTool"

// RenderResponse maps a decision to the Gemini CLI wire response. BeforeTool
// carries a JSON decision on stdout. Other hooks receive an empty JSON
// object on allow and advisory text on stderr for guidance. Block is exit 2
// with the reason on stderr.
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
		if jsonHook {
			return agent.RenderedResponse{Out: NewAllowResponse().JSON()}
		}
		return agent.RenderedResponse{Out: []byte("{}")}
	}
}
