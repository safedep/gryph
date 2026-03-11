package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewCostCmd creates the cost command.
func NewCostCmd() *cobra.Command {
	var (
		since     string
		until     string
		today     bool
		yesterday bool
		agent     string
		model     string
		sessionID string
		format    string
		by        string
		sync      bool
		force     bool
		limit     int
	)

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Show token usage and estimated costs",
		Long: `Show token usage and estimated costs for agent sessions.

Displays cost data aggregated by session, model, agent, or day.
Use --sync to collect cost data from agent transcripts before displaying.`,
		Example: `  gryph cost
  gryph cost --today
  gryph cost --since "1w" --by model
  gryph cost --agent claude-code --by day
  gryph cost --sync --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
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
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %w", err)
				}
			}()

			filter := session.NewSessionFilter().WithLimit(limit)

			if today {
				tf := events.Today()
				filter = filter.WithSince(*tf.Since)
			} else if yesterday {
				tf := events.Yesterday()
				filter = filter.WithSince(*tf.Since).WithUntil(*tf.Until)
			} else if since != "" {
				sinceTime, err := parseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since value: %w", err)
				}
				filter = filter.WithSince(sinceTime)
			}

			if until != "" {
				untilTime, err := parseDuration(until)
				if err != nil {
					return fmt.Errorf("invalid --until value: %w", err)
				}
				filter = filter.WithUntil(untilTime)
			}

			if agent != "" {
				filter = filter.WithAgent(agent)
			}

			sessions, err := app.Store.QuerySessions(ctx, filter)
			if err != nil {
				return err
			}

			if sessionID != "" {
				sessions = filterSessionsByPrefix(sessions, sessionID)
			}

			if model != "" {
				sessions = filterSessionsByModel(sessions, model)
			}

			if sync {
				syncSessionCosts(ctx, app, sessions, force)
			}

			if len(sessions) == 0 {
				return app.Presenter.RenderMessage("No sessions found.")
			}

			summary := buildCostSummary(sessions, by, model)
			return app.Presenter.RenderCostSummary(summary)
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "show costs since (e.g., \"1h\", \"2d\", \"2025-01-15\")")
	cmd.Flags().StringVar(&until, "until", "", "show costs until")
	cmd.Flags().BoolVar(&today, "today", false, "shorthand for since midnight")
	cmd.Flags().BoolVar(&yesterday, "yesterday", false, "filter to yesterday")
	cmd.Flags().StringVar(&agent, "agent", "", "filter by agent")
	cmd.Flags().StringVar(&model, "model", "", "filter by model name")
	cmd.Flags().StringVar(&sessionID, "session", "", "filter by session ID (prefix match)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	cmd.Flags().StringVar(&by, "by", "session", "group by: session, model, agent, day")
	cmd.Flags().BoolVar(&sync, "sync", false, "collect/refresh cost data before displaying")
	cmd.Flags().BoolVar(&force, "force", false, "with --sync: recompute even if already computed")
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum sessions")

	return cmd
}

func filterSessionsByPrefix(sessions []*session.Session, prefix string) []*session.Session {
	var result []*session.Session
	for _, s := range sessions {
		if strings.HasPrefix(s.ID.String(), prefix) {
			result = append(result, s)
		}
	}
	return result
}

func filterSessionsByModel(sessions []*session.Session, model string) []*session.Session {
	var result []*session.Session
	for _, s := range sessions {
		for _, m := range s.ModelUsage {
			if strings.Contains(strings.ToLower(m.Model), strings.ToLower(model)) {
				result = append(result, s)
				break
			}
		}
	}
	return result
}

func syncSessionCosts(ctx context.Context, app *App, sessions []*session.Session, force bool) {
	pw := tui.NewProgressWriter(os.Stderr, app.Config.ShouldUseColors())
	synced := 0

	for i, sess := range sessions {
		if sess.HasCostData() && !force {
			continue
		}

		pw.Update("Syncing cost data (%d/%d) ...", i+1, len(sessions))

		recoverTranscriptPath(ctx, app.Store, sess)
		collectSessionCost(sess)

		if sess.HasCostData() {
			synced++
			if err := app.Store.UpdateSession(ctx, sess); err != nil {
				log.Debugf("failed to update session %s: %v", sess.ID, err)
			}
		}
	}

	pw.Clear()
	if synced > 0 {
		fmt.Fprintf(os.Stderr, "Synced cost data for %d session(s)\n", synced)
	}
}

func buildCostSummary(sessions []*session.Session, groupBy, modelFilter string) *tui.CostSummaryView {
	summary := &tui.CostSummaryView{
		TotalSessions: len(sessions),
		GroupBy:       groupBy,
	}

	for _, s := range sessions {
		models := s.ModelUsage
		if modelFilter != "" {
			models = filterModelUsage(models, modelFilter)
		}

		var sessionTokens int64
		var sessionCost float64
		var modelViews []tui.ModelUsageView
		for _, m := range models {
			tokens := m.TotalTokens()
			sessionTokens += tokens
			modelViews = append(modelViews, tui.ModelUsageView{
				Model:            m.Model,
				InputTokens:      m.InputTokens,
				OutputTokens:     m.OutputTokens,
				CacheReadTokens:  m.CacheReadTokens,
				CacheWriteTokens: m.CacheWriteTokens,
			})
		}

		if modelFilter == "" {
			sessionCost = s.EstimatedCostUSD
			summary.TotalInputTokens += s.InputTokens
			summary.TotalOutputTokens += s.OutputTokens
			summary.TotalCacheRead += s.CacheReadTokens
			summary.TotalCacheWrite += s.CacheWriteTokens
		} else {
			for _, m := range models {
				summary.TotalInputTokens += m.InputTokens
				summary.TotalOutputTokens += m.OutputTokens
				summary.TotalCacheRead += m.CacheReadTokens
				summary.TotalCacheWrite += m.CacheWriteTokens
			}
		}

		summary.TotalTokens += sessionTokens
		summary.TotalCost += sessionCost

		summary.Sessions = append(summary.Sessions, &tui.CostSessionView{
			ID:          s.ID.String(),
			ShortID:     tui.FormatShortID(s.ID.String()),
			AgentName:   s.AgentName,
			ProjectName: s.ProjectName,
			StartedAt:   s.StartedAt,
			ModelCount:  len(models),
			TotalTokens: sessionTokens,
			TotalCost:   sessionCost,
			Models:      modelViews,
		})
	}

	switch groupBy {
	case "model":
		summary.Groups = buildModelGroups(sessions, modelFilter)
	case "agent":
		summary.Groups = buildAgentGroups(sessions, modelFilter)
	case "day":
		summary.Groups = buildDayGroups(sessions, modelFilter)
	}

	return summary
}

func filterModelUsage(models []cost.ModelUsage, filter string) []cost.ModelUsage {
	var result []cost.ModelUsage
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.Model), strings.ToLower(filter)) {
			result = append(result, m)
		}
	}
	return result
}

func buildModelGroups(sessions []*session.Session, modelFilter string) []tui.CostGroupView {
	groups := make(map[string]*tui.CostGroupView)
	sessionSeen := make(map[string]map[string]bool)

	for _, s := range sessions {
		models := s.ModelUsage
		if modelFilter != "" {
			models = filterModelUsage(models, modelFilter)
		}
		for _, m := range models {
			g, ok := groups[m.Model]
			if !ok {
				g = &tui.CostGroupView{Label: m.Model}
				groups[m.Model] = g
				sessionSeen[m.Model] = make(map[string]bool)
			}
			if !sessionSeen[m.Model][s.ID.String()] {
				g.SessionCount++
				sessionSeen[m.Model][s.ID.String()] = true
			}
			g.InputTokens += m.InputTokens
			g.OutputTokens += m.OutputTokens
			g.CacheRead += m.CacheReadTokens
			g.CacheWrite += m.CacheWriteTokens
			g.TotalTokens += m.TotalTokens()
		}
	}

	return sortedGroups(groups)
}

func buildAgentGroups(sessions []*session.Session, modelFilter string) []tui.CostGroupView {
	groups := make(map[string]*tui.CostGroupView)

	for _, s := range sessions {
		models := s.ModelUsage
		if modelFilter != "" {
			models = filterModelUsage(models, modelFilter)
		}

		g, ok := groups[s.AgentName]
		if !ok {
			g = &tui.CostGroupView{Label: s.AgentName}
			groups[s.AgentName] = g
		}
		g.SessionCount++

		if modelFilter == "" {
			g.InputTokens += s.InputTokens
			g.OutputTokens += s.OutputTokens
			g.CacheRead += s.CacheReadTokens
			g.CacheWrite += s.CacheWriteTokens
			g.TotalTokens += s.InputTokens + s.OutputTokens + s.CacheReadTokens + s.CacheWriteTokens
			g.TotalCost += s.EstimatedCostUSD
		} else {
			for _, m := range models {
				g.InputTokens += m.InputTokens
				g.OutputTokens += m.OutputTokens
				g.CacheRead += m.CacheReadTokens
				g.CacheWrite += m.CacheWriteTokens
				g.TotalTokens += m.TotalTokens()
			}
		}
	}

	return sortedGroups(groups)
}

func buildDayGroups(sessions []*session.Session, modelFilter string) []tui.CostGroupView {
	groups := make(map[string]*tui.CostGroupView)

	for _, s := range sessions {
		models := s.ModelUsage
		if modelFilter != "" {
			models = filterModelUsage(models, modelFilter)
		}

		day := s.StartedAt.Format("2006-01-02")
		g, ok := groups[day]
		if !ok {
			g = &tui.CostGroupView{Label: day}
			groups[day] = g
		}
		g.SessionCount++

		if modelFilter == "" {
			g.InputTokens += s.InputTokens
			g.OutputTokens += s.OutputTokens
			g.CacheRead += s.CacheReadTokens
			g.CacheWrite += s.CacheWriteTokens
			g.TotalTokens += s.InputTokens + s.OutputTokens + s.CacheReadTokens + s.CacheWriteTokens
			g.TotalCost += s.EstimatedCostUSD
		} else {
			for _, m := range models {
				g.InputTokens += m.InputTokens
				g.OutputTokens += m.OutputTokens
				g.CacheRead += m.CacheReadTokens
				g.CacheWrite += m.CacheWriteTokens
				g.TotalTokens += m.TotalTokens()
			}
		}
	}

	return sortedGroups(groups)
}

func sortedGroups(groups map[string]*tui.CostGroupView) []tui.CostGroupView {
	result := make([]tui.CostGroupView, 0, len(groups))
	for _, g := range groups {
		result = append(result, *g)
	}
	return result
}
