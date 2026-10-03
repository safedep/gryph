package cli

import (
	"context"
	"fmt"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewSupervisorCmd creates the supervisor command group. It holds the tasks
// that keep Gryph in place on the host.
func NewSupervisorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "supervisor",
		Short: "Keep Gryph in place on this host",
	}
	cmd.AddCommand(newSupervisorReconcileCmd())
	return cmd
}

func newSupervisorReconcileCmd() *cobra.Command {
	var (
		once   bool
		repair bool
		format string
	)

	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Assess the Gryph assets, record changes, and repair hook configurations",
		Long: `Assess the Gryph assets, record changes, and repair hook configurations.

One pass assesses every asset, records each change since the last pass as
a tamper event in the system session, and prints the self-protection
table. When policy.self_protection.repair is on, or with --repair, the
pass also rewrites a hook configuration that differs from a current
install. It touches only the Gryph entries, refuses a symbolic link in the
path, and leaves a file that does not parse. After three repairs of one
asset within an hour it stops repairing that asset and records a tamper
event.

A timer runs this command. See gryph install --repair-timer.`,
		Example: `  gryph supervisor reconcile --once
  gryph supervisor reconcile --once --repair
  gryph supervisor reconcile --once --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !once {
				return ErrConfig("invalid flags", fmt.Errorf("reconcile runs one pass. Pass --once"))
			}
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}
			app.Presenter = tui.NewPresenter(getFormat(format), tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors(),
			})

			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}
			defer func() {
				if err := app.Close(); err != nil {
					log.Errorf("failed to close app: %v", err)
				}
			}()

			report, err := app.Reconcile(ctx, repair || engine.RepairEnabled(app.Config))
			if report != nil {
				view := protectionView(report)
				if renderErr := app.Presenter.RenderProtection(&view); renderErr != nil {
					return renderErr
				}
			}
			if err != nil {
				return err
			}
			if n := len(report.Failed) + len(report.RateLimited); n > 0 {
				return &exitError{code: 1, message: fmt.Sprintf("reconcile: %d hook configuration(s) need attention", n)}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&once, "once", false, "run one pass and exit")
	cmd.Flags().BoolVar(&repair, "repair", false, "repair hook configurations in this pass, also when policy.self_protection.repair is off")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json, jsonl, csv")
	return cmd
}
