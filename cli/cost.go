package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/pricing"
	"github.com/safedep/gryph/storage"
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

			provider, err := pricing.NewBundledProvider()
			if err != nil {
				return fmt.Errorf("failed to load pricing data: %w", err)
			}

			summary := buildCostSummary(provider, sessions, by, model)
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

func buildCostSummary(provider cost.PricingProvider, sessions []*session.Session, groupBy, modelFilter string) *tui.CostSummaryView {
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
			modelCost := computeModelCost(provider, m)
			sessionCost += modelCost
			modelViews = append(modelViews, tui.ModelUsageView{
				Model:            m.Model,
				InputTokens:      m.InputTokens,
				OutputTokens:     m.OutputTokens,
				CacheReadTokens:  m.CacheReadTokens,
				CacheWriteTokens: m.CacheWriteTokens,
				Cost:             modelCost,
			})
			summary.TotalInputTokens += m.InputTokens
			summary.TotalOutputTokens += m.OutputTokens
			summary.TotalCacheRead += m.CacheReadTokens
			summary.TotalCacheWrite += m.CacheWriteTokens
		}

		if sessionCost == 0 && modelFilter == "" {
			sessionCost = s.EstimatedCostUSD
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
		summary.Groups = buildModelGroups(provider, sessions, modelFilter)
	case "agent":
		summary.Groups = buildAgentGroups(provider, sessions, modelFilter)
	case "day":
		summary.Groups = buildDayGroups(provider, sessions, modelFilter)
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

func buildModelGroups(provider cost.PricingProvider, sessions []*session.Session, modelFilter string) []tui.CostGroupView {
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
			g.TotalCost += computeModelCost(provider, m)
		}
	}

	return sortedGroupsByCost(groups)
}

func buildAgentGroups(provider cost.PricingProvider, sessions []*session.Session, modelFilter string) []tui.CostGroupView {
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

		for _, m := range models {
			g.InputTokens += m.InputTokens
			g.OutputTokens += m.OutputTokens
			g.CacheRead += m.CacheReadTokens
			g.CacheWrite += m.CacheWriteTokens
			g.TotalTokens += m.TotalTokens()
			g.TotalCost += computeModelCost(provider, m)
		}
	}

	return sortedGroupsByCost(groups)
}

func buildDayGroups(provider cost.PricingProvider, sessions []*session.Session, modelFilter string) []tui.CostGroupView {
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

		for _, m := range models {
			g.InputTokens += m.InputTokens
			g.OutputTokens += m.OutputTokens
			g.CacheRead += m.CacheReadTokens
			g.CacheWrite += m.CacheWriteTokens
			g.TotalTokens += m.TotalTokens()
			g.TotalCost += computeModelCost(provider, m)
		}
	}

	return sortedGroupsByLabel(groups)
}

func computeModelCost(provider cost.PricingProvider, m cost.ModelUsage) float64 {
	if provider == nil {
		return 0
	}
	p, err := provider.GetPricing(m.Model)
	if err != nil || p == nil {
		return 0
	}
	return float64(m.InputTokens)*p.InputPer1M/1_000_000 +
		float64(m.OutputTokens)*p.OutputPer1M/1_000_000 +
		float64(m.CacheReadTokens)*p.CacheRead/1_000_000 +
		float64(m.CacheWriteTokens)*p.CacheWrite/1_000_000
}

func sortedGroupsByLabel(groups map[string]*tui.CostGroupView) []tui.CostGroupView {
	result := make([]tui.CostGroupView, 0, len(groups))
	for _, g := range groups {
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Label < result[j].Label
	})
	return result
}

func sortedGroupsByCost(groups map[string]*tui.CostGroupView) []tui.CostGroupView {
	result := make([]tui.CostGroupView, 0, len(groups))
	for _, g := range groups {
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalCost > result[j].TotalCost
	})
	return result
}

func recoverTranscriptPath(ctx context.Context, store storage.Store, sess *session.Session) {
	if sess.TranscriptPath != "" {
		return
	}

	evts, err := store.GetEventsBySession(ctx, sess.ID)
	if err != nil {
		log.Errorf("failed to get events for session %s: %v", sess.ID, err)
		return
	}
	if len(evts) == 0 {
		return
	}

	for _, evt := range evts {
		if len(evt.RawEvent) == 0 {
			continue
		}
		var raw struct {
			TranscriptPath string `json:"transcript_path"`
		}
		if err := json.Unmarshal(evt.RawEvent, &raw); err != nil {
			continue
		}
		if raw.TranscriptPath != "" {
			sess.TranscriptPath = raw.TranscriptPath
			if err := store.UpdateSession(ctx, sess); err != nil {
				log.Errorf("failed to update session transcript path: %v", err)
			}
			return
		}
	}
}

func collectSessionCost(sess *session.Session) {
	if sess.TranscriptPath == "" {
		return
	}

	var collector cost.TokenCollector
	switch sess.AgentName {
	case agent.AgentClaudeCode:
		collector = claudecode.NewTranscriptCollector()
	default:
		return
	}

	usage, err := collector.Collect(context.Background(), sess.TranscriptPath)
	if err != nil {
		log.Debugf("failed to collect cost data: %v", err)
		return
	}
	if usage == nil {
		return
	}

	provider, err := pricing.NewBundledProvider()
	if err != nil {
		log.Debugf("failed to create pricing provider: %v", err)
		return
	}

	calc := cost.NewDefaultCalculator(provider, sess.ID, collector.Source())
	sc, err := calc.Calculate(usage)
	if err != nil {
		log.Debugf("failed to calculate cost: %v", err)
		return
	}
	if sc == nil {
		return
	}

	sess.InputTokens = sc.Usage.InputTokens
	sess.OutputTokens = sc.Usage.OutputTokens
	sess.CacheReadTokens = sc.Usage.CacheReadTokens
	sess.CacheWriteTokens = sc.Usage.CacheWriteTokens
	sess.EstimatedCostUSD = sc.TotalCost
	sess.ModelUsage = sc.Usage.Models
	sess.CostSource = string(sc.Source)
	now := sc.ComputedAt
	sess.CostComputedAt = &now
}
