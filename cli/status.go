package cli

import (
	"context"
	"os"
	"time"

	"github.com/safedep/dry/log"
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

			view, err := tui.RunWithSpinner("Checking installation status...", func() (*tui.StatusView, error) {
				v := &tui.StatusView{
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

					v.Agents = append(v.Agents, agentView)
				}

				// Database info
				v.Database = tui.DatabaseView{
					Location: app.Paths.DatabaseFile,
				}

				if stat, err := os.Stat(app.Paths.DatabaseFile); err == nil {
					v.Database.SizeBytes = stat.Size()
					v.Database.SizeHuman = tui.FormatBytes(stat.Size())

					// Initialize store to get actual counts
					if err := app.InitStore(ctx); err == nil {
						if stats, err := app.Store.GetSessionStats(ctx); err == nil {
							v.Database.SessionCount = stats.TotalSessions
							v.Database.EventCount = stats.TotalEvents
							v.Database.OldestEvent = stats.OldestSession
							v.Database.NewestEvent = stats.NewestSession
						}
					}
				}

				// Config info
				v.Config = tui.ConfigStatusView{
					Location:      app.Paths.ConfigFile,
					LoggingLevel:  string(app.Config.Logging.Level),
					RetentionDays: app.Config.Storage.RetentionDays,
				}

				// If retention is enabled, count events that would be cleaned up
				if app.Config.Storage.RetentionDays > 0 && app.Store != nil {
					cutoff := time.Now().AddDate(0, 0, -app.Config.Storage.RetentionDays)
					v.Config.RetentionCutoff = cutoff
					if count, err := app.Store.CountEventsBefore(ctx, cutoff); err == nil {
						v.Config.EventsToClean = count
					}
				}

				return v, nil
			})
			if err != nil {
				return err
			}

			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %w", err)
				}
			}()

			return app.Presenter.RenderStatus(view)
		},
	}

	return cmd
}
