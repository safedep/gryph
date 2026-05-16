package mediation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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
// adapters. The Phase 4 surface is the contract; the full JSON-RPC proxy is
// deferred to Phase 5.
type MCPAdapter struct {
	classifier Classifier
	scorer     InjectionScorer
}

// MCPAdapterOption configures a MCPAdapter at construction time.
type MCPAdapterOption func(*MCPAdapter)

// WithMCPClassifier wires a Classifier into the adapter.
func WithMCPClassifier(c Classifier) MCPAdapterOption {
	return func(a *MCPAdapter) {
		if c != nil {
			a.classifier = c
		}
	}
}

// WithMCPInjectionScorer wires an InjectionScorer into the adapter.
func WithMCPInjectionScorer(s InjectionScorer) MCPAdapterOption {
	return func(a *MCPAdapter) {
		if s != nil {
			a.scorer = s
		}
	}
}

// NewMCPAdapter creates an MCPAdapter.
func NewMCPAdapter(opts ...MCPAdapterOption) *MCPAdapter {
	a := &MCPAdapter{}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Normalize converts an MCP tools/call request into a canonical Action.
// Type is always ActionToolUse, Tool comes from params.name, Parameters.Raw
// is params.arguments, and AgentSessionID comes from _meta.sessionID (string)
// when present. Optional classifier / scorer hooks run after normalization,
// matching the HookAdapter shape.
func (a *MCPAdapter) Normalize(_ context.Context, req *MCPToolCall, sess *session.Session) (*model.Action, error) {
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

	if a.classifier != nil {
		action.DataClassifications = a.classifier.Classify(action)
	}
	if a.scorer != nil && action.Type == model.ActionToolUse {
		action.InjectionScore = a.scorer.Score(action)
	}

	return action, nil
}
