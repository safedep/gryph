package mediation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// HookAdapter normalizes hook events into canonical actions. Optional
// classifier and injection scorer run after normalization to populate the
// reserved risk-signal fields on Action. The AARM safe-by-default
// classification safety net lives in classify.NewFailSafe so callers can
// wrap any Classifier (including nil) once at construction.
type HookAdapter struct {
	classifier Classifier
	scorer     InjectionScorer
}

// HookAdapterOption configures a HookAdapter at construction time.
type HookAdapterOption func(*HookAdapter)

// WithClassifier wires a Classifier into the adapter. After Normalize
// produces an Action, the classifier is invoked and its output stored in
// Action.DataClassifications. Wrap the supplied Classifier in
// classify.NewFailSafe to keep the AARM-conformant safety-net label.
func WithClassifier(c Classifier) HookAdapterOption {
	return func(h *HookAdapter) {
		if c != nil {
			h.classifier = c
		}
	}
}

// WithInjectionScorer wires an InjectionScorer into the adapter. The score
// is populated only when Action.Type == ActionToolUse.
func WithInjectionScorer(s InjectionScorer) HookAdapterOption {
	return func(h *HookAdapter) {
		if s != nil {
			h.scorer = s
		}
	}
}

// NewHookAdapter creates a HookAdapter.
func NewHookAdapter(opts ...HookAdapterOption) *HookAdapter {
	h := &HookAdapter{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Normalize implements Adapter.
func (h *HookAdapter) Normalize(_ context.Context, event *events.Event, sess *session.Session) (*model.Action, error) {
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

	if h.classifier != nil {
		action.DataClassifications = h.classifier.Classify(action)
	}
	if h.scorer != nil && action.Type == model.ActionToolUse {
		action.InjectionScore = h.scorer.Score(action)
	}

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
