package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

const policyApproveDefaultLimit = 50

func newPolicyApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve",
		Short: "Inspect and act on approval requests for escalated actions",
		Long: "Manage the approval workflow for escalated actions. " +
			"On a managed host the decision service keeps a request queue: " +
			"list shows the open requests of this account, show prints one. " +
			"History queries the receipt log for approval-related decisions.",
	}
	cmd.AddCommand(newPolicyApproveListCmd(), newPolicyApproveShowCmd(), newPolicyApproveWatchCmd(), newPolicyApproveResolveCmd(), newPolicyApproveHistoryCmd())
	return cmd
}

func newPolicyApproveListCmd() *cobra.Command {
	var (
		state  string
		limit  int
		format string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pending approval requests",
		Long: "Lists the approval requests of this account. On a managed host " +
			"the decision service keeps the queue, and a request stays pending " +
			"until an approver answers or it expires. A hook that decides in " +
			"process asks on its own terminal and keeps no queue, so the list " +
			"is then empty. Use `gryph policy approve history` for past decisions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			app, err := loadApp()
			if err != nil {
				return err
			}
			if err := app.InitReadStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				if cerr := app.Close(); cerr != nil {
					log.Errorf("failed to close app: %v", cerr)
				}
			}()
			filter := &storage.ApprovalRequestFilter{Limit: limit, AllAccounts: true}
			if state != "all" {
				if !isValidApprovalState(state) {
					return ErrConfig("invalid state filter", fmt.Errorf("state %q must be one of pending, approved, denied, expired, all", state))
				}
				filter.State = state
			}
			rows, err := app.Reads.QueryApprovalRequests(ctx, filter)
			if err != nil {
				return fmt.Errorf("failed to query approval requests: %w", err)
			}
			out := cmd.OutOrStdout()
			if format == "json" {
				return writeApprovalRequestsJSON(out, rows)
			}
			renderApprovalRequestsTable(out, policyColorizer(app), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", storage.ApprovalRequestPending, "state filter: pending, approved, denied, expired, all")
	cmd.Flags().IntVar(&limit, "limit", policyApproveDefaultLimit, "maximum number of requests to return")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	return cmd
}

func newPolicyApproveShowCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one approval request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			app, err := loadApp()
			if err != nil {
				return err
			}
			if err := app.InitReadStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				if cerr := app.Close(); cerr != nil {
					log.Errorf("failed to close app: %v", cerr)
				}
			}()
			row, err := app.Reads.GetApprovalRequestByPrefix(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to find the approval request: %w", err)
			}
			if row == nil {
				return ErrConfig("no approval request matches id", fmt.Errorf("id %q did not match any request", args[0]))
			}
			out := cmd.OutOrStdout()
			if format == "json" {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(approvalRequestToView(row))
			}
			renderApprovalRequest(out, policyColorizer(app), row)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	return cmd
}

func isValidApprovalState(s string) bool {
	switch s {
	case storage.ApprovalRequestPending, storage.ApprovalRequestApproved, storage.ApprovalRequestDenied, storage.ApprovalRequestExpired:
		return true
	}
	return false
}

type approvalRequestView struct {
	ID           string   `json:"id"`
	SessionID    string   `json:"session_id"`
	ActionID     string   `json:"action_id"`
	Sequence     int64    `json:"receipt_sequence"`
	ActionDigest string   `json:"action_digest"`
	Rules        []string `json:"rules,omitempty"`
	Requester    string   `json:"requester,omitempty"`
	Host         string   `json:"host,omitempty"`
	Agent        string   `json:"agent,omitempty"`
	Summary      string   `json:"summary"`
	Project      string   `json:"project,omitempty"`
	MinAssurance string   `json:"min_assurance,omitempty"`
	State        string   `json:"state"`
	RequestedAt  string   `json:"requested_at"`
	ExpiresAt    string   `json:"expires_at"`
	Inline       bool     `json:"inline"`
	DecidedAt    string   `json:"decided_at,omitempty"`
	Channel      string   `json:"channel,omitempty"`
	Assurance    string   `json:"assurance,omitempty"`
	Approver     string   `json:"approver,omitempty"`
	PeerTrust    string   `json:"peer_trust,omitempty"`
	Note         string   `json:"note,omitempty"`
	Scope        string   `json:"scope,omitempty"`
}

func approvalRequestToView(r *storage.ApprovalRequestRow) approvalRequestView {
	v := approvalRequestView{
		ID:           r.ID.String(),
		SessionID:    r.SessionID.String(),
		ActionID:     r.ActionID.String(),
		Sequence:     r.ReceiptSequence,
		ActionDigest: r.ActionDigest,
		Rules:        r.RuleIDs,
		Requester:    r.Requester,
		Host:         r.Host,
		Agent:        r.Agent,
		Summary:      r.Summary,
		Project:      r.Project,
		MinAssurance: r.MinAssurance,
		State:        r.State,
		RequestedAt:  r.RequestedAt.Format(time.RFC3339Nano),
		ExpiresAt:    r.ExpiresAt.Format(time.RFC3339Nano),
		Inline:       r.Inline,
		Channel:      r.Channel,
		Assurance:    r.Assurance,
		Approver:     r.Approver,
		PeerTrust:    r.PeerTrust,
		Note:         r.Note,
		Scope:        r.Scope,
	}
	if r.DecidedAt != nil {
		v.DecidedAt = r.DecidedAt.Format(time.RFC3339Nano)
	}
	return v
}

