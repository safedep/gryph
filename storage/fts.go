package storage

import (
	"context"
	"fmt"
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
