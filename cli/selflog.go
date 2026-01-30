package cli

import (
	"context"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewSelfLogCmd creates the self-log command.
func NewSelfLogCmd() *cobra.Command {
	var (
		since  string
		limit  int
		format string
	)

	cmd := &cobra.Command{
		Use:   "self-log",
		Short: "View the tool's own audit trail",
		Long: `View the tool's own audit trail.

Shows logged actions performed by gryph itself, such as
installation, uninstallation, and configuration changes.`,
		Example: `  gryph self-log
  gryph self-log --limit 10
  gryph self-log --since "1w"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Update presenter format
			app.Presenter = tui.NewPresenter(getFormat(format), tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors(),
			})

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
			filter := &storage.SelfAuditFilter{
				Limit: limit,
			}

			if since != "" {
				if sinceTime, err := parseDuration(since); err == nil {
					filter.Since = &sinceTime
				}
			}

			// Query self-audit entries
			entries, err := app.Store.QuerySelfAudits(ctx, filter)
			if err != nil {
				return err
			}

			if len(entries) == 0 {
				return app.Presenter.RenderMessage("No self-audit entries found.")
			}

			// Convert to view models
			views := make([]*tui.SelfAuditView, len(entries))
			for i, e := range entries {
				views[i] = &tui.SelfAuditView{
					ID:           e.ID.String(),
					Timestamp:    e.Timestamp,
					Action:       e.Action,
					AgentName:    e.AgentName,
					Result:       e.Result,
					ErrorMessage: e.ErrorMessage,
					ToolVersion:  e.ToolVersion,
					Details:      e.Details,
				}
			}

			return app.Presenter.RenderSelfAudits(views)
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "filter by time")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum entries")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")

	return cmd
}
