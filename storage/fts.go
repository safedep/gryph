package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage/ent/auditevent"
)

const createFTSTable = `
CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
    event_id UNINDEXED,
    session_id UNINDEXED,
    searchable_text,
    tokenize='porter unicode61'
);`

const createFTSMetaTable = `
CREATE TABLE IF NOT EXISTS events_fts_meta (
    key TEXT PRIMARY KEY,
    value TEXT
);`

// InitFTS creates the FTS5 virtual table if it doesn't exist.
func (s *SQLiteStore) InitFTS(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, createFTSTable)
	if err != nil {
		return fmt.Errorf("failed to create FTS table: %w", err)
	}
	_, err = s.db.ExecContext(ctx, createFTSMetaTable)
	if err != nil {
		return fmt.Errorf("failed to create FTS meta table: %w", err)
	}
	return nil
}

func buildSearchableText(event *events.Event) string {
	var parts []string

	if event.ToolName != "" {
		parts = append(parts, event.ToolName)
	}
	if event.ErrorMessage != "" {
		parts = append(parts, event.ErrorMessage)
	}
	if event.DiffContent != "" {
		parts = append(parts, event.DiffContent)
	}
	if event.ConversationContext != "" {
		parts = append(parts, event.ConversationContext)
	}

	payloadText := extractPayloadText(event)
	if payloadText != "" {
		parts = append(parts, payloadText)
	}

	return strings.Join(parts, "\n")
}

func extractPayloadText(event *events.Event) string {
	if len(event.Payload) == 0 {
		return ""
	}

	var parts []string

	switch event.ActionType {
	case events.ActionFileRead:
		if p, err := event.GetFileReadPayload(); err == nil && p != nil {
			parts = append(parts, p.Path)
		}
	case events.ActionFileWrite:
		if p, err := event.GetFileWritePayload(); err == nil && p != nil {
			parts = append(parts, p.Path)
		}
	case events.ActionFileDelete:
		if p, err := event.GetFileDeletePayload(); err == nil && p != nil {
			parts = append(parts, p.Path)
		}
	case events.ActionCommandExec:
		if p, err := event.GetCommandExecPayload(); err == nil && p != nil {
			parts = append(parts, p.Command)
			if p.Description != "" {
				parts = append(parts, p.Description)
			}
			if p.StdoutPreview != "" {
				parts = append(parts, p.StdoutPreview)
			}
			if p.StderrPreview != "" {
				parts = append(parts, p.StderrPreview)
			}
		}
	case events.ActionToolUse:
		if p, err := event.GetToolUsePayload(); err == nil && p != nil {
			parts = append(parts, p.ToolName)
			if p.OutputPreview != "" {
				parts = append(parts, p.OutputPreview)
			}
			extractRawJSONStrings(p.Input, &parts)
		}
	case events.ActionNotification:
		if p, err := event.GetNotificationPayload(); err == nil && p != nil {
			parts = append(parts, p.Message)
		}
	}

	return strings.Join(parts, "\n")
}

func extractRawJSONStrings(raw json.RawMessage, parts *[]string) {
	if len(raw) == 0 {
		return
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}
	for _, v := range m {
		if val, ok := v.(string); ok && val != "" {
			*parts = append(*parts, val)
		}
	}
}

func (s *SQLiteStore) indexEvent(ctx context.Context, event *events.Event) error {
	searchableText := buildSearchableText(event)
	if searchableText == "" {
		return nil
	}

	// Check if already indexed (FTS5 has no unique constraint)
	var exists int
	_ = s.db.QueryRowContext(ctx,
		"SELECT 1 FROM events_fts WHERE event_id = ? LIMIT 1",
		event.ID.String(),
	).Scan(&exists)
	if exists == 1 {
		return nil
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events_fts(event_id, session_id, searchable_text) VALUES (?, ?, ?)`,
		event.ID.String(), event.SessionID.String(), searchableText,
	)
	if err != nil {
		return fmt.Errorf("failed to index event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) cleanFTSBefore(ctx context.Context, before time.Time) {
	evts, err := s.client.AuditEvent.Query().
		Where(auditevent.TimestampLT(before)).
		Select(auditevent.FieldID).
		All(ctx)
	if err != nil || len(evts) == 0 {
		return
	}

	for _, e := range evts {
		_, _ = s.db.ExecContext(ctx,
			`DELETE FROM events_fts WHERE event_id = ?`, e.ID.String(),
		)
	}
}

const backfillBatchSize = 500

func (s *SQLiteStore) BackfillFTS(ctx context.Context, store EventStore) (int, error) {
	// Check if backfill already completed
	var done string
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM events_fts_meta WHERE key = 'backfill_done'",
	).Scan(&done)
	if err == nil && done == "1" {
		return 0, nil
	}

	indexed := 0
	offset := 0
	for {
		filter := events.NewEventFilter().WithLimit(backfillBatchSize).WithOffset(offset)
		batch, err := store.QueryEvents(ctx, filter)
		if err != nil {
			return indexed, fmt.Errorf("failed to fetch events for backfill: %w", err)
		}
		if len(batch) == 0 {
			break
		}

		for _, evt := range batch {
			if err := s.indexEvent(ctx, evt); err != nil {
				continue
			}
			indexed++
		}

		offset += len(batch)
		if len(batch) < backfillBatchSize {
			break
		}
	}

	// Mark backfill complete
	_, _ = s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO events_fts_meta (key, value) VALUES ('backfill_done', '1')")

	return indexed, nil
}

func (s *SQLiteStore) SearchEvents(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	ftsQuery := escapeFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT f.event_id, f.session_id,
            snippet(events_fts, 2, '>>>', '<<<', '...', 32) as snippet,
            rank
        FROM events_fts f
        WHERE events_fts MATCH ?
        ORDER BY rank
        LIMIT ?
    `, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search events: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warnf("failed to close rows: %v", err)
		}
	}()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var eventIDStr, sessionIDStr string
		if err := rows.Scan(&eventIDStr, &sessionIDStr, &r.Snippet, &r.Rank); err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}
		var parseErr error
		r.EventID, parseErr = uuid.Parse(eventIDStr)
		if parseErr != nil {
			continue
		}
		r.SessionID, parseErr = uuid.Parse(sessionIDStr)
		if parseErr != nil {
			continue
		}
		results = append(results, r)
	}

	return results, rows.Err()
}

// escapeFTSQuery wraps each token in double quotes so FTS5 operators
// like - * OR AND are treated as literals.
func escapeFTSQuery(query string) string {
	// Strip null bytes which cause SQLite string termination issues
	query = strings.ReplaceAll(query, "\x00", "")
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return ""
	}
	var quoted []string
	for _, t := range tokens {
		t = strings.ReplaceAll(t, `"`, `""`)
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " ")
}

func (s *SQLiteStore) HasSearch() bool {
	var name string
	err := s.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='events_fts'",
	).Scan(&name)
	return err == nil && name == "events_fts"
}

func (s *SQLiteStore) DistinctAgents(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT agent_name FROM sessions ORDER BY agent_name")
	if err != nil {
		return nil, fmt.Errorf("failed to query distinct agents: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warnf("failed to close rows: %v", err)
		}
	}()

	var agents []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan agent name: %w", err)
		}
		agents = append(agents, name)
	}

	return agents, rows.Err()
}

var _ Searcher = (*SQLiteStore)(nil)
