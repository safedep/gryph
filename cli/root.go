// Package cli provides the command-line interface for gryph.
package cli

import (
	"os"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// App holds the runtime and the presenter of one command run.
type App struct {
	*engine.Runtime
	Presenter tui.Presenter
}

// NewApp creates a new App with the given configuration.
func NewApp(cfg *config.Config) (*App, error) {
	rt, err := engine.New(cfg)
	if err != nil {
		return nil, err
	}
	presenter := tui.NewPresenter(tui.FormatTable, tui.PresenterOptions{
		Writer:    os.Stdout,
		UseColors: cfg.ShouldUseColors(),
	})
	return &App{Runtime: rt, Presenter: presenter}, nil
}

// RegisterCheckFactory registers a factory that produces a security.Check.
// External binaries that import gryph as a library call it from init() to
// add checks. See engine.RegisterCheckFactory.
func RegisterCheckFactory(f func(cfg *config.Config) security.Check) {
	engine.RegisterCheckFactory(f)
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

It integrates with AI coding agents (Claude Code, Cursor, Gemini CLI, etc.) via their
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
		NewPolicyCmd(),
		NewSelfLogCmd(),
		NewDiffCmd(),
		NewCatCmd(),
		NewHookCmd(),
		NewRetentionCmd(),
		NewStreamCmd(),
		NewStatsCmd(),
		NewCostCmd(),
		NewVersionCmd(),
		NewAarmCmd(),
		NewSupervisorCmd(),
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
		log.Warnf("config: %v. Gryph uses the default config.", err)
		cfg = config.Default()
	}

	if globalFlags.NoColor {
		cfg.Display.Colors = config.ColorNever
	}

	return NewApp(cfg)
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
