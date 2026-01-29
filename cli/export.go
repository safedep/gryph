package cli

import (
	"context"
	"os"

	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewExportCmd creates the export command.
func NewExportCmd() *cobra.Command {
	var (
		since  string
		until  string
		agent  string
		format string
		output string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export audit data",
		Long: `Export audit data.

Exports audit events to various formats for external analysis.`,
		Example: `  gryph export --format json -o events.json
  gryph export --since "1w" --format csv
  gryph export --agent claude-code --format jsonl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Determine output destination
			writer := cmd.OutOrStdout()
			if output != "" {
				file, err := os.Create(output)
				if err != nil {
					return err
				}
				defer file.Close()
				writer = file
			}

			// Update presenter format
			app.Presenter = tui.NewPresenter(getFormat(format), tui.PresenterOptions{
				Writer:    writer,
				UseColors: false, // Never use colors for export
			})

			// Initialize store
			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer app.Close()

			// Build filter
			filter := events.NewEventFilter().WithLimit(0) // No limit for export

			if since != "" {
				if sinceTime, err := parseDuration(since); err == nil {
					filter = filter.WithSince(sinceTime)
				}
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

			if len(evts) == 0 {
				return app.Presenter.RenderMessage("No events to export.")
			}

			// Convert to view models
			eventViews := make([]*tui.EventView, len(evts))
			for i, e := range evts {
				eventViews[i] = eventToView(e)
			}

			return app.Presenter.RenderEvents(eventViews)
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "export events since")
	cmd.Flags().StringVar(&until, "until", "", "export events until")
	cmd.Flags().StringVar(&agent, "agent", "", "filter by agent")
	cmd.Flags().StringVar(&format, "format", "jsonl", "output format: json, jsonl, csv")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to file (default: stdout)")

	return cmd
}
