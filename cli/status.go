package cli

import (
	"context"
	"os"
	"time"

	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewStatusCmd creates the status command.
func NewStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show installation status and health",
		Long: `Show installation status and health.

Displays the current status of the tool including:
- Tool version
- Installed agents and their hook status
- Database information
- Configuration settings`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Build status view
			view := &tui.StatusView{
				Version: version.Version,
			}

			// Agent status
			for _, adapter := range app.Registry.All() {
				detection, _ := adapter.Detect(ctx)
				hookStatus, _ := adapter.Status(ctx)

				agentView := tui.AgentStatusView{
					Name:        adapter.Name(),
					DisplayName: adapter.DisplayName(),
				}

				if detection != nil && detection.Installed {
					agentView.Installed = true
					agentView.Version = detection.Version
				}

				if hookStatus != nil {
					agentView.HooksCount = len(hookStatus.Hooks)
					agentView.HooksActive = hookStatus.Installed && hookStatus.Valid
				}

				view.Agents = append(view.Agents, agentView)
			}

			// Database info
			view.Database = tui.DatabaseView{
				Location: app.Paths.DatabaseFile,
			}

			if stat, err := os.Stat(app.Paths.DatabaseFile); err == nil {
				view.Database.SizeBytes = stat.Size()
				view.Database.SizeHuman = tui.FormatBytes(stat.Size())

				// Initialize store to get actual counts
				if err := app.InitStore(ctx); err == nil {
					defer app.Close()
					if stats, err := app.Store.GetSessionStats(ctx); err == nil {
						view.Database.SessionCount = stats.TotalSessions
						view.Database.EventCount = stats.TotalEvents
						view.Database.OldestEvent = stats.OldestSession
						view.Database.NewestEvent = stats.NewestSession
					}
				}
			}

			// Config info
			view.Config = tui.ConfigStatusView{
				Location:      app.Paths.ConfigFile,
				LoggingLevel:  string(app.Config.Logging.Level),
				RetentionDays: app.Config.Storage.RetentionDays,
			}

			// If retention is enabled, count events that would be cleaned up
			if app.Config.Storage.RetentionDays > 0 && app.Store != nil {
				cutoff := time.Now().AddDate(0, 0, -app.Config.Storage.RetentionDays)
				view.Config.RetentionCutoff = cutoff
				if count, err := app.Store.CountEventsBefore(ctx, cutoff); err == nil {
					view.Config.EventsToClean = count
				}
			}

			return app.Presenter.RenderStatus(view)
		},
	}

	return cmd
}
