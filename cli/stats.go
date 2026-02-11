package cli

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/tui/component/stats"
	"github.com/spf13/cobra"
)

type statsParams struct {
	since string
	until string
	agent string
}

func NewStatsCmd() *cobra.Command {
	var p statsParams

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Interactive statistics dashboard",
		Long: `Launch a fullscreen TUI dashboard showing activity statistics.

Displays overview metrics, activity breakdown, agent stats, timeline,
code changes, command results, error rates, and session info.`,
		Example: `  gryph stats
  gryph stats --since 7d
  gryph stats --since 2w --until 1w
  gryph stats --since 30d --agent claude-code`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}

			defer func() {
				if err := app.Close(); err != nil {
					log.Errorf("failed to close app: %w", err)
				}
			}()

			opts := stats.Options{
				Store:       app.Store,
				AgentFilter: p.agent,
			}

			switch p.since {
			case "today", "":
				opts.TimeRange = stats.RangeToday
			case "7d", "week":
				opts.TimeRange = stats.Range7Days
			case "30d", "month":
				opts.TimeRange = stats.Range30Days
			case "all":
				opts.TimeRange = stats.RangeAll
			default:
				t, err := parseDuration(p.since)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", p.since, err)
				}
				opts.Since = &t
			}

			if p.until != "" {
				t, err := parseDuration(p.until)
				if err != nil {
					return fmt.Errorf("invalid --until value %q: %w", p.until, err)
				}
				opts.Until = &t
			}

			prog := tea.NewProgram(stats.New(opts), tea.WithAltScreen())
			_, err = prog.Run()

			return err
		},
	}

	cmd.Flags().StringVar(&p.since, "since", "today", "time range: today, 7d, 30d, all, or duration (30m, 1h, 2w)")
	cmd.Flags().StringVar(&p.until, "until", "", "end of time window (same syntax as --since)")
	cmd.Flags().StringVar(&p.agent, "agent", "", "filter by agent name")

	return cmd
}
