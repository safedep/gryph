package mediation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/identity"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/session"
)

// MCPToolCall is the structurally-typed shape of an MCP tools/call JSON-RPC
// payload. The full proxy will deserialize the wire format into this struct
// and hand it to MCPAdapter.Normalize.
type MCPToolCall struct {
	Method string            `json:"method"`
	Params MCPToolCallParams `json:"params"`
	Meta   map[string]any    `json:"_meta,omitempty"`
}

// MCPToolCallParams is the params object of a tools/call invocation.
type MCPToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// MCPAdapter normalizes MCP tool-call requests into canonical actions.
// Mirrors HookAdapter so the classifier / scorer wiring is identical across
// adapters. The Phase 4 surface is the contract. The full JSON-RPC proxy is
// deferred to Phase 5. The shared Common holds the classifier, scorer, and
// identity capturer so both adapters configure them through the same option
// helpers (WithClassifier, WithInjectionScorer, WithIdentityCapturer).
type MCPAdapter struct {
	Common
}

// NewMCPAdapter creates an MCPAdapter. Accepts the shared CommonOption set.
// Per-event Meta overrides (req.Meta["human_principal"],
// ["service_identity"], ["role_scope"]) take precedence over the capturer's
// output when present and non-empty.
func NewMCPAdapter(opts ...CommonOption) *MCPAdapter {
	a := &MCPAdapter{Common: Common{IdentityCapture: identity.NewDefaultCapturer()}}
	for _, opt := range opts {
		opt(&a.Common)
	}
	return a
}

// Normalize converts an MCP tools/call request into a canonical Action.
// Type is always ActionToolUse, Tool comes from params.name, Parameters.Raw
// is params.arguments, and AgentSessionID comes from _meta.sessionID (string)
// when present. Optional classifier / scorer / identity hooks run after
// normalization, matching the HookAdapter shape.
func (a *MCPAdapter) Normalize(ctx context.Context, req *MCPToolCall, sess *session.Session) (*model.Action, error) {
	if req == nil {
		return nil, fmt.Errorf("mediation: nil MCP tool call")
	}
	if req.Params.Name == "" {
		return nil, fmt.Errorf("mediation: MCP tool call missing params.name")
	}

	action := &model.Action{
		ID:   uuid.New(),
		Type: model.ActionToolUse,
		Tool: req.Params.Name,
		Parameters: model.Parameters{
			Raw: req.Params.Arguments,
		},
	}

	populateWellKnownParams(&action.Parameters, req.Params.Arguments)

	if req.Meta != nil {
		if sid, ok := req.Meta["sessionID"].(string); ok {
			action.AgentSessionID = sid
		} else if sid, ok := req.Meta["session_id"].(string); ok {
			action.AgentSessionID = sid
		}
		if agent, ok := req.Meta["agent"].(string); ok {
			action.Agent = agent
		}
		if subID, ok := req.Meta["subagent_id"].(string); ok {
			action.SubagentID = subID
		}
		if subType, ok := req.Meta["subagent_type"].(string); ok {
			action.SubagentType = subType
		}
	}

	if sess != nil {
		action.Project = sess.ProjectName
		if action.AgentSessionID == "" {
			action.AgentSessionID = sess.AgentSessionID
		}
		if action.WorkingDir == "" {
			action.WorkingDir = sess.WorkingDirectory
		}
	}

	a.applyEnrichment(ctx, action, req.Meta)

	return action, nil
}
