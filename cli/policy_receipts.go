package cli

import (
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
		showHash    bool
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
				verifier, verifyErr := loadReceiptVerifierFromConfig(app.Config, app.Paths)
				if verifyErr != nil {
					return ErrConfig("load trust store", verifyErr)
				}
				breaks, sigResults, verr := verifyReceiptChains(ctx, app.Store, rows, filter.SessionID, allSessions, verifier)
				if verr != nil {
					return verr
				}
				sigSummary := summarizeSignatureResults(sigResults)
				if format == "json" {
					return writeReceiptsJSONWithSignatures(out, rows, breaks, sigResults, sigSummary)
				}
				renderReceiptsTable(out, c, rows, true)
				renderVerifyResults(out, c, breaks)
				renderSignatureVerifyResults(out, c, sigSummary, sigResults)
				if len(breaks) > 0 || sigSummary.SignedInvalid > 0 {
					return ErrConfig("receipt verification failed", fmt.Errorf("%d chain break(s), %d invalid signature(s)", len(breaks), sigSummary.SignedInvalid))
				}
				return nil
			}

			if format == "json" {
				return writeReceiptsJSON(out, rows, nil)
			}
			renderReceiptsTable(out, c, rows, showHash)
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
	cmd.Flags().BoolVar(&showHash, "show-hash", false, "include the per-row hash in the table output")
	return cmd
}

type receiptVerifyBreak struct {
	SessionID uuid.UUID `json:"session_id"`
	Sequence  int64     `json:"sequence"`
	Reason    string    `json:"reason"`
}

// verifyReceiptChains visits every session implied by rows / sessionFilter /
// allSessions, loads its full chain, re-derives every receipt hash, and (when
// verifier is non-nil) verifies each signature inline. Chain breaks and per-
// receipt signature verdicts are returned together so callers can render
// both without a second full pass over the data.
func verifyReceiptChains(ctx context.Context, store storage.Store, rows []*storage.ReceiptRow, sessionFilter *uuid.UUID, allSessions bool, verifier *receipt.Ed25519Verifier) ([]receiptVerifyBreak, []receiptSignatureResult, error) {
	sessionIDs, err := collectVerifySessionIDs(ctx, store, rows, sessionFilter, allSessions)
	if err != nil {
		return nil, nil, err
	}

	var (
		breaks     []receiptVerifyBreak
		sigResults []receiptSignatureResult
	)
	for _, sid := range sessionIDs {
		full, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{
			SessionID: &sid,
			Limit:     -1,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("verify: load session receipts: %w", err)
		}
		sort.Slice(full, func(i, j int) bool { return full[i].Sequence < full[j].Sequence })
		chainRows := make([]receipt.ChainRow, 0, len(full))
		for _, r := range full {
			chainRows = append(chainRows, receipt.ChainRowFromReceipt(r))
		}
		for _, b := range receipt.VerifyChain(chainRows) {
			breaks = append(breaks, receiptVerifyBreak{
				SessionID: b.SessionID,
				Sequence:  b.Sequence,
				Reason:    b.Reason,
			})
		}
		for _, r := range full {
			sigResults = append(sigResults, verifyOneSignature(ctx, store, r, verifier))
		}
	}

	if len(breaks) > 0 {
		emitChainBrokenAudit(ctx, store, breaks)
	}
	return breaks, sigResults, nil
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
	SubagentID     string                 `json:"subagent_id,omitempty"`
	SubagentType   string                 `json:"subagent_type,omitempty"`
	PolicyHash     string                 `json:"policy_hash,omitempty"`
	SignerKeyID    string                 `json:"signer_key_id,omitempty"`
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
		SubagentID:     r.SubagentID,
		SubagentType:   r.SubagentType,
		SignerKeyID:    r.SignerKeyID,
	}
	if r.DurationMS != nil {
		d := *r.DurationMS
		v.DurationMS = &d
	}
	if len(r.PrevHash) > 0 {
		v.PrevHash = hex.EncodeToString(r.PrevHash)
	}
	if len(r.PolicyHash) > 0 {
		v.PolicyHash = hex.EncodeToString(r.PolicyHash)
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

// receiptTableTrailing describes the variable trailing portion of the receipt
// table (after the shared decision column). Header is rendered into the
// header row and cell(r) returns the per-row text. The two are decoupled so
// approve history can render its own "note" tail without duplicating the
// surrounding scaffolding.
type receiptTableTrailing struct {
	Title   string
	Headers []string
	Format  string
	Cells   func(r *storage.ReceiptRow) []interface{}
}

func defaultReceiptTrailing(showHash bool) receiptTableTrailing {
	if showHash {
		return receiptTableTrailing{
			Title:   "Receipts",
			Headers: []string{"result", "hash"},
			Format:  "  %-9s  %s\n",
			Cells: func(r *storage.ReceiptRow) []interface{} {
				return []interface{}{r.ResultStatus, shortHash(r.Hash)}
			},
		}
	}
	return receiptTableTrailing{
		Title:   "Receipts",
		Headers: []string{"result"},
		Format:  "  %s\n",
		Cells: func(r *storage.ReceiptRow) []interface{} {
			return []interface{}{r.ResultStatus}
		},
	}
}

func renderReceiptsTable(w io.Writer, c *tui.Colorizer, rows []*storage.ReceiptRow, showHash bool) {
	renderReceiptsTableWith(w, c, rows, defaultReceiptTrailing(showHash), "No receipts recorded yet.")
}

func renderReceiptsTableWith(w io.Writer, c *tui.Colorizer, rows []*storage.ReceiptRow, trailing receiptTableTrailing, emptyMsg string) {
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(w, c.Dim(emptyMsg))
		return
	}
	_, _ = fmt.Fprintln(w, c.Header(trailing.Title))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(100))

	headerPrefix := fmt.Sprintf("  %-20s  %-5s  %-13s  %-10s  %-12s  %-10s",
		c.Dim("recorded_at"), c.Dim("seq"), c.Dim("session"),
		c.Dim("agent"), c.Dim("tool"), c.Dim("decision"))
	headerTail := make([]interface{}, len(trailing.Headers))
	for i, h := range trailing.Headers {
		headerTail[i] = c.Dim(h)
	}
	_, _ = fmt.Fprint(w, headerPrefix)
	_, _ = fmt.Fprintf(w, trailing.Format, headerTail...)

	rowPrefixFmt := "  %-20s  %-5d  %-13s  %-10s  %-12s  %-10s"
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, rowPrefixFmt,
			r.RecordedAt.Format("2006-01-02 15:04:05"),
			r.Sequence,
			tui.FormatShortID(r.SessionID.String()),
			tui.TruncateString(r.Agent, 10),
			tui.TruncateString(r.Tool, 12),
			r.Decision,
		)
		_, _ = fmt.Fprintf(w, trailing.Format, trailing.Cells(r)...)
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

