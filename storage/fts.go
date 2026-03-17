package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/safedep/gryph/core/events"
)

const createFTSTable = `
CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
    event_id UNINDEXED,
    session_id UNINDEXED,
    searchable_text,
    tokenize='porter unicode61'
);`

// InitFTS creates the FTS5 virtual table if it doesn't exist.
func (s *SQLiteStore) InitFTS(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, createFTSTable)
	if err != nil {
		return fmt.Errorf("failed to create FTS table: %w", err)
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

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events_fts(event_id, session_id, searchable_text) VALUES (?, ?, ?)`,
		event.ID.String(), event.SessionID.String(), searchableText,
	)
	if err != nil {
		return fmt.Errorf("failed to index event: %w", err)
	}
	return nil
}
