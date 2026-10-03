package supervisor

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
	"github.com/safedep/gryph/storage/remote"
)

// query answers one read on the store of the partition. The kind names a
// method of storage.ReadStore, and every parameter is the argument of that
// method. The store runs the same filter as a local read would, so a
// managed host reads what a user install reads, and the answer goes out
// in pages of remote.PageSize rows.
func (p *partition) query(ctx context.Context, q *ipc.Query) (*ipc.Frame, error) {
	if !p.bucket.take(time.Now()) {
		p.recordRateLimit(ctx, "query")
		return ipc.ErrorFrame(ipc.CodeRateLimited, "too many requests from this account"), nil
	}
	rows, err := p.read(ctx, q)
	if err != nil {
		return ipc.ErrorFrame(ipc.CodeInvalid, err.Error()), nil
	}
	page, err := pageOf(q)
	if err != nil {
		return ipc.ErrorFrame(ipc.CodeInvalid, err.Error()), nil
	}
	res, err := paginate(rows, page)
	if err != nil {
		return ipc.ErrorFrame(ipc.CodeInternal, err.Error()), nil
	}
	return ipc.NewFrame(ipc.TypeQueryResult, res)
}

// read runs the method the query names and returns its rows as values to
// encode. An unknown kind is an error, and so is a parameter that does
// not parse.
func (p *partition) read(ctx context.Context, q *ipc.Query) ([]any, error) {
	store := p.rt.Store
	switch q.Kind {
	case remote.KindEvent:
		id, err := uuidParam(q, remote.ParamID)
		if err != nil {
			return nil, err
		}
		return rowOf(store.GetEvent(ctx, id))
	case remote.KindEventByPrefix:
		return rowOf(store.GetEventByPrefix(ctx, q.Params[remote.ParamPrefix]))
	case remote.KindEvents:
		filter, err := filterParam[events.EventFilter](q)
		if err != nil {
			return nil, err
		}
		return rowsOf(store.QueryEvents(ctx, filter))
	case remote.KindCountEvents:
		filter, err := filterParam[events.EventFilter](q)
		if err != nil {
			return nil, err
		}
		n, err := store.CountEvents(ctx, filter)
		if err != nil {
			return nil, err
		}
		return []any{map[string]int{"count": n}}, nil
	case remote.KindSessionEvents:
		id, err := uuidParam(q, remote.ParamID)
		if err != nil {
			return nil, err
		}
		return rowsOf(store.GetEventsBySession(ctx, id))
	case remote.KindEventsAfter:
		after, err := time.Parse(time.RFC3339Nano, q.Params[remote.ParamAfter])
		if err != nil {
			return nil, fmt.Errorf("after: %w", err)
		}
		var afterID uuid.UUID
		if raw := q.Params[remote.ParamID]; raw != "" {
			if afterID, err = uuid.Parse(raw); err != nil {
				return nil, fmt.Errorf("id: %w", err)
			}
		}
		limit, err := strconv.Atoi(q.Params[remote.ParamLimit])
		if err != nil {
			return nil, fmt.Errorf("limit: %w", err)
		}
		return rowsOf(store.QueryEventsAfter(ctx, after, afterID, limit))
	case remote.KindSession:
		id, err := uuidParam(q, remote.ParamID)
		if err != nil {
			return nil, err
		}
		return rowOf(store.GetSession(ctx, id))
	case remote.KindSessionByPrefix:
		return rowOf(store.GetSessionByPrefix(ctx, q.Params[remote.ParamPrefix]))
	case remote.KindSessions:
		filter, err := filterParam[session.SessionFilter](q)
		if err != nil {
			return nil, err
		}
		return rowsOf(store.QuerySessions(ctx, filter))
	case remote.KindReceipts:
		filter, err := filterParam[storage.ReceiptFilter](q)
		if err != nil {
			return nil, err
		}
		return rowsOf(store.QueryReceipts(ctx, filter))
	case remote.KindReceiptSessionIDs:
		ids, err := store.ListReceiptSessionIDs(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(ids))
		for _, id := range ids {
			out = append(out, id)
		}
		return out, nil
	case remote.KindDeferredActions:
		filter, err := filterParam[storage.DeferredActionFilter](q)
		if err != nil {
			return nil, err
		}
		return rowsOf(store.QueryDeferredActions(ctx, filter))
	case remote.KindDeferredActionByPrefix:
		return rowOf(store.GetDeferredActionByPrefix(ctx, q.Params[remote.ParamPrefix]))
	case remote.KindApprovalRequests:
		filter, err := filterParam[storage.ApprovalRequestFilter](q)
		if err != nil {
			return nil, err
		}
		p.expireNow(ctx)
		return rowsOf(store.QueryApprovalRequests(ctx, filter))
	case remote.KindApprovalRequestByPrefix:
		p.expireNow(ctx)
		return rowOf(store.GetApprovalRequestByPrefix(ctx, q.Params[remote.ParamPrefix]))
	case remote.KindContextStateByPrefix:
		return rowOf(store.GetContextStateByPrefix(ctx, q.Params[remote.ParamPrefix]))
	default:
		return nil, fmt.Errorf("unknown query kind %q", q.Kind)
	}
}

