package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/schedule"
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
	cmd.AddCommand(newSupervisorReconcileCmd(), newSupervisorProtocolCmd(), newSupervisorRunCmd(), newSupervisorSendCmd(), newSupervisorFakeCmd())
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

// The timer that runs the reconcile pass.
const (
	repairTimerName     = "gryph-reconcile"
	repairTimerInterval = 15 * time.Minute
)

// repairTimerJob returns the scheduled job for the reconcile pass. It names
// the running binary by its absolute path, so the job does not depend on
// PATH.
func repairTimerJob() (schedule.Job, error) {
	return repairTimerJobFor(utils.GryphCommand())
}

// repairTimerJobFor returns the scheduled job that runs program, the
// absolute path of a gryph binary.
func repairTimerJobFor(program string) (schedule.Job, error) {
	if !filepath.IsAbs(program) {
		return schedule.Job{}, fmt.Errorf("the running program %q has no absolute path", program)
	}
	return schedule.Job{
		Name:        repairTimerName,
		Description: "Gryph reconcile: keep the agent hooks in place",
		Command:     []string{program, "supervisor", "reconcile", "--once"},
		Interval:    repairTimerInterval,
	}, nil
}

// installRepairTimer installs the timer and returns its view. A failure is
// in the view, because the hooks are installed either way.
func installRepairTimer(ctx context.Context) *tui.RepairTimerView {
	job, err := repairTimerJob()
	if err != nil {
		return &tui.RepairTimerView{Error: err.Error()}
	}
	res, err := schedule.Install(ctx, job)
	if err != nil {
		return &tui.RepairTimerView{Error: err.Error()}
	}
	return &tui.RepairTimerView{Paths: res.Paths, Enabled: res.Enabled, Next: res.Next}
}

// installSystemRepairTimer installs the job for every account of the host,
// with the managed binary as the program. A failure is in the view, because
// the managed files are in place either way.
func installSystemRepairTimer(ctx context.Context, program string) *tui.RepairTimerView {
	job, err := repairTimerJobFor(program)
	if err != nil {
		return &tui.RepairTimerView{Error: err.Error()}
	}
	res, err := schedule.InstallSystemWide(ctx, job)
	if err != nil {
		return &tui.RepairTimerView{Error: err.Error()}
	}
	return &tui.RepairTimerView{Paths: res.Paths, Changed: res.Changed, Enabled: res.Enabled, Next: res.Next}
}

// removeSystemRepairTimer removes the system-wide job and returns its view.
func removeSystemRepairTimer(ctx context.Context) *tui.RepairTimerView {
	res, err := schedule.RemoveSystemWide(ctx, repairTimerName)
	if err != nil {
		return &tui.RepairTimerView{Error: err.Error()}
	}
	return &tui.RepairTimerView{Paths: res.Paths, Changed: res.Changed, Enabled: res.Enabled, Next: res.Next}
}

// removeRepairTimer removes the timer and returns its view.
func removeRepairTimer(ctx context.Context) *tui.RepairTimerView {
	res, err := schedule.Remove(ctx, repairTimerName)
	if err != nil {
		return &tui.RepairTimerView{Error: err.Error()}
	}
	return &tui.RepairTimerView{Paths: res.Paths, Enabled: res.Enabled, Next: res.Next}
}
