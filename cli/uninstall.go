package cli

import (
	"context"
	"os"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewUninstallCmd creates the uninstall command.
func NewUninstallCmd() *cobra.Command {
	var (
		agents         []string
		purge          bool
		dryRun         bool
		restoreBackup  bool
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove hooks from AI coding agents",
		Long: `Remove hooks from AI coding agents.

Removes installed hooks from all or specified agents. Optionally
removes the database and configuration files as well.`,
		Example: `  gryph uninstall
  gryph uninstall --agent claude-code
  gryph uninstall --purge
  gryph uninstall --restore-backup`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Initialize store for audit logging (unless purging or dry-run)
			if !dryRun && !purge {
				if err := config.EnsureDirectories(); err == nil {
					if err := app.InitStore(ctx); err == nil {
						defer app.Close()
					}
				}
			}

			// Filter agents if specified
			adapters := app.Registry.All()
			if len(agents) > 0 {
				adapters = filterAdapters(adapters, agents)
				if len(adapters) == 0 {
					return ErrAgentNotFound(agents[0])
				}
			}

			// Build uninstall view
			view := &tui.UninstallView{
				Purged: purge,
			}

			for _, adapter := range adapters {
				opts := agent.UninstallOptions{
					DryRun:        dryRun,
					RestoreBackup: restoreBackup,
					BackupDir:     app.Paths.BackupsDir,
				}

				result, err := adapter.Uninstall(ctx, opts)
				if err != nil {
					view.Agents = append(view.Agents, tui.AgentUninstallView{
						Name:        adapter.Name(),
						DisplayName: adapter.DisplayName(),
						Error:       err.Error(),
					})
					// Log self-audit for failed uninstall
					if !dryRun {
						logSelfAudit(ctx, app.Store, SelfAuditActionUninstall, adapter.Name(),
							map[string]interface{}{"error": err.Error()},
							SelfAuditResultError, err.Error())
					}
					continue
				}

				view.Agents = append(view.Agents, tui.AgentUninstallView{
					Name:            adapter.Name(),
					DisplayName:     adapter.DisplayName(),
					HooksRemoved:    result.HooksRemoved,
					BackupsRestored: result.BackupsRestored,
				})

				// Log self-audit for successful uninstall
				if !dryRun && len(result.HooksRemoved) > 0 {
					logSelfAudit(ctx, app.Store, SelfAuditActionUninstall, adapter.Name(),
						map[string]interface{}{
							"hooks_removed":     result.HooksRemoved,
							"backups_restored":  result.BackupsRestored,
							"restore_backup":    restoreBackup,
						},
						SelfAuditResultSuccess, "")
				}
			}

			// Purge database and config if requested
			if purge && !dryRun {
				// Log purge before removing files
				logSelfAudit(ctx, app.Store, SelfAuditActionPurge, "",
					map[string]interface{}{
						"database_removed": app.Paths.DatabaseFile,
						"config_removed":   app.Paths.ConfigFile,
					},
					SelfAuditResultSuccess, "")

				os.Remove(app.Paths.DatabaseFile)
				os.Remove(app.Paths.ConfigFile)
			}

			return app.Presenter.RenderUninstall(view)
		},
	}

	cmd.Flags().StringArrayVar(&agents, "agent", nil, "uninstall from specific agent only (repeatable)")
	cmd.Flags().BoolVar(&purge, "purge", false, "also remove database and configuration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed")
	cmd.Flags().BoolVar(&restoreBackup, "restore-backup", false, "restore backed-up hooks if available")

	return cmd
}
