package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/safedep/dry/log"
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

			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %v", err)
				}
			}()

			days := app.Config.Storage.RetentionDays
			policyCfg := app.Config.EffectivePolicy()
			contextDays := policyCfg.ContextRetentionDays
			if days == 0 && contextDays == 0 {
				fmt.Fprintln(os.Stderr, "Retention policy disabled (retention_days=0, policy.context_retention_days=0)")
				return nil
			}

			now := time.Now()
			var eventCutoff, contextCutoff time.Time
			if days > 0 {
				eventCutoff = now.AddDate(0, 0, -days)
			}
			if contextDays > 0 {
				contextCutoff = now.AddDate(0, 0, -contextDays)
			}

			if dryRun {
				if days > 0 {
					count, err := app.Store.CountEventsBefore(ctx, eventCutoff)
					if err != nil {
						return err
					}
					fmt.Printf("Would delete %d events older than %s (%d days)\n",
						count, eventCutoff.Format(time.RFC3339), days)
				}
				if contextDays > 0 {
					count, err := app.Store.CountContextBefore(ctx, contextCutoff)
					if err != nil {
						return err
					}
					fmt.Printf("Would delete %d context actions older than %s (%d days)\n",
						count, contextCutoff.Format(time.RFC3339), contextDays)
				}
				return nil
			}

			var eventsDeleted, contextDeleted int
			if days > 0 {
				n, err := app.Store.DeleteEventsBefore(ctx, eventCutoff)
				if err != nil {
					return err
				}
				eventsDeleted = n
				details := map[string]interface{}{
					"events_deleted": n,
					"cutoff_time":    eventCutoff.Format(time.RFC3339),
					"retention_days": days,
				}
				if err := logSelfAudit(ctx, app.Store, SelfAuditActionRetentionCleanup, "",
					details, SelfAuditResultSuccess, ""); err != nil {
					log.Errorf("failed to log self-audit: %v", err)
				}
			}
			if contextDays > 0 {
				n, err := app.Store.DeleteContextBefore(ctx, contextCutoff)
				if err != nil {
					return err
				}
				contextDeleted = n
				if contextDeleted > 0 {
					details := map[string]interface{}{
						"aarm_context_actions_deleted": contextDeleted,
						"cutoff":                       contextCutoff.Format(time.RFC3339),
						"context_retention_days":       contextDays,
					}
					if err := logSelfAudit(ctx, app.Store, SelfAuditActionContextCleanup, "",
						details, SelfAuditResultSuccess, ""); err != nil {
						log.Errorf("failed to log self-audit: %v", err)
					}
				}
			}

			if days > 0 {
				fmt.Printf("Deleted %d events older than %d days\n", eventsDeleted, days)
			}
			if contextDays > 0 {
				fmt.Printf("Deleted %d context actions older than %d days\n", contextDeleted, contextDays)
			}
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

			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to initialize database", err)
			}

			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %v", err)
				}
			}()

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
