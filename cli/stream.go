package cli

import (
	"fmt"
	"os"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/stream"
	"github.com/safedep/gryph/stream/nop"
	"github.com/safedep/gryph/stream/stdout"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewStreamCmd creates the hidden stream parent command.
func NewStreamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "stream",
		Short:  "Stream event management",
		Hidden: true,
	}

	cmd.AddCommand(newStreamSyncCmd())
	return cmd
}

func newStreamSyncCmd() *cobra.Command {
	var (
		quiet      bool
		format     string
		batchSize  int
		iterations int
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync events to configured stream targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp()
			if err != nil {
				return err
			}

			defer func() {
				if err := app.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "Error closing app: %v\n", err)
				}
			}()

			app.Presenter = tui.NewPresenter(getFormat(format), tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors(),
			})

			ctx := cmd.Context()
			if err := app.InitStore(ctx); err != nil {
				return err
			}

			registry := buildStreamRegistry(app.Config)
			if len(registry.Enabled()) == 0 {
				if !quiet {
					return app.Presenter.RenderMessage("No enabled stream targets configured.")
				}
				return nil
			}

			var progress *tui.ProgressWriter
			if !quiet && tui.IsTerminal() {
				progress = tui.NewProgressWriter(os.Stderr, app.Config.ShouldUseColors())
			}

			syncOpts := []stream.SyncOption{
				stream.WithProgressCallback(func(p stream.SyncProgress) {
					if progress != nil {
						progress.Update("Syncing %s: %d events, %d audits...",
							p.TargetName, p.EventsSent, p.AuditsSent)
					}
				}),
			}
			if batchSize > 0 {
				syncOpts = append(syncOpts, stream.WithBatchSize(batchSize))
			}
			if iterations > 0 {
				syncOpts = append(syncOpts, stream.WithIterations(iterations))
			}

			syncer := stream.NewSyncer(app.Store, registry)
			result, err := syncer.Sync(ctx, syncOpts...)
			if err != nil {
				return err
			}

			if progress != nil {
				progress.Clear()
			}

			if quiet {
				for _, tr := range result.TargetResults {
					if tr.Error != nil {
						fmt.Fprintf(os.Stderr, "[%s] error: %v\n", tr.TargetName, tr.Error)
					}
				}
				return nil
			}

			return app.Presenter.RenderStreamSync(buildStreamSyncView(result))
		},
	}

	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json, jsonl, csv")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress output except errors")
	cmd.Flags().IntVar(&batchSize, "batch-size", 0, "number of items per batch (0 = use default 500)")
	cmd.Flags().IntVar(&iterations, "iterations", 0, "max batch iterations (0 = unlimited, drain all)")
	return cmd
}

func buildStreamSyncView(result *stream.SyncResult) *tui.StreamSyncView {
	view := &tui.StreamSyncView{
		TargetResults: make([]tui.StreamTargetResultView, 0, len(result.TargetResults)),
	}

	for _, tr := range result.TargetResults {
		trView := tui.StreamTargetResultView{
			TargetName: tr.TargetName,
			EventsSent: tr.EventsSent,
			AuditsSent: tr.AuditsSent,
		}
		if tr.Error != nil {
			trView.Error = tr.Error.Error()
			view.HasErrors = true
		}
		view.TotalEvents += tr.EventsSent
		view.TotalAudits += tr.AuditsSent
		view.TargetResults = append(view.TargetResults, trView)
	}

	return view
}

func buildStreamRegistry(cfg *config.Config) *stream.Registry {
	registry := stream.NewRegistry()
	for _, tc := range cfg.Streams.Targets {
		switch tc.Type {
		case stream.TargetTypeStdout:
			registry.Register(stdout.New(tc.Name, tc.Enabled))
		case stream.TargetTypeNop:
			registry.Register(nop.New(tc.Name, tc.Enabled))
		}
	}

	return registry
}
