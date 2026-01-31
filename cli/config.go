package cli

import (
	"context"
	"fmt"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

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

			mgr, err := config.NewManager(app.Paths.ConfigFile)
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

			mgr, err := config.NewManager(app.Paths.ConfigFile)
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

			mgr, err := config.NewManager(app.Paths.ConfigFile)
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

			mgr, err := config.NewManager(app.Paths.ConfigFile)
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
