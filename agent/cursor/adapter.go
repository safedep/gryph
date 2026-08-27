// Package cursor provides the adapter for Cursor integration.
package cursor

import (
	"context"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
)

const (
	// AgentName is the machine identifier for Cursor.
	AgentName = agent.AgentCursor
	// DisplayName is the human-readable name for Cursor.
	DisplayName = "Cursor"
)

// Adapter implements the agent.Adapter interface for Cursor.
type Adapter struct {
	privacyChecker *events.PrivacyChecker
	loggingLevel   config.LoggingLevel
	contentHash    bool
}

// New creates a new Cursor adapter.
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

// Detect determines if Cursor is installed.
func (a *Adapter) Detect(ctx context.Context) (*agent.DetectionResult, error) {
	return Detect(ctx)
}

// Install installs hooks for Cursor.
func (a *Adapter) Install(ctx context.Context, opts agent.InstallOptions) (*agent.InstallResult, error) {
	return InstallHooks(ctx, opts)
}

// Uninstall removes hooks from Cursor.
func (a *Adapter) Uninstall(ctx context.Context, opts agent.UninstallOptions) (*agent.UninstallResult, error) {
	return UninstallHooks(ctx, opts)
}

// Status checks the current hook state.
func (a *Adapter) Status(ctx context.Context) (*agent.HookStatus, error) {
	return GetHookStatus(ctx)
}

// ParseEvent converts a Cursor event to the common format.
func (a *Adapter) ParseEvent(ctx context.Context, hookType string, rawData []byte) (*events.Event, error) {
	return a.parseHookEvent(hookType, rawData)
}

// RenderResponse maps a decision to the Cursor wire response. Cursor reads
// JSON on stdout for every hook and never uses exit codes. Each hook type
// has its own response schema.
func (a *Adapter) RenderResponse(hookType string, decision agent.HookDecision, detail string) agent.HookResponse {
	switch decision {
	case agent.DecisionBlock:
		return agent.RenderedResponse{
			Out: renderDecisionResponse(hookType, NewDenyResponse(detail), false),
		}
	case agent.DecisionGuidance:
		r := NewGuidanceResponse(detail)
		resp := agent.RenderedResponse{
			Out: renderDecisionResponse(hookType, r, true),
		}
		// preToolUse JSON carries decision=allow with no reason field, so
		// the advisory text routes to stderr.
		if hookType == "preToolUse" && r.Reason != "" {
			resp.Err = r.Reason
		}
		return resp
	default:
		return agent.RenderedResponse{Out: renderAllowResponse(hookType)}
	}
}

// renderDecisionResponse builds the JSON response for a hook type carrying
// an explicit deny (cont=false) or allow-with-advisory (cont=true) decision.
func renderDecisionResponse(hookType string, response *HookResponse, cont bool) []byte {
	switch hookType {
	case "preToolUse":
		return GeneratePreToolUseResponse(response)

	case "beforeShellExecution", "beforeMCPExecution", "beforeReadFile", "beforeTabFileRead":
		return GeneratePermissionResponse(response)

	case "beforeSubmitPrompt", "sessionStart":
		return GenerateContinueResponse(cont, response.Reason)

	default:
		return []byte("{}")
	}
}

// renderAllowResponse builds the JSON response for the plain allow path.
func renderAllowResponse(hookType string) []byte {
	allowResponse := NewAllowResponse()

	switch hookType {
	case "preToolUse":
		return GeneratePreToolUseResponse(allowResponse)

	case "beforeShellExecution", "beforeMCPExecution", "beforeReadFile", "beforeTabFileRead":
		return GeneratePermissionResponse(allowResponse)

	case "beforeSubmitPrompt", "sessionStart":
		return GenerateContinueResponse(true, "")

	case "stop", "subagentStop":
		return GenerateStopResponse("")

	default:
		return []byte("{}")
	}
}

// Register adds this adapter to the given registry.
func Register(registry *agent.Registry, privacyChecker *events.PrivacyChecker, loggingLevel config.LoggingLevel, contentHash bool) {
	registry.Register(New(privacyChecker, loggingLevel, contentHash))
}

// Ensure Adapter implements agent.Adapter
var _ agent.Adapter = (*Adapter)(nil)
