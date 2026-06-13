package mediation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/identity"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// HookAdapter normalizes hook events into canonical actions. Optional
// classifier and injection scorer run after normalization to populate the
// reserved risk-signal fields on Action. The AARM safe-by-default
// classification safety net lives in classify.NewFailSafe so callers can
// wrap any Classifier (including nil) once at construction. The shared
// Common holds the classifier, scorer, and identity capturer so both
// adapters configure them through the same option helpers.
type HookAdapter struct {
	Common
}

// NewHookAdapter creates a HookAdapter. Accepts the shared CommonOption set
// (WithClassifier, WithInjectionScorer, WithIdentityCapturer).
func NewHookAdapter(opts ...CommonOption) *HookAdapter {
	h := &HookAdapter{Common: Common{IdentityCapture: identity.NewDefaultCapturer()}}
	for _, opt := range opts {
		opt(&h.Common)
	}
	return h
}

// Normalize implements Adapter.
func (h *HookAdapter) Normalize(ctx context.Context, event *events.Event, sess *session.Session) (*model.Action, error) {
	if event == nil {
		return nil, fmt.Errorf("mediation: event must not be nil")
	}

	action := &model.Action{
		ID:             uuid.New(),
		Timestamp:      event.Timestamp,
		SessionID:      event.SessionID,
		EventID:        event.ID,
		Type:           normalizeActionType(event.ActionType),
		Tool:           event.ToolName,
		Agent:          event.AgentName,
		AgentSessionID: event.AgentSessionID,
		WorkingDir:     event.WorkingDirectory,
		SubagentID:     event.SubagentID,
		SubagentType:   event.SubagentType,
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
	action.Phase = phaseForHookType(event.HookType)
	applyContentMatch(action, event.FullContent)

	h.applyEnrichment(ctx, action, nil)

	return action, nil
}

func extractParameters(event *events.Event) (model.Parameters, error) {
	if len(event.Payload) == 0 {
		return model.Parameters{}, nil
	}

	switch event.ActionType {
	case events.ActionFileRead:
		p, err := event.GetFileReadPayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		path := p.Path
		if path == "" {
			path = p.Pattern
		}
		return model.Parameters{
			Path:      path,
			SizeBytes: p.SizeBytes,
		}, nil

	case events.ActionFileWrite:
		p, err := event.GetFileWritePayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		return model.Parameters{
			Path:         p.Path,
			SizeBytes:    p.SizeBytes,
			LinesAdded:   p.LinesAdded,
			LinesRemoved: p.LinesRemoved,
			Content:      p.ContentPreview,
		}, nil

	case events.ActionFileDelete:
		p, err := event.GetFileDeletePayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		return model.Parameters{Path: p.Path}, nil

	case events.ActionCommandExec:
		p, err := event.GetCommandExecPayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		return model.Parameters{
			Command: p.Command,
			Args:    p.Args,
			Content: p.StdoutPreview,
		}, nil

	case events.ActionToolUse:
		p, err := event.GetToolUsePayload()
		if err != nil || p == nil {
			return model.Parameters{}, err
		}
		params := model.Parameters{Raw: rawToolInput(p.Input)}
		populateWellKnownParams(&params, params.Raw)
		return params, nil

	default:
		return model.Parameters{}, nil
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

func normalizeActionType(actionType events.ActionType) model.ActionType {
	switch actionType {
	case events.ActionFileRead:
		return model.ActionFileRead
	case events.ActionFileWrite:
		return model.ActionFileWrite
	case events.ActionFileDelete:
		return model.ActionFileDelete
	case events.ActionCommandExec:
		return model.ActionCommandExec
	case events.ActionNetworkRequest:
		return model.ActionNetworkRequest
	case events.ActionToolUse:
		return model.ActionToolUse
	case events.ActionSessionStart:
		return model.ActionSessionStart
	case events.ActionSessionEnd:
		return model.ActionSessionEnd
	case events.ActionNotification:
		return model.ActionNotification
	case events.ActionSubagentStart:
		return model.ActionSubagentStart
	case events.ActionSubagentStop:
		return model.ActionSubagentStop
	default:
		return model.ActionUnknown
	}
}
