package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// NewConfigCmd creates the config command.
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

			// Update presenter format
			app.Presenter = tui.NewPresenter(getFormat(format), tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors(),
			})

			// Convert config to map for display
			v := viper.New()
			v.SetConfigFile(app.Paths.ConfigFile)
			if err := v.ReadInConfig(); err != nil {
				// Use defaults if config not found
				v.Set("logging.level", app.Config.Logging.Level)
				v.Set("logging.stdout_max_chars", app.Config.Logging.StdoutMaxChars)
				v.Set("logging.stderr_max_chars", app.Config.Logging.StderrMaxChars)
				v.Set("logging.context_max_chars", app.Config.Logging.ContextMaxChars)
				v.Set("storage.retention_days", app.Config.Storage.RetentionDays)
				v.Set("privacy.hash_file_contents", app.Config.Privacy.HashFileContents)
				v.Set("display.colors", app.Config.Display.Colors)
				v.Set("display.timezone", app.Config.Display.Timezone)
			}

			view := &tui.ConfigView{
				Location: app.Paths.ConfigFile,
				Values:   v.AllSettings(),
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

			// Use viper to get the value
			v := viper.New()
			v.SetConfigFile(app.Paths.ConfigFile)
			if err := v.ReadInConfig(); err != nil {
				// Use defaults if config not found
				v.Set("logging.level", app.Config.Logging.Level)
				v.Set("logging.stdout_max_chars", app.Config.Logging.StdoutMaxChars)
				v.Set("logging.stderr_max_chars", app.Config.Logging.StderrMaxChars)
				v.Set("logging.context_max_chars", app.Config.Logging.ContextMaxChars)
				v.Set("storage.retention_days", app.Config.Storage.RetentionDays)
				v.Set("privacy.hash_file_contents", app.Config.Privacy.HashFileContents)
				v.Set("display.colors", app.Config.Display.Colors)
				v.Set("display.timezone", app.Config.Display.Timezone)
			}

			value := v.Get(key)
			if value == nil {
				return fmt.Errorf("key not found: %s", key)
			}

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

			// Ensure config directory exists
			if err := os.MkdirAll(app.Paths.ConfigDir, 0700); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}

			// Initialize store for audit logging
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

			// Load or create config
			v := viper.New()
			v.SetConfigFile(app.Paths.ConfigFile)
			v.SetConfigType("yaml")

			// Ignore error if config file doesn't exist
			if err := v.ReadInConfig(); err != nil {
				log.Warnf("failed to read config: %w", err)
			}

			// Get old value for audit
			oldValue := v.Get(key)

			// Parse value type
			var parsedValue interface{} = value
			if value == "true" {
				parsedValue = true
			} else if value == "false" {
				parsedValue = false
			} else if strings.HasPrefix(value, "[") {
				// Simple array parsing
				inner := strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
				parts := strings.Split(inner, ",")
				for i, p := range parts {
					parts[i] = strings.TrimSpace(p)
				}
				parsedValue = parts
			}

			// Set the value
			v.Set(key, parsedValue)

			// Write config
			configMap := v.AllSettings()
			data, err := yaml.Marshal(configMap)
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}

			if err := os.WriteFile(app.Paths.ConfigFile, data, 0600); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			// Log self-audit for config change
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

			// Initialize store for audit logging
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

			// Remove existing config file
			if err := os.Remove(app.Paths.ConfigFile); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove config: %w", err)
			}

			// Log self-audit for config reset
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
