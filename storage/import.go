package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage/ent/aarmreceipt"
	"github.com/safedep/gryph/storage/ent/auditevent"
)

// ImportEvents implements ImportStore. It inserts the events of one
// session as they are, sequence and content included, marked imported,
// and skips the ones that are already there. It writes no session
// counter: the session row carries the counters of the user's database.
func (s *SQLiteStore) ImportEvents(ctx context.Context, sessionID uuid.UUID, evts []*events.Event) (int, error) {
	if err := s.requireImportedSession(ctx, sessionID); err != nil {
		return 0, err
	}
	taken := 0
	for _, event := range evts {
		if event.SessionID != sessionID {
			return taken, fmt.Errorf("storage: import: event %s belongs to session %s, not %s", event.ID, event.SessionID, sessionID)
		}
		exists, err := s.client.AuditEvent.Query().Where(auditevent.IDEQ(event.ID)).Exist(ctx)
		if err != nil {
			return taken, fmt.Errorf("storage: import: %w", err)
		}
		if exists {
			continue
		}
		event.Imported = true
		if err := s.SaveEvent(ctx, event); err != nil {
			return taken, err
		}
		taken++
	}
	return taken, nil
}

// ImportReceipts implements ImportStore. A receipt keeps its hash, its
// signature and its key: the chain of the session is the one the user's
// database held.
func (s *SQLiteStore) ImportReceipts(ctx context.Context, sessionID uuid.UUID, rows []*ReceiptRow) (int, error) {
	if err := s.requireImportedSession(ctx, sessionID); err != nil {
		return 0, err
	}
	taken := 0
	for _, row := range rows {
		if row.SessionID != sessionID {
			return taken, fmt.Errorf("storage: import: receipt %d belongs to session %s, not %s", row.Sequence, row.SessionID, sessionID)
		}
		exists, err := s.client.AarmReceipt.Query().
			Where(aarmreceipt.SessionIDEQ(sessionID), aarmreceipt.SequenceEQ(row.Sequence)).
			Exist(ctx)
		if err != nil {
			return taken, fmt.Errorf("storage: import: %w", err)
		}
		if exists {
			continue
		}
		row.Imported = true
		if err := s.InsertReceipt(ctx, row); err != nil {
			return taken, err
		}
		taken++
	}
	return taken, nil
}

// ImportSession implements ImportStore. It comes first: the events and
// the receipts reference the session row. It reports false when the
// session is already there, and the rows of that session then still
// come in, so a run that stopped gets completed.
func (s *SQLiteStore) ImportSession(ctx context.Context, sess *session.Session) (bool, error) {
	existing, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		return false, err
	}
	if existing != nil {
		if !existing.Imported {
			return false, fmt.Errorf("storage: import: session %s is one this store recorded itself", sess.ID)
		}
		return false, nil
	}
	sess.Imported = true
	if err := s.SaveSession(ctx, sess); err != nil {
		return false, err
	}
	if sess.HasCostData() || sess.TranscriptPath != "" {
		if err := s.UpdateSession(ctx, sess); err != nil {
			return false, err
		}
	}
	return true, nil
}

// requireImportedSession admits rows into an imported session only. A
// session that this store recorded itself refuses them: the user's rows
// must not join a chain of the service. A session that is not there yet
// needs its row first.
func (s *SQLiteStore) requireImportedSession(ctx context.Context, sessionID uuid.UUID) error {
	existing, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("storage: import: session %s has no row yet, import the session first", sessionID)
	}
	if !existing.Imported {
		return fmt.Errorf("storage: import: session %s is one this store recorded itself", sessionID)
	}
	return nil
}
