package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
)

// receiptFollowOverlap is how far back each poll reads. A hook computes
// recorded_at before it commits the receipt, and hooks of other sessions
// commit in parallel. So a receipt can commit with a time before the latest
// time that follow has printed. A poll from the latest time only would skip
// it. The seen set drops the rows that the overlap reads again.
const receiptFollowOverlap = 10 * time.Second

const receiptFollowTargetMax = 60

type receiptQuerier interface {
	QueryReceipts(ctx context.Context, filter *storage.ReceiptFilter) ([]*storage.ReceiptRow, error)
}

type receiptFollower struct {
	store  receiptQuerier
	filter storage.ReceiptFilter
	w      io.Writer
	c      *tui.Colorizer
	seen   map[uuid.UUID]time.Time
	latest time.Time
	// floor is the first row that follow printed. The initial limit can cut
	// older rows that the overlap reads again, and follow must not print
	// them after newer rows.
	floor *storage.ReceiptRow
}

func newReceiptFollower(store receiptQuerier, filter storage.ReceiptFilter, w io.Writer, c *tui.Colorizer) *receiptFollower {
	return &receiptFollower{store: store, filter: filter, w: w, c: c, seen: map[uuid.UUID]time.Time{}}
}

// follow prints the latest limit receipts, then the new receipts on each
// tick, until ctx ends.
func (f *receiptFollower) follow(ctx context.Context, limit int, tick <-chan time.Time) error {
	if err := f.printInitial(ctx, limit); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			if err := f.poll(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				log.Warnf("policy receipts: poll: %v", err)
			}
		}
	}
}

func (f *receiptFollower) printInitial(ctx context.Context, limit int) error {
	filter := f.filter
	// With a session, the store orders by sequence and keeps the oldest
	// rows. Follow shows the newest, so it reads the session in full.
	filter.Limit = limit
	if filter.SessionID != nil {
		filter.Limit = -1
	}
	rows, err := f.store.QueryReceipts(ctx, &filter)
	if err != nil {
		return fmt.Errorf("failed to query receipts: %w", err)
	}
	sortReceiptsByTime(rows)
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	f.latest = time.Now()
	if len(rows) > 0 {
		f.floor = rows[0]
	}
	return f.print(rows)
}

func (f *receiptFollower) poll(ctx context.Context) error {
	filter := f.filter
	since := f.latest.Add(-receiptFollowOverlap)
	filter.Since = &since
	filter.Limit = -1
	rows, err := f.store.QueryReceipts(ctx, &filter)
	if err != nil {
		return err
	}
	sortReceiptsByTime(rows)
	if err := f.print(rows); err != nil {
		return err
	}
	for id, at := range f.seen {
		if at.Before(since) {
			delete(f.seen, id)
		}
	}
	return nil
}

func (f *receiptFollower) print(rows []*storage.ReceiptRow) error {
	for _, r := range rows {
		if _, ok := f.seen[r.ID]; ok {
			continue
		}
		if f.floor != nil && compareReceipts(r, f.floor) < 0 {
			continue
		}
		f.seen[r.ID] = r.RecordedAt
		if r.RecordedAt.After(f.latest) {
			f.latest = r.RecordedAt
		}
		if _, err := fmt.Fprintln(f.w, formatReceiptLine(f.c, r)); err != nil {
			return err
		}
	}
	return nil
}

func sortReceiptsByTime(rows []*storage.ReceiptRow) {
	slices.SortFunc(rows, compareReceipts)
}

func compareReceipts(a, b *storage.ReceiptRow) int {
	if c := a.RecordedAt.Compare(b.RecordedAt); c != 0 {
		return c
	}
	return strings.Compare(a.ID.String(), b.ID.String())
}

// formatReceiptLine renders one receipt as a line. Every stored string can
// come from an agent or a policy file, so each one is escaped.
func formatReceiptLine(c *tui.Colorizer, r *storage.ReceiptRow) string {
	decision := fmt.Sprintf("%-10s", tui.TruncateString(tui.EscapeLine(r.Decision), 10))
	return fmt.Sprintf("%s  %-8s  %-12s  %-12s  %s  %-28s  %s",
		c.Dim(r.RecordedAt.Local().Format("15:04:05")),
		tui.FormatShortID(r.SessionID.String()),
		tui.TruncateString(tui.EscapeLine(r.Agent), 12),
		tui.TruncateString(tui.EscapeLine(r.Tool), 12),
		colorDecision(c, r.Decision, decision),
		receiptRuleCell(r.MatchedRuleIDs),
		receiptTarget(r.ActionPayload),
	)
}

func colorDecision(c *tui.Colorizer, decision, text string) string {
	switch decision {
	case "block":
		return c.Error(text)
	case "warn", "escalate", "defer":
		return c.Warning(text)
	case "guidance":
		return c.Cyan(text)
	case "allow":
		return c.Success(text)
	}
	return text
}

// receiptTarget names what the action touched: the command, the path or the
// URL, in that order.
func receiptTarget(payload map[string]interface{}) string {
	for _, key := range []string{"command", "path", "url"} {
		if v, ok := payload[key].(string); ok && v != "" {
			return tui.TruncateString(tui.EscapeLine(v), receiptFollowTargetMax)
		}
	}
	return ""
}
