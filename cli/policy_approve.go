package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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
		Long: "Manage the approval workflow for AARM-escalated actions. " +
			"The CLI-only frontend keeps in-flight requests in-process; " +
			"history queries the receipt log for approval-related decisions.",
	}
	cmd.AddCommand(newPolicyApproveListCmd(), newPolicyApproveHistoryCmd())
	return cmd
}

func newPolicyApproveListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending approval requests (stub)",
		Long: "Lists pending approval requests. With the CLI-only frontend, " +
			"requests live only in the prompting process and cannot be " +
			"enumerated across processes. A persistent queue lands in Phase 4.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := loadApp()
			if err != nil {
				log.Warnf("loadApp failed during policy approve list: %v", err)
			}
			c := policyColorizer(app)
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, c.Dim("no pending approvals"))
			_, _ = fmt.Fprintln(out, c.Dim("CLI prompts block in-process; a persistent queue lands in Phase 4"))
			return nil
		},
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
			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				if cerr := app.Close(); cerr != nil {
					log.Errorf("failed to close app: %v", cerr)
				}
			}()

			rows, err := queryApprovalHistory(ctx, app.Store, sessionID, limit)
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

func queryApprovalHistory(ctx context.Context, store storage.Store, sessionID string, limit int) ([]*storage.ReceiptRow, error) {
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
