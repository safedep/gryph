// Package cli provides the command-line interface for gryph.
package cli

import (
	"context"
	"os"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/agent/cursor"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// App holds the application dependencies.
type App struct {
	Config    *config.Config
	Store     storage.Store
	Registry  *agent.Registry
	Presenter tui.Presenter
	Paths     *config.Paths
	Security  *security.Evaluator
}

// NewApp creates a new App with the given configuration.
func NewApp(cfg *config.Config) *App {
	paths := config.ResolvePaths()

	// Create registry and register adapters
	registry := agent.NewRegistry()
	claudecode.Register(registry)
	cursor.Register(registry)

	// Create presenter based on config
	presenter := tui.NewPresenter(tui.FormatTable, tui.PresenterOptions{
		Writer:    os.Stdout,
		UseColors: cfg.ShouldUseColors(),
	})

	// Create security evaluator with placeholder check
	sec := security.New(&security.Config{FailOpen: true})
	sec.RegisterCheck(security.NewPlaceholderCheck())

	return &App{
		Config:    cfg,
		Registry:  registry,
		Presenter: presenter,
		Paths:     paths,
		Security:  sec,
	}
}

// InitStore initializes the database store.
func (a *App) InitStore(ctx context.Context) error {
	dbPath := a.Config.GetDatabasePath()
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		return err
	}
	if err := store.Init(ctx); err != nil {
		return err
	}
	a.Store = store
	return nil
}

// Close closes the application resources.
func (a *App) Close() error {
	if a.Store != nil {
		return a.Store.Close()
	}
	return nil
}

// GlobalFlags holds the global command flags.
type GlobalFlags struct {
	ConfigPath string
	Verbose    bool
	Quiet      bool
	NoColor    bool
	Format     string
}

var globalFlags GlobalFlags

// NewRootCmd creates the root command.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "gryph",
		Short: "AI Coding Agent Audit Trail Tool",
		Long: `Gryph is a local-first CLI tool that logs and audits AI agent actions.

It integrates with AI coding agents (Claude Code, Cursor, etc.) via their
native hook systems to create a comprehensive audit trail of all agent actions.`,
		Version: version.Version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Handle NO_COLOR environment variable
			if os.Getenv("NO_COLOR") != "" {
				globalFlags.NoColor = true
			}

			if os.Getenv("GRYPH_NO_COLOR") != "" {
				globalFlags.NoColor = true
			}

			setupInternalLogger()

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&globalFlags.ConfigPath, "config", "c", "", "path to config file")
	rootCmd.PersistentFlags().BoolVarP(&globalFlags.Verbose, "verbose", "v", false, "increase output verbosity")
	rootCmd.PersistentFlags().BoolVarP(&globalFlags.Quiet, "quiet", "q", false, "suppress non-essential output")
	rootCmd.PersistentFlags().BoolVar(&globalFlags.NoColor, "no-color", false, "disable colored output")

	// Add subcommands
	rootCmd.AddCommand(
		NewInstallCmd(),
		NewUninstallCmd(),
		NewStatusCmd(),
		NewDoctorCmd(),
		NewLogsCmd(),
		NewQueryCmd(),
		NewSessionsCmd(),
		NewSessionCmd(),
		NewExportCmd(),
		NewConfigCmd(),
		NewSelfLogCmd(),
		NewDiffCmd(),
		NewHookCmd(),
		NewRetentionCmd(),
	)

	return rootCmd
}

// Execute runs the root command.
func Execute() error {
	return NewRootCmd().Execute()
}

// setupLogger sets up the DRY logger
func setupInternalLogger() {
	// Always skip the stdout logger since we are running in a CLI context.
	// with our own TUI.
	_ = os.Setenv("APP_LOG_SKIP_STDOUT_LOGGER", "true")

	log.Init("gryph", "cli")
}

// loadApp loads the application with configuration.
func loadApp() (*App, error) {
	cfg, err := config.Load(globalFlags.ConfigPath)
	if err != nil {
		// Use defaults if config not found
		cfg = config.Default()
	}

	// Override with flags
	if globalFlags.NoColor {
		cfg.Display.Colors = config.ColorNever
	}

	return NewApp(cfg), nil
}

// getFormat returns the output format from flags or default.
func getFormat(format string) tui.Format {
	switch format {
	case "json":
		return tui.FormatJSON
	case "jsonl":
		return tui.FormatJSONL
	case "csv":
		return tui.FormatCSV
	default:
		return tui.FormatTable
	}
}