// signatureStatus is the per-receipt outcome of signature verification.
type signatureStatus string

const (
	signatureStatusUnsigned signatureStatus = "unsigned"
	signatureStatusOK       signatureStatus = "signed_ok"
	signatureStatusInvalid  signatureStatus = "signed_invalid"
)

// receiptSignatureResult records the signature verification verdict for one
// receipt. ReceiptID + (SessionID, Sequence) identify the row.
type receiptSignatureResult struct {
	SessionID uuid.UUID       `json:"session_id"`
	Sequence  int64           `json:"sequence"`
	ReceiptID uuid.UUID       `json:"receipt_id"`
	KeyID     string          `json:"key_id,omitempty"`
	Status    signatureStatus `json:"status"`
	Reason    string          `json:"reason,omitempty"`
}

type signatureSummary struct {
	SignedOK      int `json:"signed_ok"`
	Unsigned      int `json:"unsigned"`
	SignedInvalid int `json:"signed_invalid"`
}

func verifyOneSignature(ctx context.Context, store storage.Store, r *storage.ReceiptRow, verifier *receipt.Ed25519Verifier) receiptSignatureResult {
	res := receiptSignatureResult{
		SessionID: r.SessionID,
		Sequence:  r.Sequence,
		ReceiptID: r.ID,
		KeyID:     r.SignerKeyID,
	}
	if len(r.Signature) == 0 {
		res.Status = signatureStatusUnsigned
		return res
	}
	if verifier == nil {
		res.Status = signatureStatusInvalid
		res.Reason = "no trust store configured"
		emitSignatureInvalidAudit(ctx, store, r, res.Reason)
		return res
	}
	if !verifier.HasKey(r.SignerKeyID) {
		res.Status = signatureStatusInvalid
		res.Reason = "unknown signer_key_id"
		emitSignatureInvalidAudit(ctx, store, r, res.Reason)
		return res
	}
	if err := verifier.Verify(r.Hash, r.Signature, r.SignerKeyID); err != nil {
		res.Status = signatureStatusInvalid
		res.Reason = err.Error()
		emitSignatureInvalidAudit(ctx, store, r, res.Reason)
		return res
	}
	res.Status = signatureStatusOK
	return res
}

func emitSignatureInvalidAudit(ctx context.Context, store storage.Store, r *storage.ReceiptRow, reason string) {
	if store == nil {
		return
	}
	details := map[string]interface{}{
		"session_id":    r.SessionID.String(),
		"sequence":      r.Sequence,
		"signer_key_id": r.SignerKeyID,
	}
	if err := logSelfAudit(ctx, store, SelfAuditActionReceiptSignatureInvalid, "",
		details, SelfAuditResultError, reason); err != nil {
		log.Errorf("failed to record receipt_signature_invalid audit: %v", err)
	}
}

func summarizeSignatureResults(results []receiptSignatureResult) signatureSummary {
	var s signatureSummary
	for _, r := range results {
		switch r.Status {
		case signatureStatusOK:
			s.SignedOK++
		case signatureStatusUnsigned:
			s.Unsigned++
		case signatureStatusInvalid:
			s.SignedInvalid++
		}
	}
	return s
}

func writeReceiptsJSONWithSignatures(w io.Writer, rows []*storage.ReceiptRow, breaks []receiptVerifyBreak, sigResults []receiptSignatureResult, summary signatureSummary) error {
	views := make([]policyReceiptView, 0, len(rows))
	for _, r := range rows {
		views = append(views, receiptToView(r))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	out := map[string]interface{}{
		"receipts":          views,
		"chain_breaks":      breaks,
		"signature_results": sigResults,
		"signature_summary": summary,
	}
	return enc.Encode(out)
}

func renderSignatureVerifyResults(w io.Writer, c *tui.Colorizer, summary signatureSummary, results []receiptSignatureResult) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "%s signed_ok=%d unsigned=%d signed_invalid=%d\n",
		c.Header("Signature verification:"),
		summary.SignedOK, summary.Unsigned, summary.SignedInvalid)
	if summary.SignedInvalid == 0 {
		return
	}
	for _, r := range results {
		if r.Status != signatureStatusInvalid {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %s session=%s seq=%d key_id=%s %s\n",
			c.Error("INVALID"),
			tui.FormatShortID(r.SessionID.String()),
			r.Sequence, r.KeyID, r.Reason)
	}
}
