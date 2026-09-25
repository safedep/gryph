package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/core/privacy"
	"github.com/spf13/cobra"
)

func newPolicyReceiptsExportCmd() *cobra.Command {
	var (
		sessionID         string
		since             time.Duration
		until             time.Duration
		format            string
		output            string
		includeSignatures bool
		exportProfile     string
	)
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Stream the receipt log to JSONL or CSV",
		Long: "Streams receipts to stdout (or the configured --output file) row " +
			"by row so very large exports do not buffer into memory. " +
			"--format jsonl emits one full receipt per line. --format csv " +
			"emits a flat row per receipt; the snapshot and action_payload " +
			"JSON columns are omitted. Signatures are excluded by default; " +
			"pass --include-signatures to ship the cryptographic material " +
			"with the export. --export-profile applies to the command and the " +
			"URL of each JSONL row. A receipt with hash v2 still verifies, " +
			"because its hash reads their digests.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()

			format = strings.ToLower(strings.TrimSpace(format))
			if format == "" {
				format = receipt.ExportFormatJSONL
			}
			switch format {
			case receipt.ExportFormatJSONL, receipt.ExportFormatCSV:
			default:
				return ErrConfig("invalid --format", fmt.Errorf("got %q, want jsonl or csv", format))
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

			profile, err := app.Config.ExportProfile(exportProfile)
			if err != nil {
				return ErrConfig("invalid export profile", err)
			}
			opts := receipt.ExportOptions{
				Format:            format,
				IncludeSignatures: includeSignatures,
				Profile:           &profile,
				Events:            app.Store,
			}
			if sessionID != "" {
				sid, err := resolveAarmSessionID(ctx, app.Store, sessionID)
				if err != nil {
					return err
				}
				opts.SessionID = &sid
			}
			if since > 0 {
				t := time.Now().Add(-since)
				opts.Since = &t
			}
			if until > 0 {
				t := time.Now().Add(-until)
				opts.Until = &t
			}

			w := io.Writer(cmd.OutOrStdout())
			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return ErrConfig("create output file", err)
				}
				defer func() {
					if cerr := f.Close(); cerr != nil {
						log.Errorf("failed to close export file: %v", cerr)
					}
				}()
				w = f
			}

			var stats receipt.ExportStats
			opts.Stats = &stats
			if err := receipt.NewSQLiteExporter(app.Store).Export(ctx, w, opts); err != nil {
				return err
			}
			return writeReceiptExportWarnings(cmd.ErrOrStderr(), stats)
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (UUID or prefix) to filter on")
	cmd.Flags().DurationVar(&since, "since", 0, "include receipts newer than this offset from now (e.g. 24h)")
	cmd.Flags().DurationVar(&until, "until", 0, "include receipts older than this offset from now")
	cmd.Flags().StringVar(&format, "format", receipt.ExportFormatJSONL, "output format: jsonl, csv")
	cmd.Flags().StringVar(&output, "output", "", "output file (default stdout)")
	cmd.Flags().BoolVar(&includeSignatures, "include-signatures", false, "include signature and signer_key_id columns")
	cmd.Flags().StringVar(&exportProfile, "export-profile", privacy.ProfileDefault, "export profile: default, metadata, full, or a profile under export.profiles")
	return cmd
}

func writeReceiptExportWarnings(w io.Writer, stats receipt.ExportStats) error {
	if stats.ProjectedV1 > 0 {
		if _, err := fmt.Fprintf(w, "warning: the export profile removed the content of %d receipt(s) with hash v1. The v1 hash covers the content, so these receipts do not verify. Export with --export-profile full to verify them.\n", stats.ProjectedV1); err != nil {
			return err
		}
	}
	if stats.MessageQuotes > 0 {
		if _, err := fmt.Fprintf(w, "warning: %d receipt(s) have a rule message that quotes content that the export profile removed. The receipt hash covers the message, so the export keeps it.\n", stats.MessageQuotes); err != nil {
			return err
		}
	}
	return nil
}
