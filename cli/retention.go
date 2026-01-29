package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// NewRetentionCmd creates the retention command.
func NewRetentionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retention",
		Short: "Manage data retention",
		Long: `Manage data retention.

Commands for managing audit data retention policy including
cleaning up old events based on the configured retention period.`,
	}

	cmd.AddCommand(newRetentionCleanupCmd())
	cmd.AddCommand(newRetentionStatusCmd())

	return cmd
}

// newRetentionCleanupCmd creates the retention cleanup subcommand.
func newRetentionCleanupCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Delete events older than retention policy",
		Long: `Delete events older than retention policy.

Removes audit events older than the configured retention_days setting.
Self-audit entries are preserved and not affected by this cleanup.`,
		Example: `  gryph retention cleanup
  gryph retention cleanup --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Initialize store
			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer app.Close()

			days := app.Config.Storage.RetentionDays
			if days == 0 {
				fmt.Fprintln(os.Stderr, "Retention policy disabled (retention_days=0)")
				return nil
			}

			cutoff := time.Now().AddDate(0, 0, -days)

			if dryRun {
				// Show what would be deleted
				count, err := app.Store.CountEventsBefore(ctx, cutoff)
				if err != nil {
					return err
				}
				fmt.Printf("Would delete %d events older than %s (%d days)\n",
					count, cutoff.Format(time.RFC3339), days)
				return nil
			}

			deleted, err := app.Store.DeleteEventsBefore(ctx, cutoff)
			if err != nil {
				return err
			}

			// Log self-audit
			logSelfAudit(ctx, app.Store, SelfAuditActionRetentionCleanup, "",
				map[string]interface{}{
					"events_deleted": deleted,
					"cutoff_time":    cutoff.Format(time.RFC3339),
					"retention_days": days,
				},
				SelfAuditResultSuccess, "")

			fmt.Printf("Deleted %d events older than %d days\n", deleted, days)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted without deleting")

	return cmd
}

// newRetentionStatusCmd creates the retention status subcommand.
func newRetentionStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show retention policy status",
		Long: `Show retention policy status.

Displays the current retention configuration and statistics about
events that would be affected by cleanup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Initialize store
			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer app.Close()

			days := app.Config.Storage.RetentionDays
			fmt.Printf("Retention Policy:\n")
			if days == 0 {
				fmt.Printf("  Status:          Disabled\n")
				fmt.Printf("  Retention Days:  Unlimited\n")
			} else {
				fmt.Printf("  Status:          Enabled\n")
				fmt.Printf("  Retention Days:  %d\n", days)

				cutoff := time.Now().AddDate(0, 0, -days)
				fmt.Printf("  Cutoff Date:     %s\n", cutoff.Format("2006-01-02 15:04:05"))

				// Count events that would be deleted
				count, err := app.Store.CountEventsBefore(ctx, cutoff)
				if err != nil {
					return err
				}
				fmt.Printf("  Events to Clean: %d\n", count)
			}

			return nil
		},
	}

	return cmd
}
