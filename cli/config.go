package cli

import (
	"context"
	"fmt"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// resolveConfigPath returns the config file path for the config commands:
// the active managed file, then the --config flag, then the per-user file.
// This keeps config show and config get on the effective file. The write
// commands refuse before they reach the managed file.
func resolveConfigPath(app *App) string {
	if managed := config.ManagedConfigFile(); managed != "" {
		return managed
	}

	if globalFlags.ConfigPath != "" {
		return globalFlags.ConfigPath
	}

	return app.Paths.ConfigFile
}

// refuseWhenManaged blocks config writes while the system managed config
// file is active. The managed file is authoritative over --config and the
// per-user file, so any other write would silently do nothing.
func refuseWhenManaged() error {
	if managed := config.ManagedConfigFile(); managed != "" {
		return fmt.Errorf("configuration is managed by %s: contact your administrator to change it", managed)
	}

	return nil
}

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or modify configuration",
		Long: `View or modify configuration.

Subcommands allow viewing and modifying configuration values.
Changes are logged to the self-audit trail.`,
	}

	cmd.AddCommand(
		newConfigShowCmd(),
		newConfigGetCmd(),
		newConfigSetCmd(),
		newConfigResetCmd(),
	)

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Display current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}

			app.Presenter = tui.NewPresenter(getFormat(format), tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors(),
			})

			mgr, err := config.NewManager(resolveConfigPath(app))
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			view := &tui.ConfigView{
				Location: mgr.ConfigPath(),
				Values:   mgr.AllSettings(),
			}

			return app.Presenter.RenderConfig(view)
		},
	}

	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")

	return cmd
}

func newConfigGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get specific config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			app, err := loadApp()
			if err != nil {
				return err
			}

			mgr, err := config.NewManager(resolveConfigPath(app))
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if !mgr.HasKey(key) {
				return fmt.Errorf("key not found: %s", key)
			}

			value := mgr.Get(key)
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), value); err != nil {
				return fmt.Errorf("failed to write value: %w", err)
			}

			return nil
		},
	}

	return cmd
}

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			key := args[0]
			value := args[1]

			if err := refuseWhenManaged(); err != nil {
				return err
			}

			app, err := loadApp()
			if err != nil {
				return err
			}

			if err := config.EnsureDirectories(); err == nil {
				if err := app.InitStore(ctx); err == nil {
					defer func() {
						err := app.Close()
						if err != nil {
							log.Errorf("failed to close app: %w", err)
						}
					}()
				}
			}

			mgr, err := config.NewManager(resolveConfigPath(app))
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			oldValue := mgr.Get(key)
			parsedValue := config.ParseValue(value)

			if err := mgr.Set(key, parsedValue); err != nil {
				return err
			}

			if err := logSelfAudit(ctx, app.Store, SelfAuditActionConfigChange, "",
				map[string]interface{}{
					"key":       key,
					"old_value": oldValue,
					"new_value": parsedValue,
				},
				SelfAuditResultSuccess, ""); err != nil {
				return fmt.Errorf("failed to log self-audit: %w", err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %v\n", key, parsedValue); err != nil {
				return fmt.Errorf("failed to write value: %w", err)
			}

			return nil
		},
	}

	return cmd
}

func newConfigResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset to default configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if err := refuseWhenManaged(); err != nil {
				return err
			}

			app, err := loadApp()
			if err != nil {
				return err
			}

			if err := config.EnsureDirectories(); err == nil {
				if err := app.InitStore(ctx); err == nil {
					defer func() {
						err := app.Close()
						if err != nil {
							log.Errorf("failed to close app: %w", err)
						}
					}()
				}
			}

			mgr, err := config.NewManager(resolveConfigPath(app))
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if err := mgr.Reset(); err != nil {
				return err
			}

			if err := logSelfAudit(ctx, app.Store, SelfAuditActionConfigChange, "",
				map[string]interface{}{
					"action": "reset",
				},
				SelfAuditResultSuccess, ""); err != nil {
				return fmt.Errorf("failed to log self-audit: %w", err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Configuration reset to defaults."); err != nil {
				return fmt.Errorf("failed to write value: %w", err)
			}

			return nil
		},
	}

	return cmd
}
