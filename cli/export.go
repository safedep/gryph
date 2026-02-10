package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
	"github.com/spf13/cobra"
)

// NewExportCmd creates the export command.
func NewExportCmd() *cobra.Command {
	var (
		since     string
		until     string
		agent     string
		output    string
		sensitive bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export raw events as schema-verifiable JSONL",
		Long: `Export raw events as schema-verifiable JSONL.

Exports audit events as raw JSON Lines for external analysis, auditing, and
pipeline consumption. Each line is a complete Event object with a $schema field
for validation. Sensitive events are excluded by default.`,
		Example: `  gryph export                                    # last 1h, non-sensitive, JSONL to stdout
  gryph export --since "1w" -o audit.jsonl        # last week to file
  gryph export --agent claude-code --sensitive     # include sensitive events`,
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

			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %w", err)
				}
			}()

			// Build filter
			filter := events.NewEventFilter().WithLimit(0)

			if since == "" {
				since = "1h"
			}

			if sinceTime, err := parseDuration(since); err == nil {
				filter = filter.WithSince(sinceTime)
			}

			if until != "" {
				if untilTime, err := parseDuration(until); err == nil {
					filter = filter.WithUntil(untilTime)
				}
			}

			if agent != "" {
				filter = filter.WithAgents(agent)
			}

			// Query events
			evts, err := app.Store.QueryEvents(ctx, filter)
			if err != nil {
				return err
			}

			// Filter sensitive events unless --sensitive is set
			if !sensitive {
				filtered := make([]*events.Event, 0, len(evts))
				for _, e := range evts {
					if !e.IsSensitive {
						filtered = append(filtered, e)
					}
				}
				evts = filtered
			}

			if len(evts) == 0 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No events to export.")
				return nil
			}

			// Determine output destination
			writer := cmd.OutOrStdout()
			if output != "" {
				file, err := os.Create(output)
				if err != nil {
					return err
				}

				defer func() {
					err := file.Close()
					if err != nil {
						log.Errorf("failed to close file: %w", err)
					}
				}()

				writer = file
			}

			// Encode events as JSONL directly
			enc := json.NewEncoder(writer)
			for _, e := range evts {
				if err := enc.Encode(e); err != nil {
					return err
				}
			}

			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Exported %d events\n", len(evts))
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "export events since (default: 1h)")
	cmd.Flags().StringVar(&until, "until", "", "export events until")
	cmd.Flags().StringVar(&agent, "agent", "", "filter by agent")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to file (default: stdout)")
	cmd.Flags().BoolVar(&sensitive, "sensitive", false, "include sensitive events")

	return cmd
}
