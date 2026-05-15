// Package storage provides database storage interfaces and implementations.
package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
)

// EventStore defines the interface for storing and querying audit events.
type EventStore interface {
	// SaveEvent persists a new audit event.
	SaveEvent(ctx context.Context, event *events.Event) error

	// GetEvent retrieves an event by ID.
	GetEvent(ctx context.Context, id uuid.UUID) (*events.Event, error)

	// GetEventByPrefix retrieves an event by ID prefix.
	GetEventByPrefix(ctx context.Context, prefix string) (*events.Event, error)

	// QueryEvents retrieves events matching the given filter.
	QueryEvents(ctx context.Context, filter *events.EventFilter) ([]*events.Event, error)

	// CountEvents returns the count of events matching the given filter.
	CountEvents(ctx context.Context, filter *events.EventFilter) (int, error)

	// GetEventsBySession retrieves all events for a session.
	GetEventsBySession(ctx context.Context, sessionID uuid.UUID) ([]*events.Event, error)

	// DeleteEventsBefore deletes events older than the given time.
	DeleteEventsBefore(ctx context.Context, before time.Time) (int, error)

	// CountEventsBefore returns the count of events older than the given time.
	CountEventsBefore(ctx context.Context, before time.Time) (int, error)

	// QueryEventsAfter retrieves events after the given time, ordered ascending.
	// When afterID is non-nil, a compound cursor (timestamp, id) is used so that
	// events sharing the same timestamp as after are only included when their ID
	// is greater than afterID. This prevents skipping records at batch boundaries.
	QueryEventsAfter(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]*events.Event, error)
}

// SessionStore defines the interface for storing and querying sessions.
type SessionStore interface {
	// SaveSession persists a new session.
	SaveSession(ctx context.Context, sess *session.Session) error

	// UpdateSession updates an existing session.
	UpdateSession(ctx context.Context, sess *session.Session) error

	// GetSession retrieves a session by ID.
	GetSession(ctx context.Context, id uuid.UUID) (*session.Session, error)

	// GetSessionByPrefix retrieves a session by ID prefix.
	GetSessionByPrefix(ctx context.Context, prefix string) (*session.Session, error)

	// QuerySessions retrieves sessions matching the given filter.
	QuerySessions(ctx context.Context, filter *session.SessionFilter) ([]*session.Session, error)

	// GetActiveSession retrieves the active session for an agent, if any.
	GetActiveSession(ctx context.Context, agentName string) (*session.Session, error)

	// GetSessionStats retrieves aggregated session statistics.
	GetSessionStats(ctx context.Context) (*session.SessionStats, error)
}

// SelfAuditStore defines the interface for storing self-audit entries.
type SelfAuditStore interface {
	// SaveSelfAudit persists a self-audit entry.
	SaveSelfAudit(ctx context.Context, entry *SelfAuditEntry) error

	// QuerySelfAudits retrieves self-audit entries matching the filter.
	QuerySelfAudits(ctx context.Context, filter *SelfAuditFilter) ([]*SelfAuditEntry, error)

	// QuerySelfAuditsAfter retrieves self-audit entries after the given time, ordered ascending.
	// When afterID is non-nil, a compound cursor (timestamp, id) is used to avoid
	// skipping records that share the same timestamp at batch boundaries.
	QuerySelfAuditsAfter(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]*SelfAuditEntry, error)
}

// StreamCursorStore defines the interface for stream sync cursors.
type StreamCursorStore interface {
	GetEventCursor(ctx context.Context, targetName string) (*StreamCursor, error)
	SaveEventCursor(ctx context.Context, cursor *StreamCursor) error
	GetAuditCursor(ctx context.Context, targetName string) (*StreamCursor, error)
	SaveAuditCursor(ctx context.Context, cursor *StreamCursor) error
}

// ContextStore defines the interface for the AARM Context Accumulator's
// persistent storage. AppendContextAction is responsible for the atomic
// per-session counter update on the state row. Implementations must keep
// the counter UPSERT race-free across concurrent same-session writes.
type ContextStore interface {
	AppendContextAction(ctx context.Context, row *ContextActionRow) error
	UpdateContextActionResult(ctx context.Context, actionID uuid.UUID, status string, durationMS int64, errorMsg string) error
	GetContextState(ctx context.Context, sessionID uuid.UUID) (*ContextStateRow, error)
	GetContextStateByPrefix(ctx context.Context, prefix string) (*ContextStateRow, error)
	QueryContextActions(ctx context.Context, sessionID uuid.UUID, limit int) ([]*ContextActionRow, error)
	QueryAllContextStates(ctx context.Context, limit int) ([]*ContextStateRow, error)
	DeleteContextBefore(ctx context.Context, before time.Time) (int, error)
	CountContextBefore(ctx context.Context, before time.Time) (int, error)
}

// ContextActionRow is the storage-layer representation of a single mediated
// action recorded by the Context Accumulator. ResultStatus is "pending" at
// append time and transitions to one of "success", "error", "blocked", or
// "rejected" via UpdateContextActionResult.
type ContextActionRow struct {
	ID                  uuid.UUID
	SessionID           uuid.UUID
	EventID             uuid.UUID
	Timestamp           time.Time
	ActionType          string
	Tool                string
	Agent               string
	Project             string
	WorkingDir          string
	ResultStatus        string
	DurationMS          *int64
	ErrorMessage        string
	DataClassifications []string
	InjectionScore      *float32
}

