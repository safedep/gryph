package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/selfprotect"
	"github.com/safedep/gryph/storage"
)

// TamperRecorder writes tamper events into the system session of the
// account, each with a receipt in that session's chain. The receipt goes
// through the same generator as an agent action, so the chain code does not
// know about tamper events.
type TamperRecorder struct {
	store    storage.Store
	receipts receipt.Generator
	session  *session.Session
}

// TamperRecorder returns the recorder of this install. Call it after
// InitStore.
func (a *Runtime) TamperRecorder() (*TamperRecorder, error) {
	if a.Store == nil {
		return nil, errors.New("engine: the store is not open")
	}
	id, err := account.CurrentID()
	if err != nil {
		return nil, err
	}
	gen, err := newReceiptGenerator(a.Config, a.Paths, a.Store)
	if err != nil {
		return nil, err
	}
	return newTamperRecorder(a.Store, gen, id), nil
}

func newTamperRecorder(store storage.Store, receipts receipt.Generator, accountID string) *TamperRecorder {
	return &TamperRecorder{store: store, receipts: receipts, session: session.NewSystemSession(accountID)}
}

// SessionID returns the ID of the system session the recorder writes to.
func (r *TamperRecorder) SessionID() uuid.UUID { return r.session.ID }

// RecordChanges compares statuses with the last recorded state of each
// asset and records one tamper event per change: a drift that appears or
// changes, a drift that clears, or a level that changes. An asset with no
// recorded state and no drift records nothing. It returns the events it
// recorded.
func (r *TamperRecorder) RecordChanges(ctx context.Context, statuses []selfprotect.AssetStatus) ([]*events.Event, error) {
	last, err := r.lastRecorded(ctx)
	if err != nil {
		return nil, err
	}
	var recorded []*events.Event
	for _, s := range statuses {
		payload := events.TamperPayload{
			Asset:       string(s.Asset),
			Agent:       s.Agent,
			LevelBefore: s.Level.String(),
			LevelAfter:  s.Level.String(),
			Drift:       s.Drift,
			Provider:    s.Provider,
			Detail:      s.Detail,
		}
		prev, seen := last[tamperKey(payload.Asset, payload.Agent)]
		switch {
		case !seen && s.Drift == "":
			continue
		case seen && prev.Drift == payload.Drift && prev.LevelAfter == payload.LevelAfter:
			continue
		case seen:
			payload.LevelBefore = prev.LevelAfter
		}
		event, err := r.Record(ctx, payload)
		if err != nil {
			return recorded, err
		}
		recorded = append(recorded, event)
	}
	return recorded, nil
}

// Record writes one tamper event and its receipt.
func (r *TamperRecorder) Record(ctx context.Context, payload events.TamperPayload) (*events.Event, error) {
	if err := r.ensureSession(ctx); err != nil {
		return nil, err
	}

	event := events.NewEvent(r.session.ID, session.SystemAgentName, events.ActionTamper)
	event.AgentSessionID = r.session.AgentSessionID
	event.ResultStatus = events.ResultSuccess
	event.Kind = events.KindOf(event, false)
	if err := event.SetPayload(payload); err != nil {
		return nil, fmt.Errorf("tamper: encode payload: %w", err)
	}
	if err := r.store.RecordEvent(ctx, event, session.EventCounts(event)); err != nil {
		return nil, fmt.Errorf("tamper: record event: %w", err)
	}

	actionID := uuid.New()
	_, err := r.receipts.Record(ctx, &receipt.RecordInput{
		SessionID: r.session.ID,
		ActionID:  actionID,
		EventID:   event.ID,
		Agent:     session.SystemAgentName,
		Action: &model.Action{
			ID:        actionID,
			Timestamp: event.Timestamp,
			SessionID: r.session.ID,
			EventID:   event.ID,
			Type:      model.ActionTamper,
			Tool:      payload.Asset,
			Operation: payload.Operation(),
			Agent:     session.SystemAgentName,
		},
		Decision: &model.EvaluationResult{
			Decision: receipt.DecisionTamper,
			Severity: tamperSeverity(payload),
			Message:  payload.Summary(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("tamper: record receipt: %w", err)
	}
	return event, nil
}

// lastRecorded returns the latest tamper payload per asset. The system
// session is small, so it reads every tamper event in it.
func (r *TamperRecorder) lastRecorded(ctx context.Context) (map[string]events.TamperPayload, error) {
	rows, err := r.store.QueryEvents(ctx, events.NewEventFilter().
		WithSession(r.session.ID).
		WithActions(events.ActionTamper).
		WithSort(events.SortDesc).
		WithLimit(0))
	if err != nil {
		return nil, fmt.Errorf("tamper: read the system session: %w", err)
	}
	last := map[string]events.TamperPayload{}
	for _, row := range rows {
		var payload events.TamperPayload
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, fmt.Errorf("tamper: decode event %s: %w", row.ID, err)
		}
		key := tamperKey(payload.Asset, payload.Agent)
		if _, seen := last[key]; !seen {
			last[key] = payload
		}
	}
	return last, nil
}

// ensureSession creates the system session row on first use. A concurrent
// creator wins the race, and this recorder uses its row.
func (r *TamperRecorder) ensureSession(ctx context.Context) error {
	existing, err := r.store.GetSession(ctx, r.session.ID)
	if err != nil {
		return fmt.Errorf("tamper: read the system session: %w", err)
	}
	if existing != nil {
		return nil
	}
	if err := r.store.SaveSession(ctx, r.session); err != nil {
		existing, getErr := r.store.GetSession(ctx, r.session.ID)
		if getErr != nil || existing == nil {
			return fmt.Errorf("tamper: create the system session: %w", err)
		}
	}
	return nil
}

func tamperKey(asset, agent string) string { return asset + "/" + agent }

func tamperSeverity(p events.TamperPayload) model.Severity {
	switch p.Operation() {
	case "drift":
		return model.SeverityHigh
	case "level":
		return model.SeverityMedium
	default:
		return model.SeverityInfo
	}
}
