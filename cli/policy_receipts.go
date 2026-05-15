package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

const policyReceiptsDefaultLimit = 100

func newPolicyReceiptsCmd() *cobra.Command {
	var (
		sessionID   string
		decision    string
		since       time.Duration
		until       time.Duration
		limit       int
		format      string
		verify      bool
		allSessions bool
	)

	cmd := &cobra.Command{
		Use:   "receipts",
		Short: "List AARM mediated-action receipts",
		Long: "List receipts produced by the AARM Mediator. By default lists the " +
			"most recent receipts across all sessions, descending. With --session, " +
			"orders by sequence ascending and shows the hash-chain for that session. " +
			"Pass --verify to re-derive the chain hash and report any breaks.\n\n" +
			"Verification scope:\n" +
			"  --verify --session ID     verifies the full chain for one session " +
			"(re-fetches every receipt regardless of --limit).\n" +
			"  --verify                  verifies sessions whose receipts appear in " +
			"the most recent --limit rows. Older sessions are not visited.\n" +
			"  --verify --all-sessions   enumerates every distinct session in the " +
			"receipt log and verifies each chain in full. Use for full-cluster audits.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if allSessions && sessionID != "" {
				return ErrConfig("invalid flags", fmt.Errorf("--all-sessions cannot be combined with --session"))
			}
			if allSessions && !verify {
				return ErrConfig("invalid flags", fmt.Errorf("--all-sessions requires --verify"))
			}

			app, err := loadApp()
			if err != nil {
				return err
			}

			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				if cerr := app.Close(); cerr != nil {
					log.Errorf("failed to close app: %v", cerr)
				}
			}()

			c := policyColorizer(app)
			out := cmd.OutOrStdout()

			filter := &storage.ReceiptFilter{
				Decision: decision,
				Limit:    limit,
			}
			if since > 0 {
				t := time.Now().Add(-since)
				filter.Since = &t
			}
			if until > 0 {
				t := time.Now().Add(-until)
				filter.Until = &t
			}
			if sessionID != "" {
				sid, err := resolveAarmSessionID(ctx, app.Store, sessionID)
				if err != nil {
					return err
				}
				filter.SessionID = &sid
			}

			rows, err := app.Store.QueryReceipts(ctx, filter)
			if err != nil {
				return fmt.Errorf("failed to query receipts: %w", err)
			}

			if verify {
				breaks, verr := verifyReceiptChains(ctx, app.Store, rows, filter.SessionID, allSessions)
				if verr != nil {
					return verr
				}
				if format == "json" {
					return writeReceiptsJSON(out, rows, breaks)
				}
				renderReceiptsTable(out, c, rows)
				renderVerifyResults(out, c, breaks)
				if len(breaks) > 0 {
					return ErrConfig("receipt chain verification failed", fmt.Errorf("%d break(s) detected", len(breaks)))
				}
				return nil
			}

			if format == "json" {
				return writeReceiptsJSON(out, rows, nil)
			}
			renderReceiptsTable(out, c, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (UUID or prefix) to inspect")
	cmd.Flags().StringVar(&decision, "decision", "", "filter to a single decision (allow, block, guidance, warn, escalate)")
	cmd.Flags().DurationVar(&since, "since", 0, "include receipts newer than this offset from now (e.g. 24h)")
	cmd.Flags().DurationVar(&until, "until", 0, "include receipts older than this offset from now")
	cmd.Flags().IntVar(&limit, "limit", policyReceiptsDefaultLimit, "maximum number of receipts to return")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	cmd.Flags().BoolVar(&verify, "verify", false, "re-derive the hash chain and report any breaks")
	cmd.Flags().BoolVar(&allSessions, "all-sessions", false, "with --verify, enumerate every session in the receipt log and verify each chain in full. Mutually exclusive with --session")
	return cmd
}

type receiptVerifyBreak struct {
	SessionID uuid.UUID `json:"session_id"`
	Sequence  int64     `json:"sequence"`
	Reason    string    `json:"reason"`
}

