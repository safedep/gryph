package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
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

const receiptFollowMaxFailures = 5

type receiptFollower struct {
	store  receiptQuerier
	filter storage.ReceiptFilter
	w      io.Writer
	errW   io.Writer
	c      *tui.Colorizer
	seen   map[uuid.UUID]time.Time
	latest time.Time
}

func newReceiptFollower(store receiptQuerier, filter storage.ReceiptFilter, w, errW io.Writer, c *tui.Colorizer) *receiptFollower {
	return &receiptFollower{store: store, filter: filter, w: w, errW: errW, c: c, seen: map[uuid.UUID]time.Time{}}
}

// follow prints the latest limit receipts, then the new receipts on each
// tick, until ctx ends. It stops with an error after
// receiptFollowMaxFailures failed polls in a row.
func (f *receiptFollower) follow(ctx context.Context, limit int, tick <-chan time.Time) error {
	if err := f.printInitial(ctx, limit); err != nil {
		return err
	}
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			since := f.pollSince()
			rows, err := f.fetch(ctx, since)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				failures++
				if failures >= receiptFollowMaxFailures {
					return err
				}
				if _, werr := fmt.Fprintln(f.errW, f.c.Warning(fmt.Sprintf("%v, retrying", err))); werr != nil {
					return werr
				}
				continue
			}
			failures = 0
			if err := f.show(rows); err != nil {
				return err
			}
			f.prune(since)
		}
	}
}

// printInitial reads the latest limit rows and every row of the overlap
// window. It prints the newest limit rows of both and marks the rest as
// seen. The first poll reads the window again and must not print a row that
// the limit cut after newer rows.
func (f *receiptFollower) printInitial(ctx context.Context, limit int) error {
	start := time.Now()
	filter := f.filter
	// With a session, the store orders by sequence and keeps the oldest
	// rows. Follow shows the newest, so it reads the session in full.
	filter.Limit = limit
	if filter.SessionID != nil {
		filter.Limit = -1
	}
	older, err := f.store.QueryReceipts(ctx, &filter)
	if err != nil {
		return fmt.Errorf("failed to query receipts: %w", err)
	}
	recent, err := f.fetch(ctx, f.windowSince(start))
	if err != nil {
		return err
	}
	rows := mergeReceipts(older, recent)
	cut := max(0, len(rows)-limit)
	for _, r := range rows[:cut] {
		f.seen[r.ID] = r.RecordedAt
	}
	f.latest = start
	return f.show(rows[cut:])
}

func (f *receiptFollower) pollSince() time.Time {
	return f.windowSince(f.latest)
}

// windowSince is the start of the overlap window before t. It never reads
// before the user's --since.
func (f *receiptFollower) windowSince(t time.Time) time.Time {
	since := t.Add(-receiptFollowOverlap)
	if f.filter.Since != nil && f.filter.Since.After(since) {
		return *f.filter.Since
	}
	return since
}

func (f *receiptFollower) fetch(ctx context.Context, since time.Time) ([]*storage.ReceiptRow, error) {
	filter := f.filter
	filter.Since = &since
	filter.Limit = -1
	rows, err := f.store.QueryReceipts(ctx, &filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query receipts: %w", err)
	}
	sortReceiptsByTime(rows)
	return rows, nil
}

func (f *receiptFollower) show(rows []*storage.ReceiptRow) error {
	for _, r := range rows {
		if _, ok := f.seen[r.ID]; ok {
			continue
		}
		if _, err := fmt.Fprintln(f.w, formatReceiptLine(f.c, r)); err != nil {
			return err
		}
		f.seen[r.ID] = r.RecordedAt
		if r.RecordedAt.After(f.latest) {
			f.latest = r.RecordedAt
		}
	}
	return nil
}

func (f *receiptFollower) prune(since time.Time) {
	for id, at := range f.seen {
		if at.Before(since) {
			delete(f.seen, id)
		}
	}
}

func mergeReceipts(a, b []*storage.ReceiptRow) []*storage.ReceiptRow {
	byID := make(map[uuid.UUID]*storage.ReceiptRow, len(a)+len(b))
	for _, r := range slices.Concat(a, b) {
		byID[r.ID] = r
	}
	rows := slices.Collect(maps.Values(byID))
	sortReceiptsByTime(rows)
	return rows
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
