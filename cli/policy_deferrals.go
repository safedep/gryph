package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

const policyDeferralsDefaultLimit = 100

func newPolicyDeferralsCmd() *cobra.Command {
	var (
		status    string
		sessionID string
		limit     int
		format    string
	)
	cmd := &cobra.Command{
		Use:   "deferrals",
		Short: "List and resolve AARM-deferred actions",
		Long: "List, resolve, and sweep timed-out actions deferred by the " +
			"AARM Mediator. A defer pauses the agent's tool call by blocking " +
			"it and queues a pending-deferral row that an operator resolves " +
			"out-of-band (or the timeout sweep flips to deny).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
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

			filter := &storage.DeferredActionFilter{Limit: limit}
			if status != "" && status != "all" {
				if !isValidDeferralStatus(status) {
					return ErrConfig("invalid status filter", fmt.Errorf("status %q must be one of pending, resolved_allow, resolved_deny, resolved_timeout, all", status))
				}
				filter.Status = status
			}
			if sessionID != "" {
				sid, rerr := resolveAarmSessionID(ctx, app.Store, sessionID)
				if rerr != nil {
					return rerr
				}
				filter.SessionID = &sid
			}
			rows, err := app.Store.QueryDeferredActions(ctx, filter)
			if err != nil {
				return fmt.Errorf("failed to query deferred actions: %w", err)
			}
			out := cmd.OutOrStdout()
			if format == "json" {
				return writeDeferralsJSON(out, rows)
			}
			renderDeferralsTable(out, policyColorizer(app), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "pending", "status filter: pending, resolved_allow, resolved_deny, resolved_timeout, all")
	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (UUID or prefix) to filter on")
	cmd.Flags().IntVar(&limit, "limit", policyDeferralsDefaultLimit, "maximum number of rows to return")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")

	cmd.AddCommand(newPolicyDeferralsResolveCmd(), newPolicyDeferralsSweepCmd())
	return cmd
}

func isValidDeferralStatus(s string) bool {
	switch s {
	case storage.DeferredActionStatusPending,
		storage.DeferredActionStatusResolvedAllow,
		storage.DeferredActionStatusResolvedDeny,
		storage.DeferredActionStatusResolvedTimeout:
		return true
	}
	return false
}

func newPolicyDeferralsResolveCmd() *cobra.Command {
	var (
		id       string
		decision string
		note     string
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve a pending deferral to allow or deny",
		Long: "Resolves a pending deferral by id or id-prefix. The deferral " +
			"row transitions to resolved_allow / resolved_deny, a follow-up " +
			"receipt is appended to the same session with the resolved " +
			"decision, and a deferral_resolved self-audit row is emitted. " +
			"--decision allow prompts for confirmation unless --yes is set.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			if id == "" {
				return ErrConfig("missing --id", fmt.Errorf("--id is required"))
			}
			decision = strings.ToLower(strings.TrimSpace(decision))
			if decision != "allow" && decision != "deny" {
				return ErrConfig("invalid --decision", fmt.Errorf("--decision must be allow or deny"))
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

			row, err := app.Store.GetDeferredActionByPrefix(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to resolve deferral id: %w", err)
			}
			if row == nil {
				return ErrConfig("no deferral matches id", fmt.Errorf("id %q did not match any deferred-action row", id))
			}
			if row.Status != storage.DeferredActionStatusPending {
				return ErrConfig("deferral already resolved", fmt.Errorf("deferral %s is in status %s", row.ID, row.Status))
			}

			out := cmd.OutOrStdout()
			if decision == "allow" && !yes {
				confirmed, cerr := confirmAllowResolution(out, cmd.InOrStdin(), row.ID.String())
				if cerr != nil {
					return cerr
				}
				if !confirmed {
					_, _ = fmt.Fprintf(out, "Aborted: deferral %s not resolved\n", tui.FormatShortID(row.ID.String()))
					return nil
				}
			}

			resolver := operatorIdentity()
			if err := resolveDeferralRow(ctx, app.Store, row, decision, resolver, note); err != nil {
				return err
			}

			c := policyColorizer(app)
			_, _ = fmt.Fprintf(out, "%s Resolved deferral %s to %s\n",
				c.StatusOK(), c.Cyan(tui.FormatShortID(row.ID.String())), c.Success(decision))
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "deferred-action id or id-prefix (required)")
	cmd.Flags().StringVar(&decision, "decision", "", "resolution decision: allow or deny (required)")
	cmd.Flags().StringVar(&note, "note", "", "optional resolution note recorded on the row and follow-up receipt")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the interactive confirmation prompt for --decision allow")
	return cmd
}

// confirmAllowResolution prints a one-line confirmation prompt and reads a
// single line from in. Returns true only on a "y" / "yes" reply
// (case-insensitive). A bare newline, EOF, or any other reply is treated as
// a no-op so the operator must explicitly type yes to widen the policy.
func confirmAllowResolution(out io.Writer, in io.Reader, deferralID string) (bool, error) {
	_, _ = fmt.Fprintf(out, "Resolve deferral %s as ALLOW? [y/N]: ", tui.FormatShortID(deferralID))
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("failed to read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func newPolicyDeferralsSweepCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "sweep",
		Short: "Flip expired pending deferrals to resolved_timeout",
		Long: "Walks the pending-deferral queue and flips every row whose " +
			"expires_at is past to resolved_timeout. AARM R4 forbids implicit " +
			"allow on timeout, so each timed-out row is followed by a deny " +
			"receipt and a deferral_timeout self-audit row. The sweep itself " +
			"emits a deferral_sweep summary row.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
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

			now := time.Now().UTC()
			pending, err := app.Store.QueryDeferredActions(ctx, &storage.DeferredActionFilter{
				Status:        storage.DeferredActionStatusPending,
				ExpiredBefore: &now,
				Limit:         -1,
			})
			if err != nil {
				return fmt.Errorf("failed to query expired deferrals: %w", err)
			}
			out := cmd.OutOrStdout()
			if dryRun {
				_, _ = fmt.Fprintf(out, "Would flip %d expired deferral(s) to resolved_timeout\n", len(pending))
				return nil
			}
			processed := 0
			for _, row := range pending {
				if err := timeoutDeferralRow(ctx, app.Store, row, now); err != nil {
					log.Errorf("failed to timeout deferral %s: %v", row.ID, err)
					continue
				}
				processed++
			}
			details := map[string]interface{}{
				"processed":    processed,
				"candidates":   len(pending),
				"swept_before": now.Format(time.RFC3339),
			}
			if err := logSelfAudit(ctx, app.Store, SelfAuditActionDeferralSweep, "",
				details, SelfAuditResultSuccess, ""); err != nil {
				log.Errorf("failed to record deferral_sweep audit: %v", err)
			}
			_, _ = fmt.Fprintf(out, "Swept %d expired deferral(s) (of %d candidates)\n", processed, len(pending))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be flipped without changing state")
	return cmd
}

// deferralResolution describes the structured intent applied by
// applyDeferralResolution. Both the operator-driven resolve path and the
// timeout sweep path build one of these and hand it to the shared helper so
// the receipt-first + idempotency ordering lives in a single place.
type deferralResolution struct {
	Status           string
	FollowUpDecision model.Decision
	FollowUpResult   model.ResultStatus
	FollowUpMessage  string
	Resolver         string
	Note             string
	AuditAction      string
	ExtraDetails     map[string]interface{}
}

// applyDeferralResolution flips the deferred-action row to its terminal
// state, inserts the follow-up receipt in the same session with
// deferral_of_sequence set to the original defer receipt's sequence, and
// emits the matching self-audit row.
//
// Failure ordering: the follow-up receipt is inserted FIRST, then the
// deferred-action row is flipped. A failure between those two steps leaves
// the row pending with a follow-up already in place. The next retry detects
// the existing follow-up via GetFollowUpReceipt and skips re-inserting it
// before flipping the row, so the operation is idempotent. The inverse
// order (flip row first) would strand the row in resolved status with no
// follow-up receipt on a partial failure, which AARM R4 forbids.
func applyDeferralResolution(ctx context.Context, store storage.Store, row *storage.DeferredActionRow, now time.Time, spec deferralResolution) error {
	original, err := findOriginalReceipt(ctx, store, row.SessionID, row.ReceiptSequence)
	if err != nil {
		return err
	}

	existing, err := store.GetFollowUpReceipt(ctx, row.SessionID, row.ReceiptSequence)
	if err != nil {
		return fmt.Errorf("failed to check existing follow-up receipt: %w", err)
	}

	var followUpSeq int64
	if existing != nil {
		followUpSeq = existing.Sequence
	} else {
		gen := receipt.NewSQLite(store)
		originalSeq := row.ReceiptSequence
		deferralOf := &originalSeq
		evalRes := &model.EvaluationResult{
			Decision: spec.FollowUpDecision,
			Message:  spec.FollowUpMessage,
		}
		rec, recErr := gen.Record(ctx, &receipt.RecordInput{
			SessionID:          row.SessionID,
			ActionID:           row.ActionID,
			Action:             followUpActionFrom(row, original),
			Decision:           evalRes,
			RecordedAt:         now,
			DeferReason:        row.Reason,
			DeferralOfSequence: deferralOf,
		})
		if recErr != nil {
			return fmt.Errorf("failed to record follow-up receipt: %w", recErr)
		}
		if rec != nil && spec.FollowUpResult != "" {
			if err := gen.UpdateResult(ctx, row.SessionID, rec.Sequence, model.Result{Status: spec.FollowUpResult}); err != nil {
				log.Warnf("aarm: receipt update result: %v", err)
			}
		}
		followUpSeq = followUpSeqOf(rec)
	}

	if err := store.UpdateDeferredActionResolution(ctx, row.ID, spec.Status, spec.Resolver, spec.Note, now); err != nil {
		return fmt.Errorf("failed to update deferral row: %w", err)
	}

	details := map[string]interface{}{
		"deferred_action_id":    row.ID.String(),
		"session_id":            row.SessionID.String(),
		"original_seq":          row.ReceiptSequence,
		"resolver":              spec.Resolver,
		"follow_up_receipt_seq": followUpSeq,
		"deferral_status":       spec.Status,
		"original_defer_reason": row.Reason,
		"follow_up_reused":      existing != nil,
	}
	for k, v := range spec.ExtraDetails {
		details[k] = v
	}
	if err := logSelfAudit(ctx, store, spec.AuditAction, "",
		details, SelfAuditResultSuccess, ""); err != nil {
		log.Errorf("failed to record %s audit: %v", spec.AuditAction, err)
	}
	return nil
}

// resolveDeferralRow flips a pending row to resolved_allow / resolved_deny,
// inserts the follow-up receipt with the resolved decision, and emits a
// deferral_resolved self-audit row. Thin wrapper around
// applyDeferralResolution that supplies the operator-driven spec.
func resolveDeferralRow(ctx context.Context, store storage.Store, row *storage.DeferredActionRow, decision, resolver, note string) error {
	spec := deferralResolution{
		Status:           storage.DeferredActionStatusResolvedDeny,
		FollowUpDecision: model.DecisionBlock,
		FollowUpResult:   model.ResultRejected,
		Resolver:         resolver,
		Note:             note,
		AuditAction:      SelfAuditActionDeferralResolved,
	}
	if decision == "allow" {
		spec.Status = storage.DeferredActionStatusResolvedAllow
		spec.FollowUpDecision = model.DecisionAllow
		spec.FollowUpResult = model.ResultSuccess
	}
	spec.FollowUpMessage = fmt.Sprintf("deferral resolved: %s", spec.FollowUpDecision)
	spec.ExtraDetails = map[string]interface{}{
		"resolution":               string(spec.FollowUpDecision),
		"resolution_note_recorded": note != "",
	}
	return applyDeferralResolution(ctx, store, row, time.Now().UTC(), spec)
}

// timeoutDeferralRow flips a single expired pending deferral to
// resolved_timeout, inserts a deny follow-up receipt, and emits a per-row
// deferral_timeout self-audit row. Thin wrapper around
// applyDeferralResolution that supplies the timeout-sweep spec.
func timeoutDeferralRow(ctx context.Context, store storage.Store, row *storage.DeferredActionRow, now time.Time) error {
	spec := deferralResolution{
		Status:           storage.DeferredActionStatusResolvedTimeout,
		FollowUpDecision: model.DecisionBlock,
		FollowUpResult:   model.ResultRejected,
		FollowUpMessage:  fmt.Sprintf("deferral %s timed out", row.ID.String()[:8]),
		Resolver:         "system:timeout",
		Note:             "deferral expired",
		AuditAction:      SelfAuditActionDeferralTimeout,
		ExtraDetails: map[string]interface{}{
			"expires_at": row.ExpiresAt.Format(time.RFC3339),
		},
	}
	return applyDeferralResolution(ctx, store, row, now, spec)
}

func followUpSeqOf(rec *receipt.Record) int64 {
	if rec == nil {
		return 0
	}
	return rec.Sequence
}

// findOriginalReceipt returns the original defer receipt row for the
// (session_id, sequence) pair so the follow-up receipt can carry the same
// action_type / agent / tool. Returns (nil, nil) when the original is gone
// (e.g. pruned by retention). The caller should still record the follow-up.
func findOriginalReceipt(ctx context.Context, store storage.Store, sessionID uuid.UUID, sequence int64) (*storage.ReceiptRow, error) {
	rec, err := store.GetReceiptBySessionSequence(ctx, sessionID, sequence)
	if err != nil {
		return nil, fmt.Errorf("failed to load original defer receipt: %w", err)
	}
	return rec, nil
}

// followUpActionFrom builds a minimal model.Action for a resolution or
// timeout follow-up receipt. We copy the originating receipt's action_type /
// agent / tool / project so the chained hash inputs do not record the
// fallback "unknown" action_type for the follow-up row.
func followUpActionFrom(row *storage.DeferredActionRow, original *storage.ReceiptRow) *model.Action {
	a := &model.Action{
		ID:        row.ActionID,
		SessionID: row.SessionID,
	}
	if original != nil {
		a.Type = model.ActionType(original.ActionType)
		a.Agent = original.Agent
		a.Tool = original.Tool
		a.Project = original.Project
	}
	return a
}

func operatorIdentity() string {
	id := detectOperatorIdentity()
	if id == "operator" {
		return "cli"
	}
	return "cli:" + id
}

func renderDeferralsTable(w io.Writer, c *tui.Colorizer, rows []*storage.DeferredActionRow) {
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(w, c.Dim("No deferrals match the filter."))
		return
	}
	_, _ = fmt.Fprintln(w, c.Header("Deferrals"))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(100))
	headerPrefix := fmt.Sprintf("  %-13s  %-13s  %-5s  %-16s  %-20s  %-20s  %s",
		c.Dim("id"), c.Dim("session"), c.Dim("seq"), c.Dim("status"),
		c.Dim("deferred_at"), c.Dim("expires_at"), c.Dim("reason"))
	_, _ = fmt.Fprintln(w, headerPrefix)
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "  %-13s  %-13s  %-5d  %-16s  %-20s  %-20s  %s\n",
			tui.FormatShortID(r.ID.String()),
			tui.FormatShortID(r.SessionID.String()),
			r.ReceiptSequence,
			r.Status,
			r.DeferredAt.Format("2006-01-02 15:04:05"),
			r.ExpiresAt.Format("2006-01-02 15:04:05"),
			tui.TruncateString(r.Reason, 40),
		)
	}
}

