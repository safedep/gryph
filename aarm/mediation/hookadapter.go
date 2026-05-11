package mediation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// HookAdapter is the AML adapter for Gryph's existing agent-hook
// transport. It maps an events.Event (already parsed by an agent
// adapter) plus the current Session into a canonical aarm.Action.
type HookAdapter struct{}

// NewHookAdapter returns a stateless HookAdapter.
func NewHookAdapter() *HookAdapter {
	return &HookAdapter{}
}

// Normalize implements Adapter.
func (h *HookAdapter) Normalize(_ context.Context, event *events.Event, sess *session.Session) (*aarm.Action, error) {
	if event == nil {
		return nil, fmt.Errorf("mediation: event must not be nil")
	}

	action := &aarm.Action{
		ID:             uuid.New(),
		Timestamp:      event.Timestamp,
		SessionID:      event.SessionID,
		EventID:        event.ID,
		Type:           event.ActionType,
		Tool:           event.ToolName,
		Agent:          event.AgentName,
		AgentSessionID: event.AgentSessionID,
		WorkingDir:     event.WorkingDirectory,
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

	params, err := extractParameters(event)
	if err != nil {
		return nil, fmt.Errorf("mediation: extract parameters for %s: %w", event.ActionType, err)
	}
	action.Parameters = params

	return action, nil
}

// extractParameters pulls the canonical Parameters from an event's
// type-specific payload. Unknown or payload-less event types return an
// empty Parameters without error — the engine should still see the
// action, even if no parameter fields apply.
func extractParameters(event *events.Event) (aarm.Parameters, error) {
	if len(event.Payload) == 0 {
		return aarm.Parameters{}, nil
	}

	switch event.ActionType {
	case events.ActionFileRead:
		p, err := event.GetFileReadPayload()
		if err != nil || p == nil {
			return aarm.Parameters{}, err
		}
		path := p.Path
		if path == "" {
			path = p.Pattern
		}
		return aarm.Parameters{
			Path:      path,
			SizeBytes: p.SizeBytes,
		}, nil

	case events.ActionFileWrite:
		p, err := event.GetFileWritePayload()
		if err != nil || p == nil {
			return aarm.Parameters{}, err
		}
		return aarm.Parameters{
			Path:         p.Path,
			SizeBytes:    p.SizeBytes,
			LinesAdded:   p.LinesAdded,
			LinesRemoved: p.LinesRemoved,
			Content:      p.ContentPreview,
		}, nil

	case events.ActionFileDelete:
		p, err := event.GetFileDeletePayload()
		if err != nil || p == nil {
			return aarm.Parameters{}, err
		}
		return aarm.Parameters{Path: p.Path}, nil

	case events.ActionCommandExec:
		p, err := event.GetCommandExecPayload()
		if err != nil || p == nil {
			return aarm.Parameters{}, err
		}
		return aarm.Parameters{
			Command: p.Command,
			Args:    p.Args,
			Content: p.StdoutPreview,
		}, nil

	case events.ActionToolUse:
		p, err := event.GetToolUsePayload()
		if err != nil || p == nil {
			return aarm.Parameters{}, err
		}
		params := aarm.Parameters{Raw: rawToolInput(p.Input)}
		if v, ok := params.Raw["url"].(string); ok {
			params.URL = v
		}
		if v, ok := params.Raw["file_path"].(string); ok {
			params.Path = v
		} else if v, ok := params.Raw["path"].(string); ok {
			params.Path = v
		}
		if v, ok := params.Raw["command"].(string); ok {
			params.Command = v
		}
		return params, nil

	default:
		// Session start/end, notifications, subagent events, network
		// requests — no canonical parameters today. Future adapters
		// may populate Raw for these.
		return aarm.Parameters{}, nil
	}
}

func rawToolInput(in json.RawMessage) map[string]any {
	if len(in) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(in, &m); err != nil {
		return nil
	}
	return m
}
