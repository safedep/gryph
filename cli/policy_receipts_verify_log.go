package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/spf13/cobra"
)

func newPolicyReceiptsVerifyLogCmd() *cobra.Command {
	var (
		input      string
		trustStore string
		format     string
	)
	cmd := &cobra.Command{
		Use:   "verify-log",
		Short: "Verify a JSONL receipt export without DB access",
		Long: "Reads a JSONL receipt export produced by `gryph policy " +
			"receipts export --format jsonl` and verifies it stand-alone. " +
			"The exported chain plus the trust store is enough to verify the " +
			"hash chain across all rows, that the first row's prev_hash is " +
			"the zero state, and that each signed row has a valid signature " +
			"under a trusted pubkey. Use `--input -` to read from stdin.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if input == "" {
				return ErrConfig("--input is required", fmt.Errorf("missing input file"))
			}

			var r io.Reader
			if input == "-" {
				r = cmd.InOrStdin()
			} else {
				f, err := os.Open(input)
				if err != nil {
					return ErrConfig("open input file", err)
				}
				defer func() {
					if cerr := f.Close(); cerr != nil {
						log.Errorf("failed to close input: %v", cerr)
					}
				}()
				r = f
			}

			app, err := loadApp()
			if err != nil {
				log.Warnf("loadApp failed during verify-log, using defaults: %v", err)
			}

			path := trustStore
			if path == "" && app != nil {
				path = app.Config.ResolveReceiptTrustStorePath(app.Paths)
			}
			var verifier *receipt.Ed25519Verifier
			if path != "" {
				ts, err := receipt.LoadTrustStore(path)
				if err != nil {
					return ErrConfig("load trust store", err)
				}
				v, err := receipt.NewEd25519Verifier(ts)
				if err != nil {
					return ErrConfig("build verifier", err)
				}
				verifier = v
			}

			res, err := receipt.VerifyExportedLog(r, verifier)
			if err != nil {
				return ErrConfig("verify log", err)
			}

			out := cmd.OutOrStdout()
			if strings.ToLower(format) == "json" {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(res); encErr != nil {
					return encErr
				}
			} else {
				renderLogVerifyResult(out, res)
			}

			if app != nil && app.Store != nil {
				_ = app.Close()
			}
			if !res.OK {
				return ErrConfig("receipt log verification failed", fmt.Errorf("%d chain break(s), %d invalid signature(s)", res.ChainBreaks, res.SignedInvalid))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "JSONL file (or - for stdin)")
	cmd.Flags().StringVar(&trustStore, "trust-store", "", "trust store JSON path (default: configured path)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text, json")
	return cmd
}

func renderLogVerifyResult(w io.Writer, res *receipt.LogVerifyResult) {
	_, _ = fmt.Fprintf(w, "Receipt log verification:\n")
	_, _ = fmt.Fprintf(w, "  total             %d\n", res.Total)
	_, _ = fmt.Fprintf(w, "  signed_ok         %d\n", res.SignedOK)
	_, _ = fmt.Fprintf(w, "  unsigned          %d\n", res.Unsigned)
	_, _ = fmt.Fprintf(w, "  signed_invalid    %d\n", res.SignedInvalid)
	_, _ = fmt.Fprintf(w, "  signed_unverified %d\n", res.SignedUnverified)
	_, _ = fmt.Fprintf(w, "  chain_breaks      %d\n", res.ChainBreaks)
	if len(res.Warnings) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Warnings:")
		for _, msg := range res.Warnings {
			_, _ = fmt.Fprintf(w, "  %s\n", msg)
		}
	}
	if len(res.Errors) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Errors:")
		for _, e := range res.Errors {
			_, _ = fmt.Fprintf(w, "  session=%s seq=%d %s\n", e.SessionID, e.Sequence, e.Reason)
		}
	}
}