type deferralView struct {
	ID              string `json:"id"`
	SessionID       string `json:"session_id"`
	ReceiptSequence int64  `json:"receipt_sequence"`
	ActionID        string `json:"action_id"`
	DeferredAt      string `json:"deferred_at"`
	ExpiresAt       string `json:"expires_at"`
	Reason          string `json:"reason"`
	Status          string `json:"status"`
	ResolvedAt      string `json:"resolved_at,omitempty"`
	Resolver        string `json:"resolver,omitempty"`
	ResolutionNote  string `json:"resolution_note,omitempty"`
}

func deferralToView(r *storage.DeferredActionRow) deferralView {
	v := deferralView{
		ID:              r.ID.String(),
		SessionID:       r.SessionID.String(),
		ReceiptSequence: r.ReceiptSequence,
		ActionID:        r.ActionID.String(),
		DeferredAt:      r.DeferredAt.Format(time.RFC3339Nano),
		ExpiresAt:       r.ExpiresAt.Format(time.RFC3339Nano),
		Reason:          r.Reason,
		Status:          r.Status,
		Resolver:        r.Resolver,
		ResolutionNote:  r.ResolutionNote,
	}
	if r.ResolvedAt != nil {
		v.ResolvedAt = r.ResolvedAt.Format(time.RFC3339Nano)
	}
	return v
}

func writeDeferralsJSON(w io.Writer, rows []*storage.DeferredActionRow) error {
	views := make([]deferralView, 0, len(rows))
	for _, r := range rows {
		views = append(views, deferralToView(r))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]interface{}{"deferrals": views})
}

