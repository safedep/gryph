package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	history, err := r.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	return r.recordChanges(ctx, history, statuses)
}

func (r *TamperRecorder) recordChanges(ctx context.Context, history *tamperHistory, statuses []selfprotect.AssetStatus) ([]*events.Event, error) {
	var recorded []*events.Event
	for _, s := range statuses {
		payload := newTamperPayload(s)
		prev, seen := history.latest[tamperKey(s.Ref())]
		switch {
		case !seen && s.Drift == "":
			continue
		case seen && prev.Drift == payload.Drift && prev.LevelAfter == payload.LevelAfter:
			continue
		case seen:
			payload.LevelBefore = prev.LevelAfter
		}
		switch {
		case payload.Drift != "":
			payload.Operation = events.TamperDrift
		case seen && prev.Drift != "":
			payload.Operation = events.TamperResolved
		default:
			payload.Operation = events.TamperLevel
		}
		event, err := r.record(ctx, history, payload)
		if err != nil {
			return recorded, err
		}
		recorded = append(recorded, event)
	}
	return recorded, nil
}

// newTamperPayload returns the payload of a status with no change recorded
// yet. The caller sets the operation and the level before.
func newTamperPayload(s selfprotect.AssetStatus) events.TamperPayload {
	return events.TamperPayload{
		Asset:       string(s.Asset),
		Agent:       s.Agent,
		LevelBefore: s.Level.String(),
		LevelAfter:  s.Level.String(),
		Drift:       s.Drift,
		Provider:    s.Provider,
		Detail:      s.Detail,
	}
}

// Record writes one tamper event and its receipt.
func (r *TamperRecorder) Record(ctx context.Context, payload events.TamperPayload) (*events.Event, error) {
	return r.record(ctx, nil, payload)
}

func (r *TamperRecorder) record(ctx context.Context, history *tamperHistory, payload events.TamperPayload) (*events.Event, error) {
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
			Operation: payload.Operation,
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
	if history != nil {
		history.add(event.Timestamp, payload)
	}
	return event, nil
}

// tamperHistory is the recorded state of the system session: the latest
// payload per asset and the time of every repair attempt.
type tamperHistory struct {
	latest   map[string]events.TamperPayload
	attempts map[string][]time.Time
}

func newTamperHistory() *tamperHistory {
	return &tamperHistory{latest: map[string]events.TamperPayload{}, attempts: map[string][]time.Time{}}
}

// add records a payload that is newer than every one before it.
func (h *tamperHistory) add(at time.Time, p events.TamperPayload) {
	key := tamperKey(selfprotect.AssetRef{Asset: selfprotect.Asset(p.Asset), Agent: p.Agent})
	h.latest[key] = p
	if p.Operation == events.TamperRepair || p.Operation == events.TamperRepairFailed {
		h.attempts[key] = append(h.attempts[key], at)
	}
}

// repairAttempts returns the count of repairs of one asset, failed or not,
// since the given time.
func (h *tamperHistory) repairAttempts(ref selfprotect.AssetRef, since time.Time) int {
	n := 0
	for _, at := range h.attempts[tamperKey(ref)] {
		if !at.Before(since) {
			n++
		}
	}
	return n
}

// loadHistory reads every tamper event in the system session, oldest
// first. The system session is small.
func (r *TamperRecorder) loadHistory(ctx context.Context) (*tamperHistory, error) {
	rows, err := r.store.QueryEvents(ctx, events.NewEventFilter().
		WithSession(r.session.ID).
		WithActions(events.ActionTamper).
		WithSort(events.SortAsc).
		WithLimit(0))
	if err != nil {
		return nil, fmt.Errorf("tamper: read the system session: %w", err)
	}
	history := newTamperHistory()
	for _, row := range rows {
		var payload events.TamperPayload
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, fmt.Errorf("tamper: decode event %s: %w", row.ID, err)
		}
		history.add(row.Timestamp, payload)
	}
	return history, nil
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

func tamperKey(ref selfprotect.AssetRef) string { return string(ref.Asset) + "/" + ref.Agent }

func tamperSeverity(p events.TamperPayload) model.Severity {
	switch p.Operation {
	case events.TamperDrift, events.TamperRepairFailed:
		return model.SeverityHigh
	case events.TamperLevel, events.TamperRateLimited:
		return model.SeverityMedium
	default:
		return model.SeverityInfo
	}
}