func verifyReceiptChains(ctx context.Context, store storage.Store, rows []*storage.ReceiptRow, sessionFilter *uuid.UUID, allSessions bool) ([]receiptVerifyBreak, error) {
	sessionIDs, err := collectVerifySessionIDs(ctx, store, rows, sessionFilter, allSessions)
	if err != nil {
		return nil, err
	}

	var breaks []receiptVerifyBreak
	for _, sid := range sessionIDs {
		full, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{
			SessionID: &sid,
			Limit:     -1,
		})
		if err != nil {
			return nil, fmt.Errorf("verify: load session receipts: %w", err)
		}
		sort.Slice(full, func(i, j int) bool { return full[i].Sequence < full[j].Sequence })
		breaks = append(breaks, verifyOneChain(full)...)
	}

	if len(breaks) > 0 {
		emitChainBrokenAudit(ctx, store, breaks)
	}
	return breaks, nil
}

func collectVerifySessionIDs(ctx context.Context, store storage.Store, rows []*storage.ReceiptRow, sessionFilter *uuid.UUID, allSessions bool) ([]uuid.UUID, error) {
	if sessionFilter != nil {
		return []uuid.UUID{*sessionFilter}, nil
	}
	if allSessions {
		ids, err := store.ListReceiptSessionIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("verify: enumerate session IDs: %w", err)
		}
		return ids, nil
	}
	seen := map[uuid.UUID]struct{}{}
	ids := make([]uuid.UUID, 0)
	for _, r := range rows {
		if _, ok := seen[r.SessionID]; ok {
			continue
		}
		seen[r.SessionID] = struct{}{}
		ids = append(ids, r.SessionID)
	}
	return ids, nil
}

func verifyOneChain(rows []*storage.ReceiptRow) []receiptVerifyBreak {
	var breaks []receiptVerifyBreak
	var prevHash []byte
	var prevSeq int64
	for _, r := range rows {
		expectedSeq := prevSeq + 1
		if prevSeq == 0 && r.Sequence != 1 {
			breaks = append(breaks, receiptVerifyBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("first receipt sequence is %d, expected 1", r.Sequence),
			})
		} else if prevSeq > 0 && r.Sequence != expectedSeq {
			breaks = append(breaks, receiptVerifyBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("sequence gap: got %d, expected %d", r.Sequence, expectedSeq),
			})
		}
		if len(prevHash) > 0 && !bytes.Equal(r.PrevHash, prevHash) {
			breaks = append(breaks, receiptVerifyBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    "prev_hash does not match previous row hash",
			})
		}
		expectedHash, err := recomputeReceiptHash(r)
		if err != nil {
			breaks = append(breaks, receiptVerifyBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    fmt.Sprintf("recompute hash: %v", err),
			})
		} else if !bytes.Equal(expectedHash, r.Hash) {
			breaks = append(breaks, receiptVerifyBreak{
				SessionID: r.SessionID,
				Sequence:  r.Sequence,
				Reason:    "stored hash does not match recomputed hash",
			})
		}
		prevHash = r.Hash
		prevSeq = r.Sequence
	}
	return breaks
}

func recomputeReceiptHash(r *storage.ReceiptRow) ([]byte, error) {
	in := receipt.NewHashInput(receipt.HashInputFields{
		Sequence:       r.Sequence,
		PrevHash:       r.PrevHash,
		RecordedAtUnix: r.RecordedAt.UnixNano(),
		SessionID:      r.SessionID,
		ActionID:       r.ActionID,
		EventID:        r.EventID,
		Agent:          r.Agent,
		Tool:           r.Tool,
		ActionType:     r.ActionType,
		Project:        r.Project,
		Decision:       r.Decision,
		Severity:       r.Severity,
		Message:        r.Message,
		MatchedRuleIDs: r.MatchedRuleIDs,
		Snapshot:       r.Snapshot,
		ActionPayload:  r.ActionPayload,
	})
	return receipt.ComputeHash(in)
}

func emitChainBrokenAudit(ctx context.Context, store storage.Store, breaks []receiptVerifyBreak) {
	if store == nil || len(breaks) == 0 {
		return
	}
	details := map[string]interface{}{
		"break_count": len(breaks),
	}
	summary := make([]map[string]interface{}, 0, len(breaks))
	for _, b := range breaks {
		summary = append(summary, map[string]interface{}{
			"session_id": b.SessionID.String(),
			"sequence":   b.Sequence,
			"reason":     b.Reason,
		})
	}
	details["breaks"] = summary
	if err := logSelfAudit(ctx, store, SelfAuditActionReceiptChainBroken, "",
		details, SelfAuditResultError, "receipt chain verification failed"); err != nil {
		log.Errorf("failed to record receipt chain failure: %v", err)
	}
}

