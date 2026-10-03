// Package cli provides the command-line interface for gryph.
package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/hookside"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/remote"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// App holds the runtime and the presenter of one command run.
type App struct {
	*engine.Runtime
	Presenter tui.Presenter
	// Reads is the store of the read commands: the local store, or the
	// partition of this account at the decision service on a managed
	// host. InitReadStore sets it.
	Reads  storage.ReadStore
	remote *ipc.Client
}

// InitReadStore opens the store of the read commands. Under a managed
// configuration with the decision service on, the hooks record nothing in
// the user's database, so the reads go to the service over the socket and
// come back from the partition of this account. The socket passes the same
// identity check as in the hook. Elsewhere it opens the local database.
func (a *App) InitReadStore(ctx context.Context) error {
	if !clientMode(a.Config) {
		if err := a.InitStore(ctx); err != nil {
			return err
		}
		a.Reads = a.Store
		return nil
	}
	socket := a.Config.Supervisor.SocketPath()
	client, err := ipc.Dial(ctx, socket, ipc.DialOptions{Version: version.Version, VerifyServer: func(conn net.Conn) error {
		return hookside.VerifyServer(conn, socket, a.Config.Supervisor.ServerAccount())
	}})
	if err != nil {
		return fmt.Errorf("the decision service at %s: %w", socket, err)
	}
	a.remote = client
	a.Reads = remote.New(client)
	return nil
}

// SetSessionCost stores the cost totals sc that this process read from
// the transcript of sess: on the local store, or at the service as a
// claim of this account. Without new totals there is nothing to send.
func (a *App) SetSessionCost(ctx context.Context, sess *session.Session, sc *cost.SessionCost) error {
	if a.remote != nil {
		if sc == nil {
			return nil
		}
		return a.remote.SessionCost(ctx, ipc.SessionCost{SessionID: sess.ID, Cost: sc})
	}
	return a.Store.UpdateSession(ctx, sess)
}

// Approve sends an answer to an approval request to the decision service.
// Only the service keeps the queue, so there is no local path.
func (a *App) Approve(ctx context.Context, req ipc.Approve) (*ipc.ApproveResult, error) {
	if a.remote == nil {
		return nil, errApprovalNeedsService
	}
	return a.remote.Approve(ctx, req)
}

// Close releases the store and the connection to the service.
func (a *App) Close() error {
	var errs []error
	if a.remote != nil {
		errs = append(errs, a.remote.Close())
		a.remote = nil
	}
	errs = append(errs, a.Runtime.Close())
	return errors.Join(errs...)
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
		newUninstallUserCmd(),
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
