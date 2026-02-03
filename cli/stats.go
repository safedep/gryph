package cli

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/tui/component/stats"
	"github.com/spf13/cobra"
)

type statsParams struct {
	since string
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

			timeRange := parseTimeRange(p.since)

			opts := stats.Options{
				Store:       app.Store,
				TimeRange:   timeRange,
				AgentFilter: p.agent,
			}

			prog := tea.NewProgram(stats.New(opts), tea.WithAltScreen())
			_, err = prog.Run()

			return err
		},
	}

	cmd.Flags().StringVar(&p.since, "since", "today", "time range: today, 7d, 30d, all")
	cmd.Flags().StringVar(&p.agent, "agent", "", "filter by agent name")

	return cmd
}

func parseTimeRange(s string) stats.TimeRange {
	switch s {
	case "7d", "week":
		return stats.Range7Days
	case "30d", "month":
		return stats.Range30Days
	case "all":
		return stats.RangeAll
	default:
		return stats.RangeToday
	}
}