func writeApprovalRequestsJSON(w io.Writer, rows []*storage.ApprovalRequestRow) error {
	views := make([]approvalRequestView, 0, len(rows))
	for _, r := range rows {
		views = append(views, approvalRequestToView(r))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]interface{}{"requests": views})
}

func renderApprovalRequestsTable(w io.Writer, c *tui.Colorizer, rows []*storage.ApprovalRequestRow) {
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(w, c.Dim("no pending approvals"))
		return
	}
	_, _ = fmt.Fprintln(w, c.Header("Approval requests"))
	_, _ = fmt.Fprintln(w, tui.HorizontalLine(100))
	_, _ = fmt.Fprintf(w, "  %-9s  %-20s  %-12s  %-16s  %-9s  %-8s  %s\n",
		c.Dim("id"), c.Dim("user"), c.Dim("agent"), c.Dim("rule"), c.Dim("state"), c.Dim("age"), c.Dim("action"))
	for _, r := range rows {
		rule := ""
		if len(r.RuleIDs) > 0 {
			rule = r.RuleIDs[0]
		}
		_, _ = fmt.Fprintf(w, "  %-9s  %-20s  %-12s  %-16s  %-9s  %-8s  %s\n",
			tui.FormatShortID(r.ID.String()),
			tui.TruncateString(r.Requester, 20),
			tui.TruncateString(r.Agent, 12),
			tui.TruncateString(rule, 16),
			r.State,
			tui.FormatDuration(time.Since(r.RequestedAt)),
			tui.TruncateString(r.Summary, 40),
		)
	}
}

func renderApprovalRequest(w io.Writer, c *tui.Colorizer, r *storage.ApprovalRequestRow) {
	v := approvalRequestToView(r)
	_, _ = fmt.Fprintln(w, c.Header("Approval request "+tui.FormatShortID(v.ID)))
	lines := [][2]string{
		{"id", v.ID}, {"state", v.State}, {"action", v.Summary}, {"digest", v.ActionDigest},
		{"rules", strings.Join(v.Rules, ", ")}, {"user", v.Requester}, {"host", v.Host}, {"agent", v.Agent},
		{"project", v.Project}, {"session", v.SessionID}, {"min_assurance", v.MinAssurance},
		{"requested_at", v.RequestedAt}, {"expires_at", v.ExpiresAt}, {"inline", strconv.FormatBool(v.Inline)},
		{"decided_at", v.DecidedAt}, {"channel", v.Channel}, {"assurance", v.Assurance}, {"approver", v.Approver},
		{"peer_trust", v.PeerTrust}, {"scope", v.Scope}, {"note", v.Note},
	}
	for _, l := range lines {
		if l[1] == "" {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %-14s %s\n", c.Dim(l[0]), l[1])
	}
}

func newPolicyApproveHistoryCmd() *cobra.Command {
	var (
		sessionID string
		limit     int
		format    string
	)
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show approval-related receipts (escalate, approved, denied, approval_timeout)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}
			if err := app.InitReadStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				if cerr := app.Close(); cerr != nil {
					log.Errorf("failed to close app: %v", cerr)
				}
			}()

			rows, err := queryApprovalHistory(ctx, app.Reads, sessionID, limit)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if format == "json" {
				return writeApprovalHistoryJSON(out, rows)
			}
			renderApprovalHistoryTable(out, policyColorizer(app), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (UUID or prefix) to filter on")
	cmd.Flags().IntVar(&limit, "limit", policyApproveDefaultLimit, "maximum number of receipts to return")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	return cmd
}

var approvalHistoryDecisions = []string{"escalate", "approved", "denied", "approval_timeout"}

func queryApprovalHistory(ctx context.Context, store storage.ReadStore, sessionID string, limit int) ([]*storage.ReceiptRow, error) {
	if limit <= 0 {
		limit = policyApproveDefaultLimit
	}

	filter := storage.ReceiptFilter{
		Decisions: approvalHistoryDecisions,
		Limit:     limit,
	}
	if sessionID != "" {
		sid, err := resolveAarmSessionID(ctx, store, sessionID)
		if err != nil {
			return nil, err
		}
		filter.SessionID = &sid
	}

	rows, err := store.QueryReceipts(ctx, &filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query receipts: %w", err)
	}
	// QueryReceipts orders ASC by sequence when SessionID is set; "history"
	// reads more naturally newest-first, so reverse the bounded slice here.
	if filter.SessionID != nil {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	return rows, nil
}

func renderApprovalHistoryTable(w io.Writer, c *tui.Colorizer, rows []*storage.ReceiptRow) {
	renderReceiptsTableWith(w, c, rows, receiptTableTrailing{
		Title:   "Approval history",
		Headers: []string{"note"},
		Format:  "  %s\n",
		Cells: func(r *storage.ReceiptRow) []interface{} {
			return []interface{}{tui.TruncateString(r.ErrorMessage, 40)}
		},
	}, "No approval-related receipts found.")
}

func writeApprovalHistoryJSON(w io.Writer, rows []*storage.ReceiptRow) error {
	views := make([]policyReceiptView, 0, len(rows))
	for _, r := range rows {
		views = append(views, receiptToView(r))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]interface{}{"receipts": views})
}
