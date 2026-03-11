package cli

import (
	"context"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewSessionsCmd creates the sessions command.
func NewSessionsCmd() *cobra.Command {
	var (
		agent  string
		since  string
		limit  int
		format string
	)

	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List recorded sessions",
		Long: `List recorded sessions.

Shows all recorded agent sessions with summary statistics.`,
		Example: `  gryph sessions
  gryph sessions --agent claude-code
  gryph sessions --since "1w"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Update presenter format
			app.Presenter = tui.NewPresenter(getFormat(format), tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors(),
			})

			// Initialize store
			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}

			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %w", err)
				}
			}()

			// Build filter
			filter := session.NewSessionFilter().WithLimit(limit)

			if agent != "" {
				filter = filter.WithAgent(agent)
			}

			if since != "" {
				if sinceTime, err := parseDuration(since); err == nil {
					filter = filter.WithSince(sinceTime)
				}
			}

			// Query sessions
			sessions, err := app.Store.QuerySessions(ctx, filter)
			if err != nil {
				return err
			}

			if len(sessions) == 0 {
				return app.Presenter.RenderMessage("No sessions found.")
			}

			// Convert to view models
			sessionViews := make([]*tui.SessionView, len(sessions))
			for i, s := range sessions {
				sessionViews[i] = sessionToView(s)
			}

			return app.Presenter.RenderSessions(sessionViews)
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", "filter by agent")
	cmd.Flags().StringVar(&since, "since", "", "filter by start time")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum sessions")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")

	return cmd
}

// sessionToView converts a session to a view model.
func sessionToView(s *session.Session) *tui.SessionView {
	view := &tui.SessionView{
		ID:               s.ID.String(),
		ShortID:          tui.FormatShortID(s.ID.String()),
		AgentName:        s.AgentName,
		AgentDisplayName: getAgentDisplayName(s.AgentName),
		AgentVersion:     s.AgentVersion,
		StartedAt:        s.StartedAt,
		EndedAt:          s.EndedAt,
		Duration:         s.Duration(),
		WorkingDirectory: s.WorkingDirectory,
		ProjectName:      s.ProjectName,
		TotalActions:     s.TotalActions,
		FilesRead:        s.FilesRead,
		FilesWritten:     s.FilesWritten,
		CommandsExecuted: s.CommandsExecuted,
		Errors:           s.Errors,
		InputTokens:      s.InputTokens,
		OutputTokens:     s.OutputTokens,
		CacheReadTokens:  s.CacheReadTokens,
		CacheWriteTokens: s.CacheWriteTokens,
		EstimatedCostUSD: s.EstimatedCostUSD,
		CostSource:       s.CostSource,
		CostComputedAt:   s.CostComputedAt,
	}

	if s.ModelUsage != nil {
		for _, mu := range s.ModelUsage {
			view.ModelUsage = append(view.ModelUsage, tui.ModelUsageView{
				Model:            mu.Model,
				InputTokens:      mu.InputTokens,
				OutputTokens:     mu.OutputTokens,
				CacheReadTokens:  mu.CacheReadTokens,
				CacheWriteTokens: mu.CacheWriteTokens,
			})
		}
	}

	return view
}

// getAgentDisplayName returns the display name for an agent.
func getAgentDisplayName(name string) string {
	return agent.AgentDisplayName(name)
}