// setSessionCost stores the cost totals the client collected on the
// session, as a claim: the service did not read the transcript.
func (p *partition) setSessionCost(ctx context.Context, sc *ipc.SessionCost) error {
	p.write.Lock()
	defer p.write.Unlock()
	sess, err := p.rt.Store.GetSession(ctx, sc.SessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("session %s is not in this partition", sc.SessionID)
	}
	sess.SetClientReportedCost(sc.Cost)
	return p.rt.Store.UpdateSession(ctx, sess)
}

func uuidParam(q *ipc.Query, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(q.Params[name])
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", name, err)
	}
	return id, nil
}

func filterParam[T any](q *ipc.Query) (*T, error) {
	var filter T
	raw := q.Params[remote.ParamFilter]
	if raw == "" {
		return &filter, nil
	}
	if err := json.Unmarshal([]byte(raw), &filter); err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	return &filter, nil
}

func pageOf(q *ipc.Query) (int, error) {
	raw := q.Params[remote.ParamPage]
	if raw == "" {
		return 0, nil
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page < 0 {
		return 0, fmt.Errorf("page %q", raw)
	}
	return page, nil
}

// rowOf wraps the answer of a lookup: one row, or none when the store
// found nothing.
func rowOf[T any](row *T, err error) ([]any, error) {
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return []any{row}, nil
}

func rowsOf[T any](rows []*T, err error) ([]any, error) {
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	return out, nil
}

// paginate encodes the rows of one page and names the next one.
func paginate(rows []any, page int) (ipc.QueryResult, error) {
	start := page * remote.PageSize
	if start > len(rows) {
		start = len(rows)
	}
	end := start + remote.PageSize
	if end > len(rows) {
		end = len(rows)
	}
	res := ipc.QueryResult{Rows: make([]json.RawMessage, 0, end-start)}
	for _, row := range rows[start:end] {
		data, err := json.Marshal(row)
		if err != nil {
			return ipc.QueryResult{}, err
		}
		res.Rows = append(res.Rows, data)
	}
	if end < len(rows) {
		res.Next = strconv.Itoa(page + 1)
	}
	return res, nil
}

// importRows takes the rows of one import frame into the partition,
// marked imported. The rows come from the user's own database, which the
// user could change, so they never touch the accumulator and a session
// the service recorded itself refuses them.
func (p *partition) importRows(ctx context.Context, body ipc.Body) (int, error) {
	p.write.Lock()
	defer p.write.Unlock()
	switch b := body.(type) {
	case *ipc.ImportEvents:
		return p.rt.Store.ImportEvents(ctx, b.SessionID, b.Events)
	case *ipc.ImportReceipts:
		return p.rt.Store.ImportReceipts(ctx, b.SessionID, b.Receipts)
	case *ipc.ImportSession:
		taken, err := p.rt.Store.ImportSession(ctx, b.Session)
		if err != nil {
			return 0, err
		}
		if taken {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("not an import frame: %T", body)
	}
}
