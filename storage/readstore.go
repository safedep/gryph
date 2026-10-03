package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// ReadStore is the part of the store that the read commands use: logs,
// query, sessions, session, cat, diff, stats, cost, policy receipts,
// policy approve and policy deferrals. It is the only part of the store
// that goes over the socket to the decision service, so a developer on a
// managed host reads their own partition and nothing else. The full Store
// stays in process.
type ReadStore interface {
	GetEvent(ctx context.Context, id uuid.UUID) (*events.Event, error)
	GetEventByPrefix(ctx context.Context, prefix string) (*events.Event, error)
	QueryEvents(ctx context.Context, filter *events.EventFilter) ([]*events.Event, error)
	CountEvents(ctx context.Context, filter *events.EventFilter) (int, error)
	GetEventsBySession(ctx context.Context, sessionID uuid.UUID) ([]*events.Event, error)
	QueryEventsAfter(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]*events.Event, error)

	GetSession(ctx context.Context, id uuid.UUID) (*session.Session, error)
	GetSessionByPrefix(ctx context.Context, prefix string) (*session.Session, error)
	QuerySessions(ctx context.Context, filter *session.SessionFilter) ([]*session.Session, error)

	QueryReceipts(ctx context.Context, filter *ReceiptFilter) ([]*ReceiptRow, error)
	ListReceiptSessionIDs(ctx context.Context) ([]uuid.UUID, error)

	QueryDeferredActions(ctx context.Context, filter *DeferredActionFilter) ([]*DeferredActionRow, error)
	GetDeferredActionByPrefix(ctx context.Context, prefix string) (*DeferredActionRow, error)

	QueryApprovalRequests(ctx context.Context, filter *ApprovalRequestFilter) ([]*ApprovalRequestRow, error)
	GetApprovalRequestByPrefix(ctx context.Context, prefix string) (*ApprovalRequestRow, error)

	// GetContextStateByPrefix resolves a session reference of the policy
	// commands, which take a context state id as well as a session id.
	GetContextStateByPrefix(ctx context.Context, prefix string) (*ContextStateRow, error)
	GetContextState(ctx context.Context, sessionID uuid.UUID) (*ContextStateRow, error)
	QueryAllContextStates(ctx context.Context, limit int) ([]*ContextStateRow, error)
	QueryContextEntries(ctx context.Context, filter *ContextEntryFilter) ([]*ContextEntryRow, error)
	ListContextSessionIDs(ctx context.Context) ([]uuid.UUID, error)
}

var _ ReadStore = (*SQLiteStore)(nil)
