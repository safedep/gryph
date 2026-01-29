package cli

import (
	"fmt"
	"os"
	"strings"

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

			fmt.Fprintln(cmd.OutOrStdout(), value)
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

			// Load or create config
			v := viper.New()
			v.SetConfigFile(app.Paths.ConfigFile)
			v.SetConfigType("yaml")
			v.ReadInConfig() // Ignore error if file doesn't exist

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

			fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %v\n", key, parsedValue)
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
			app, err := loadApp()
			if err != nil {
				return err
			}

			// Remove existing config file
			if err := os.Remove(app.Paths.ConfigFile); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove config: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Configuration reset to defaults.")
			return nil
		},
	}

	return cmd
}
