package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/storage"
	"github.com/spf13/cobra"
)

// retentionBucket describes one slice of audit data subject to retention.
// Each bucket owns its own count/delete operations and the self-audit row
// emitted after a successful delete. Adding a fourth bucket is a one-liner
// addition to the slice built in the cleanup runner.
type retentionBucket struct {
	nounPlural  string
	days        int
	auditAction string
	auditKey    string
	cutoffKey   string
	daysKey     string
	auditOnZero bool
	countFn     func(ctx context.Context, before time.Time) (int, error)
	deleteFn    func(ctx context.Context, before time.Time) (int, error)
}

// enabled reports whether the bucket is configured for retention sweeps.
func (b retentionBucket) enabled() bool { return b.days > 0 }

// cutoff returns the timestamp before which entries are eligible for delete.
func (b retentionBucket) cutoff(now time.Time) time.Time {
	return now.AddDate(0, 0, -b.days)
}

func retentionBuckets(store storage.Store, eventDays, contextDays, receiptDays int) []retentionBucket {
	return []retentionBucket{
		{
			nounPlural:  "events",
			days:        eventDays,
			auditAction: SelfAuditActionRetentionCleanup,
			auditKey:    "events_deleted",
			cutoffKey:   "cutoff_time",
			daysKey:     "retention_days",
			auditOnZero: true,
			countFn:     store.CountEventsBefore,
			deleteFn:    store.DeleteEventsBefore,
		},
		{
			nounPlural:  "context actions",
			days:        contextDays,
			auditAction: SelfAuditActionContextCleanup,
			auditKey:    "aarm_context_actions_deleted",
			cutoffKey:   "cutoff",
			daysKey:     "context_retention_days",
			countFn:     store.CountContextBefore,
			deleteFn:    store.DeleteContextBefore,
		},
		{
			nounPlural:  "receipts",
			days:        receiptDays,
			auditAction: SelfAuditActionReceiptCleanup,
			auditKey:    "aarm_receipts_deleted",
			cutoffKey:   "cutoff",
			daysKey:     "receipt_retention_days",
			countFn:     store.CountReceiptsBefore,
			deleteFn:    store.DeleteReceiptsBefore,
		},
	}
}

func reportRetentionDryRun(ctx context.Context, w io.Writer, b retentionBucket, now time.Time) error {
	if !b.enabled() {
		return nil
	}
	cutoff := b.cutoff(now)
	count, err := b.countFn(ctx, cutoff)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "Would delete %d %s older than %s (%d days)\n",
		count, b.nounPlural, cutoff.Format(time.RFC3339), b.days)
	return nil
}

func runRetentionDelete(ctx context.Context, w io.Writer, store storage.Store, b retentionBucket, now time.Time) (int, error) {
	if !b.enabled() {
		return 0, nil
	}
	cutoff := b.cutoff(now)
	deleted, err := b.deleteFn(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if deleted > 0 || b.auditOnZero {
		details := map[string]interface{}{
			b.auditKey:  deleted,
			b.cutoffKey: cutoff.Format(time.RFC3339),
			b.daysKey:   b.days,
		}
		if err := logSelfAudit(ctx, store, b.auditAction, "",
			details, SelfAuditResultSuccess, ""); err != nil {
			log.Errorf("failed to log self-audit: %v", err)
		}
	}
	_, _ = fmt.Fprintf(w, "Deleted %d %s older than %d days\n", deleted, b.nounPlural, b.days)
	return deleted, nil
}

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
			receiptDays := policyCfg.ReceiptRetentionDays
			if days == 0 && contextDays == 0 && receiptDays == 0 {
				fmt.Fprintln(os.Stderr, "Retention policy disabled (retention_days=0, policy.context_retention_days=0, policy.receipt_retention_days=0)")
				return nil
			}

			now := time.Now()
			buckets := retentionBuckets(app.Store, days, contextDays, receiptDays)

			if dryRun {
				for _, b := range buckets {
					if err := reportRetentionDryRun(ctx, os.Stdout, b, now); err != nil {
						return err
					}
				}
				return nil
			}

			for _, b := range buckets {
				if _, err := runRetentionDelete(ctx, os.Stdout, app.Store, b, now); err != nil {
					return err
				}
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
			policyCfg := app.Config.EffectivePolicy()
			receiptDays := policyCfg.ReceiptRetentionDays
			contextDays := policyCfg.ContextRetentionDays

			fmt.Printf("Retention Policy:\n")
			if days == 0 {
				fmt.Printf("  Events Status:           Disabled\n")
				fmt.Printf("  Events Retention Days:   Unlimited\n")
			} else {
				fmt.Printf("  Events Status:           Enabled\n")
				fmt.Printf("  Events Retention Days:   %d\n", days)

				cutoff := time.Now().AddDate(0, 0, -days)
				fmt.Printf("  Events Cutoff Date:      %s\n", cutoff.Format("2006-01-02 15:04:05"))

				count, err := app.Store.CountEventsBefore(ctx, cutoff)
				if err != nil {
					return err
				}
				fmt.Printf("  Events to Clean:         %d\n", count)
			}

			if contextDays > 0 {
				fmt.Printf("  Context Status:          Enabled\n")
				fmt.Printf("  Context Retention Days:  %d\n", contextDays)
				cutoff := time.Now().AddDate(0, 0, -contextDays)
				fmt.Printf("  Context Cutoff Date:     %s\n", cutoff.Format("2006-01-02 15:04:05"))
				count, err := app.Store.CountContextBefore(ctx, cutoff)
				if err != nil {
					return err
				}
				fmt.Printf("  Context to Clean:        %d\n", count)
			}

			if receiptDays > 0 {
				fmt.Printf("  Receipts Status:         Enabled\n")
				fmt.Printf("  Receipts Retention Days: %d\n", receiptDays)
				cutoff := time.Now().AddDate(0, 0, -receiptDays)
				fmt.Printf("  Receipts Cutoff Date:    %s\n", cutoff.Format("2006-01-02 15:04:05"))
				count, err := app.Store.CountReceiptsBefore(ctx, cutoff)
				if err != nil {
					return err
				}
				fmt.Printf("  Receipts to Clean:       %d\n", count)
			}

			return nil
		},
	}

	return cmd
}
