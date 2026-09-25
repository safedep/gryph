package accumulator

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/canonical"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/storage"
)

// Window implements Accumulator. It reads the latest spec.MaxEntries
// entries, and adds the latest intent when it is older. With
// IncludeContent it loads the audit events in one query and projects their
// content: a value stays only when Gryph stored it at the full level. A
// preview stored at standard never reaches a window, even after the config
// changes to full. MaxBytes keeps the latest intent first, then the newest
// entries.
func (a *SQLiteAccumulator) Window(ctx context.Context, sessionID uuid.UUID, spec model.WindowSpec) (*model.Window, error) {
	if a == nil || a.store == nil {
		return nil, fmt.Errorf("accumulator: store is not initialized")
	}
	kinds := make([]string, 0, len(spec.Kinds))
	for _, k := range spec.Kinds {
		kinds = append(kinds, string(k))
	}
	rows, err := a.store.QueryContextEntries(ctx, &storage.ContextEntryFilter{
		SessionID: &sessionID, Kinds: kinds, Limit: min(max(spec.MaxEntries, 1), config.MaxWindowEntries),
	})
	if err != nil {
		return nil, err
	}
	intent, err := a.latestIntent(ctx, sessionID, kinds, rows)
	if err != nil {
		return nil, err
	}
	if intent != nil {
		rows = append(rows, intent)
	}
	slices.SortFunc(rows, func(a, b *storage.ContextEntryRow) int { return cmp.Compare(a.Sequence, b.Sequence) })

	w := &model.Window{SessionID: sessionID, Entries: make([]model.WindowEntry, 0, len(rows))}
	for _, r := range rows {
		w.Entries = append(w.Entries, model.WindowEntry{Entry: entryFromRow(r)})
	}
	if spec.IncludeContent {
		if err := a.loadContent(ctx, w); err != nil {
			return nil, err
		}
		w.Truncated = fitBytes(w, spec.MaxBytes)
	}
	return w, nil
}

// latestIntent returns the latest intent that reached the agent when rows
// do not hold it and the kinds allow it.
func (a *SQLiteAccumulator) latestIntent(ctx context.Context, sessionID uuid.UUID, kinds []string, rows []*storage.ContextEntryRow) (*storage.ContextEntryRow, error) {
	if len(kinds) > 0 && !slices.Contains(kinds, string(events.KindIntent)) {
		return nil, nil
	}
	state, err := a.store.GetContextState(ctx, sessionID)
	if err != nil || state == nil || state.LastIntentSeq == nil {
		return nil, err
	}
	seq := *state.LastIntentSeq
	if slices.ContainsFunc(rows, func(r *storage.ContextEntryRow) bool { return r.Sequence == seq }) {
		return nil, nil
	}
	found, err := a.store.QueryContextEntries(ctx, &storage.ContextEntryFilter{SessionID: &sessionID, Sequence: &seq, Limit: 1})
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

func (a *SQLiteAccumulator) loadContent(ctx context.Context, w *model.Window) error {
	ids := make([]uuid.UUID, 0, len(w.Entries))
	for _, e := range w.Entries {
		if e.Entry.EventID != uuid.Nil {
			ids = append(ids, e.Entry.EventID)
		}
	}
	evts, err := a.store.QueryEventsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]*events.Event, len(evts))
	for _, e := range evts {
		byID[e.ID] = e
	}
	for i := range w.Entries {
		if e, ok := byID[w.Entries[i].Entry.EventID]; ok {
			w.Entries[i].Content = projectContent(e)
		}
	}
	return nil
}

// projectContent returns the content values of an event. A value stays only
// when its label says Gryph stored it at the full level.
func projectContent(e *events.Event) []privacy.Text {
	var out []privacy.Text
	add := func(_ string, t *privacy.Text) {
		if t.IsZero() {
			return
		}
		projected := *t
		if projected.Label.Level != string(config.LoggingFull) {
			projected.Value = ""
		}
		out = append(out, projected)
	}
	add("diff_content", &e.DiffContent)
	p, err := e.DecodePayload()
	if err != nil {
		log.Warnf("accumulator: decode payload of event %s: %v", e.ID, err)
		return out
	}
	if p != nil {
		privacy.Walk(p, add)
	}
	return out
}

// fitBytes keeps the content values that fit maxBytes and empties the
// rest. The latest intent comes first, then the entries from the newest to
// the oldest. A value that does not fit is emptied, and the next smaller
// values can still use the budget. Labels and entries do not count. It
// reports whether it removed a value. A maxBytes of zero or less keeps
// everything.
func fitBytes(w *model.Window, maxBytes int) bool {
	if maxBytes <= 0 {
		return false
	}
	order := make([]int, 0, len(w.Entries))
	intent := -1
	for i := len(w.Entries) - 1; i >= 0; i-- {
		if w.Entries[i].Entry.Kind == events.KindIntent {
			intent = i
			break
		}
	}
	if intent >= 0 {
		order = append(order, intent)
	}
	for i := len(w.Entries) - 1; i >= 0; i-- {
		if i != intent {
			order = append(order, i)
		}
	}

	budget := maxBytes
	truncated := false
	for _, i := range order {
		for j := range w.Entries[i].Content {
			t := &w.Entries[i].Content[j]
			if t.Value == "" {
				continue
			}
			if len(t.Value) <= budget {
				budget -= len(t.Value)
				continue
			}
			t.Value = ""
			truncated = true
		}
	}
	return truncated
}

// CanonicalWindow serializes a window with sorted object keys, so the same
// window gives the same bytes.
func CanonicalWindow(w *model.Window) ([]byte, error) {
	data, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("accumulator: marshal window: %w", err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("accumulator: decode window: %w", err)
	}
	return canonical.MarshalJSON(v)
}

// entryFromRow converts a stored entry into the model entry.
func entryFromRow(r *storage.ContextEntryRow) model.ContextEntry {
	e := model.ContextEntry{
		ID:            r.ID,
		SessionID:     r.SessionID,
		EventID:       r.EventID,
		LinkedEventID: r.LinkedEventID,
		Sequence:      r.Sequence,
		Kind:          events.Kind(r.Kind),
		Timestamp:     r.Timestamp,
		ActionType:    model.ActionType(r.ActionType),
		Tool:          r.Tool,
		ToolCallID:    r.ToolCallID,
		Phase:         model.ActionPhase(r.Phase),
		Target: model.DerivedTarget{
			Host: r.TargetHost, MCPServer: r.TargetMCPServer, MCPTool: r.TargetMCPTool,
		},
		Origin:         privacy.Origin(r.Origin),
		Tags:           r.Tags,
		Decision:       model.Decision(r.Decision),
		MatchedRuleIDs: r.MatchedRuleIDs,
		ContentDigest:  r.ContentDigest,
		Result:         model.ResultStatus(r.ResultStatus),
	}
	for _, c := range r.Classifications {
		e.Classifications = append(e.Classifications, privacy.Class(c))
	}
	if r.InjectionScore != nil {
		e.InjectionScore = *r.InjectionScore
	}
	return e
}