// ReceiptStore defines the interface for the AARM receipt log: an
// append-only, hash-chained record per session. InsertReceipt is called
// inside the generator's transaction with a pre-computed sequence and hash;
// GetLastReceiptForSession returns the prior (sequence, hash) the generator
// needs to chain onto. RecordReceiptInTx atomically reads the prior row,
// hands it to the caller-supplied builder, and inserts the returned row
// inside a single writer transaction so concurrent same-session writers
// cannot observe the same Sequence.
type ReceiptStore interface {
	InsertReceipt(ctx context.Context, row *ReceiptRow) error
	GetLastReceiptForSession(ctx context.Context, sessionID uuid.UUID) (*ReceiptRow, error)
	RecordReceiptInTx(ctx context.Context, sessionID uuid.UUID, build func(prev *ReceiptRow) (*ReceiptRow, error)) (*ReceiptRow, error)
	UpdateReceiptResult(ctx context.Context, sessionID uuid.UUID, sequence int64, status string, durationMS int64, errorMsg string) error
	QueryReceipts(ctx context.Context, filter *ReceiptFilter) ([]*ReceiptRow, error)
	CountReceipts(ctx context.Context, filter *ReceiptFilter) (int, error)
	DeleteReceiptsBefore(ctx context.Context, before time.Time) (int, error)
	CountReceiptsBefore(ctx context.Context, before time.Time) (int, error)
	ListReceiptSessionIDs(ctx context.Context) ([]uuid.UUID, error)
}

// ReceiptFilter narrows QueryReceipts and CountReceipts.
//
// Limit semantics:
//   - Limit > 0: return up to Limit rows, capped at receiptListMaxLimit
//     (defined in the storage package).
//   - Limit == 0 (or unset): treat as the default cap (receiptListMaxLimit).
//   - Limit == -1: unbounded. No LIMIT clause is applied. Intended for
//     admin operations such as full hash-chain verification where the
//     entire session must be loaded regardless of size. Not for hot paths.
type ReceiptFilter struct {
	SessionID *uuid.UUID
	Decision  string
	Since     *time.Time
	Until     *time.Time
	Limit     int
}

// ReceiptRow is the storage-layer representation of a single receipt entry.
// Mirrors the ent schema fields. Snapshot and ActionPayload are JSON-decoded
// maps. PrevHash is empty for the first receipt of a session.
type ReceiptRow struct {
	ID             uuid.UUID
	SessionID      uuid.UUID
	ActionID       uuid.UUID
	EventID        uuid.UUID
	RecordedAt     time.Time
	Sequence       int64
	Agent          string
	Tool           string
	ActionType     string
	Project        string
	Decision       string
	MatchedRuleIDs []string
	Severity       string
	Message        string
	ResultStatus   string
	DurationMS     *int64
	ErrorMessage   string
	Snapshot       map[string]interface{}
	ActionPayload  map[string]interface{}
	PrevHash       []byte
	Hash           []byte
}

// ContextStateRow is the storage-layer representation of the per-session
// counter row used by the PDP to populate the context.* CEL surface.
type ContextStateRow struct {
	SessionID           uuid.UUID
	FirstSeenAt         time.Time
	LastActionAt        time.Time
	TotalActions        int
	FilesRead           int
	FilesWritten        int
	CommandsExecuted    int
	NetworkRequests     int
	Errors              int
	ToolsUsed           []string
	ClassificationsSeen []string
	EntitiesSeen        []string
	SemanticDrift       float64
}

// StreamCursor represents the sync cursor for a single collection (events or audits).
type StreamCursor struct {
	TargetName   string
	LastSyncedAt time.Time
	LastID       string
}

// SelfAuditEntry represents a self-audit log entry for storage.
type SelfAuditEntry struct {
	ID           uuid.UUID
	Timestamp    time.Time
	Action       string
	AgentName    string
	Details      map[string]interface{}
	Result       string
	ErrorMessage string
	ToolVersion  string
}

// SelfAuditFilter provides filtering for self-audit queries.
type SelfAuditFilter struct {
	Since  *time.Time
	Action string
	Limit  int
}

// Store combines all storage interfaces.
type Store interface {
	EventStore
	SessionStore
	SelfAuditStore
	StreamCursorStore
	ContextStore
	ReceiptStore

	// Init initializes the database schema.
	Init(ctx context.Context) error

	// Close closes the database connection.
	Close() error
}

// SearchResult holds a single FTS match.
type SearchResult struct {
	EventID   uuid.UUID
	SessionID uuid.UUID
	Snippet   string
	Rank      float64
}

// Searcher provides full-text search and discovery capabilities.
// SQLite-specific; the TUI accepts this as optional via Options.
type Searcher interface {
	SearchEvents(ctx context.Context, query string, limit int) ([]SearchResult, error)
	HasSearch() bool
	BackfillFTS(ctx context.Context, store EventStore) (int, error)
	DistinctAgents(ctx context.Context) ([]string, error)
}

// DatabaseInfo contains information about the database.
type DatabaseInfo struct {
	Path         string
	SizeBytes    int64
	EventCount   int
	SessionCount int
	OldestEvent  time.Time
	NewestEvent  time.Time
}
