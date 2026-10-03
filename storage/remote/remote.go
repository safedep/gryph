// Package remote is the read store of a managed host: every read goes to
// the decision service over the socket, and the service answers from the
// partition of the account that the kernel reports for the connection. No
// request names a partition. It implements storage.ReadStore and nothing
// more, so a client cannot write the store or read another account's.
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/storage"
)

// The query kinds. Each names one method of storage.ReadStore.
const (
	KindEvent                   = "event"
	KindEventByPrefix           = "event_by_prefix"
	KindEvents                  = "events"
	KindCountEvents             = "count_events"
	KindSessionEvents           = "session_events"
	KindEventsAfter             = "events_after"
	KindSession                 = "session"
	KindSessionByPrefix         = "session_by_prefix"
	KindSessions                = "sessions"
	KindReceipts                = "receipts"
	KindReceiptSessionIDs       = "receipt_session_ids"
	KindDeferredActions         = "deferred_actions"
	KindDeferredActionByPrefix  = "deferred_action_by_prefix"
	KindContextStateByPrefix    = "context_state_by_prefix"
	KindApprovalRequests        = "approval_requests"
	KindApprovalRequestByPrefix = "approval_request_by_prefix"
)

// The query parameters.
const (
	ParamID     = "id"
	ParamPrefix = "prefix"
	ParamFilter = "filter"
	ParamPage   = "page"
	ParamAfter  = "after"
	ParamLimit  = "limit"
	// ParamAll asks for the rows of every account, for an approver.
	ParamAll = "all"
)

// PageSize is the count of rows in one query_result. A longer answer
// comes in pages: the client asks for the next page with the cursor of
// the last one.
const PageSize = ipc.MaxQueryItems

// Store is the remote read store over one connection.
type Store struct {
	client *ipc.Client
}

var _ storage.ReadStore = (*Store)(nil)

// New returns the read store over client.
func New(client *ipc.Client) *Store {
	return &Store{client: client}
}

// Client returns the connection, for the one write the read commands
// make: the cost totals of a session.
func (s *Store) Client() *ipc.Client { return s.client }

func (s *Store) GetEvent(ctx context.Context, id uuid.UUID) (*events.Event, error) {
	return one[events.Event](ctx, s, ipc.Query{Kind: KindEvent, Params: map[string]string{ParamID: id.String()}})
}

func (s *Store) GetEventByPrefix(ctx context.Context, prefix string) (*events.Event, error) {
	return one[events.Event](ctx, s, ipc.Query{Kind: KindEventByPrefix, Params: map[string]string{ParamPrefix: prefix}})
}

func (s *Store) QueryEvents(ctx context.Context, filter *events.EventFilter) ([]*events.Event, error) {
	return many[events.Event](ctx, s, KindEvents, filter)
}

func (s *Store) CountEvents(ctx context.Context, filter *events.EventFilter) (int, error) {
	n, err := one[countRow](ctx, s, ipc.Query{Kind: KindCountEvents, Params: map[string]string{ParamFilter: encode(filter)}})
	if err != nil {
		return 0, err
	}
	if n == nil {
		return 0, fmt.Errorf("remote: count without a row")
	}
	return n.Count, nil
}

func (s *Store) GetEventsBySession(ctx context.Context, sessionID uuid.UUID) ([]*events.Event, error) {
	return pages[events.Event](ctx, s, ipc.Query{Kind: KindSessionEvents, Params: map[string]string{ParamID: sessionID.String()}})
}

func (s *Store) QueryEventsAfter(ctx context.Context, after time.Time, afterID uuid.UUID, limit int) ([]*events.Event, error) {
	params := map[string]string{ParamAfter: after.UTC().Format(time.RFC3339Nano), ParamLimit: strconv.Itoa(limit)}
	if afterID != uuid.Nil {
		params[ParamID] = afterID.String()
	}
	return pages[events.Event](ctx, s, ipc.Query{Kind: KindEventsAfter, Params: params})
}

func (s *Store) GetSession(ctx context.Context, id uuid.UUID) (*session.Session, error) {
	return one[session.Session](ctx, s, ipc.Query{Kind: KindSession, Params: map[string]string{ParamID: id.String()}})
}