type policyReceiptView struct {
	ID             string                 `json:"id"`
	SessionID      string                 `json:"session_id"`
	Sequence       int64                  `json:"sequence"`
	RecordedAt     string                 `json:"recorded_at"`
	Agent          string                 `json:"agent,omitempty"`
	Tool           string                 `json:"tool,omitempty"`
	ActionType     string                 `json:"action_type"`
	Project        string                 `json:"project,omitempty"`
	Decision       string                 `json:"decision"`
	MatchedRuleIDs []string               `json:"matched_rule_ids,omitempty"`
	Severity       string                 `json:"severity,omitempty"`
	Message        string                 `json:"message,omitempty"`
	ResultStatus   string                 `json:"result_status"`
	DurationMS     *int64                 `json:"duration_ms,omitempty"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
	Snapshot       map[string]interface{} `json:"snapshot,omitempty"`
	ActionPayload  map[string]interface{} `json:"action_payload,omitempty"`
	PrevHash       string                 `json:"prev_hash,omitempty"`
	Hash           string                 `json:"hash"`
}

func receiptToView(r *storage.ReceiptRow) policyReceiptView {
	v := policyReceiptView{
		ID:             r.ID.String(),
		SessionID:      r.SessionID.String(),
		Sequence:       r.Sequence,
		RecordedAt:     r.RecordedAt.Format(time.RFC3339Nano),
		Agent:          r.Agent,
		Tool:           r.Tool,
		ActionType:     r.ActionType,
		Project:        r.Project,
		Decision:       r.Decision,
		MatchedRuleIDs: r.MatchedRuleIDs,
		Severity:       r.Severity,
		Message:        r.Message,
		ResultStatus:   r.ResultStatus,
		ErrorMessage:   r.ErrorMessage,
		Snapshot:       r.Snapshot,
		ActionPayload:  r.ActionPayload,
		Hash:           hex.EncodeToString(r.Hash),
	}
	if r.DurationMS != nil {
		d := *r.DurationMS
		v.DurationMS = &d
	}
	if len(r.PrevHash) > 0 {
		v.PrevHash = hex.EncodeToString(r.PrevHash)
	}
	return v
}

func writeReceiptsJSON(w io.Writer, rows []*storage.ReceiptRow, breaks []receiptVerifyBreak) error {
	views := make([]policyReceiptView, 0, len(rows))
	for _, r := range rows {
		views = append(views, receiptToView(r))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	out := map[string]interface{}{
		"receipts": views,
	}
	if breaks != nil {
		out["chain_breaks"] = breaks
	}
	return enc.Encode(out)
}

func renderReceiptsTable(w io.Writer, c *tui.Colorizer, rows []*storage.ReceiptRow) {
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(w, c.Dim("No receipts recorded yet."))
		return
	}
	_, _ = fmt.Fprintln(w, c.Header("Receipts"))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(100))
	_, _ = fmt.Fprintf(w, "  %-20s  %-5s  %-13s  %-10s  %-12s  %-10s  %-9s  %s\n",
		c.Dim("recorded_at"), c.Dim("seq"), c.Dim("session"),
		c.Dim("agent"), c.Dim("tool"), c.Dim("decision"),
		c.Dim("result"), c.Dim("hash"))
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "  %-20s  %-5d  %-13s  %-10s  %-12s  %-10s  %-9s  %s\n",
			r.RecordedAt.Format("2006-01-02 15:04:05"),
			r.Sequence,
			tui.FormatShortID(r.SessionID.String()),
			tui.TruncateString(r.Agent, 10),
			tui.TruncateString(r.Tool, 12),
			r.Decision,
			r.ResultStatus,
			shortHash(r.Hash),
		)
	}
}

func renderVerifyResults(w io.Writer, c *tui.Colorizer, breaks []receiptVerifyBreak) {
	_, _ = fmt.Fprintln(w)
	if len(breaks) == 0 {
		_, _ = fmt.Fprintln(w, c.Success("Chain verification: OK"))
		return
	}
	_, _ = fmt.Fprintln(w, c.Error("Chain verification: FAILED"))
	for _, b := range breaks {
		_, _ = fmt.Fprintf(w, "  session=%s seq=%d %s\n",
			tui.FormatShortID(b.SessionID.String()), b.Sequence, b.Reason)
	}
}

func shortHash(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	s := hex.EncodeToString(b)
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
