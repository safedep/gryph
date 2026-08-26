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
			"with the export.",
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

			opts := receipt.ExportOptions{
				Format:            format,
				IncludeSignatures: includeSignatures,
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

			exporter := receipt.NewSQLiteExporter(app.Store)
			return exporter.Export(ctx, w, opts)
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (UUID or prefix) to filter on")
	cmd.Flags().DurationVar(&since, "since", 0, "include receipts newer than this offset from now (e.g. 24h)")
	cmd.Flags().DurationVar(&until, "until", 0, "include receipts older than this offset from now")
	cmd.Flags().StringVar(&format, "format", receipt.ExportFormatJSONL, "output format: jsonl, csv")
	cmd.Flags().StringVar(&output, "output", "", "output file (default stdout)")
	cmd.Flags().BoolVar(&includeSignatures, "include-signatures", false, "include signature and signer_key_id columns")
	return cmd
}
