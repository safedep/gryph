package cli

import (
	"context"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewInstallCmd creates the install command.
func NewInstallCmd() *cobra.Command {
	var (
		agents   []string
		dryRun   bool
		force    bool
		noBackup bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install hooks for AI coding agents",
		Long: `Install hooks for AI coding agents.

Discovers all supported agents on the system and installs hooks
to enable audit logging. Existing hooks are backed up by default.`,
		Example: `  gryph install
  gryph install --agent claude-code
  gryph install --dry-run
  gryph install --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Ensure directories exist
			if !dryRun {
				if err := config.EnsureDirectories(); err != nil {
					return err
				}

				// Initialize store
				if err := app.InitStore(ctx); err != nil {
					return ErrDatabase("failed to initialize database", err)
				}
				defer app.Close()
			}

			// Filter agents if specified
			adapters := app.Registry.All()
			if len(agents) > 0 {
				adapters = filterAdapters(adapters, agents)
				if len(adapters) == 0 {
					return ErrAgentNotFound(agents[0])
				}
			}

			// Build install view
			view := &tui.InstallView{
				Database: app.Paths.DatabaseFile,
				Config:   app.Paths.ConfigFile,
			}

			for _, adapter := range adapters {
				detection, err := adapter.Detect(ctx)
				if err != nil {
					view.Agents = append(view.Agents, tui.AgentInstallView{
						Name:        adapter.Name(),
						DisplayName: adapter.DisplayName(),
						Error:       err.Error(),
					})
					continue
				}

				agentView := tui.AgentInstallView{
					Name:        adapter.Name(),
					DisplayName: adapter.DisplayName(),
					Installed:   detection.Installed,
					Version:     detection.Version,
					Path:        detection.Path,
				}

				if detection.Installed {
					opts := agent.InstallOptions{
						DryRun:    dryRun,
						Force:     force,
						Backup:    !noBackup,
						BackupDir: app.Paths.BackupsDir,
					}

					result, err := adapter.Install(ctx, opts)
					if err != nil {
						agentView.Error = err.Error()
						// Log self-audit for failed install
						if !dryRun {
							logSelfAudit(ctx, app.Store, SelfAuditActionInstall, adapter.Name(),
								map[string]interface{}{"error": err.Error()},
								SelfAuditResultError, err.Error())
						}
					} else {
						agentView.HooksInstalled = result.HooksInstalled
						agentView.Warnings = result.Warnings
						// Log self-audit for successful install
						if !dryRun && len(result.HooksInstalled) > 0 {
							logSelfAudit(ctx, app.Store, SelfAuditActionInstall, adapter.Name(),
								map[string]interface{}{
									"hooks_installed": result.HooksInstalled,
									"warnings":        result.Warnings,
								},
								SelfAuditResultSuccess, "")
						}
					}
				}

				view.Agents = append(view.Agents, agentView)
			}

			return app.Presenter.RenderInstall(view)
		},
	}

	cmd.Flags().StringArrayVar(&agents, "agent", nil, "install for specific agent only (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be installed")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing hooks without prompting")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "skip backup of existing hooks")

	return cmd
}

func filterAdapters(adapters []agent.Adapter, names []string) []agent.Adapter {
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	filtered := make([]agent.Adapter, 0)
	for _, adapter := range adapters {
		if nameSet[adapter.Name()] {
			filtered = append(filtered, adapter)
		}
	}
	return filtered
}