func (s *Store) GetSessionByPrefix(ctx context.Context, prefix string) (*session.Session, error) {
	return one[session.Session](ctx, s, ipc.Query{Kind: KindSessionByPrefix, Params: map[string]string{ParamPrefix: prefix}})
}

func (s *Store) QuerySessions(ctx context.Context, filter *session.SessionFilter) ([]*session.Session, error) {
	return many[session.Session](ctx, s, KindSessions, filter)
}

func (s *Store) QueryReceipts(ctx context.Context, filter *storage.ReceiptFilter) ([]*storage.ReceiptRow, error) {
	return many[storage.ReceiptRow](ctx, s, KindReceipts, filter)
}

func (s *Store) ListReceiptSessionIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := pages[uuid.UUID](ctx, s, ipc.Query{Kind: KindReceiptSessionIDs})
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out, nil
}

func (s *Store) QueryDeferredActions(ctx context.Context, filter *storage.DeferredActionFilter) ([]*storage.DeferredActionRow, error) {
	return many[storage.DeferredActionRow](ctx, s, KindDeferredActions, filter)
}

func (s *Store) GetDeferredActionByPrefix(ctx context.Context, prefix string) (*storage.DeferredActionRow, error) {
	return one[storage.DeferredActionRow](ctx, s, ipc.Query{Kind: KindDeferredActionByPrefix, Params: map[string]string{ParamPrefix: prefix}})
}

func (s *Store) QueryApprovalRequests(ctx context.Context, filter *storage.ApprovalRequestFilter) ([]*storage.ApprovalRequestRow, error) {
	return many[storage.ApprovalRequestRow](ctx, s, KindApprovalRequests, filter)
}

func (s *Store) GetApprovalRequestByPrefix(ctx context.Context, prefix string) (*storage.ApprovalRequestRow, error) {
	return one[storage.ApprovalRequestRow](ctx, s, ipc.Query{Kind: KindApprovalRequestByPrefix, Params: map[string]string{ParamPrefix: prefix, ParamAll: "1"}})
}

func (s *Store) GetContextStateByPrefix(ctx context.Context, prefix string) (*storage.ContextStateRow, error) {
	return one[storage.ContextStateRow](ctx, s, ipc.Query{Kind: KindContextStateByPrefix, Params: map[string]string{ParamPrefix: prefix}})
}

// countRow is the one row of a count.
type countRow struct {
	Count int `json:"count"`
}

func encode(filter any) string {
	data, err := json.Marshal(filter)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// one runs a query that answers with one row or none.
func one[T any](ctx context.Context, s *Store, q ipc.Query) (*T, error) {
	res, err := s.client.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("remote: %s: %w", q.Kind, err)
	}
	if len(res.Rows) == 0 {
		return nil, nil
	}
	var row T
	if err := json.Unmarshal(res.Rows[0], &row); err != nil {
		return nil, fmt.Errorf("remote: %s: decode row: %w", q.Kind, err)
	}
	return &row, nil
}

// many runs a filtered query over every page.
func many[T any](ctx context.Context, s *Store, kind string, filter any) ([]*T, error) {
	return pages[T](ctx, s, ipc.Query{Kind: kind, Params: map[string]string{ParamFilter: encode(filter)}})
}

// pages runs q once per page until the server reports no next page.
func pages[T any](ctx context.Context, s *Store, q ipc.Query) ([]*T, error) {
	var out []*T
	page := 0
	for {
		if q.Params == nil {
			q.Params = map[string]string{}
		}
		q.Params[ParamPage] = strconv.Itoa(page)
		res, err := s.client.Query(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("remote: %s: %w", q.Kind, err)
		}
		for _, raw := range res.Rows {
			var row T
			if err := json.Unmarshal(raw, &row); err != nil {
				return nil, fmt.Errorf("remote: %s: decode row: %w", q.Kind, err)
			}
			out = append(out, &row)
		}
		if res.Next == "" {
			return out, nil
		}
		next, err := strconv.Atoi(res.Next)
		if err != nil || next <= page {
			return nil, fmt.Errorf("remote: %s: bad cursor %q", q.Kind, res.Next)
		}
		page = next
	}
}
