package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
	"github.com/safedep/gryph/tui/component/livelog"
	"github.com/spf13/cobra"
)

type logParams struct {
	follow   bool
	live     bool
	interval time.Duration
	since    string
	until    string
	today    bool
	limit    int
	session  string
	agent    string
	format   string
}

// NewLogsCmd creates the logs command.
func NewLogsCmd() *cobra.Command {
	var p logParams

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Display recent agent activity",
		Long: `Display recent agent activity.

Shows audit logs grouped by session. Use filters to narrow down
the results.`,
		Example: `  gryph logs
  gryph logs --follow
  gryph logs --live
  gryph logs --since "1h"
  gryph logs --today
  gryph logs --agent claude-code`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if p.live && p.follow {
				return fmt.Errorf("--live and --follow are mutually exclusive")
			}

			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}

			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %w", err)
				}
			}()

			if p.live {
				return runLiveLogs(app, p)
			}

			outputFormat := getFormat(p.format)
			if p.follow {
				outputFormat = tui.FormatJSONL
			}

			app.Presenter = tui.NewPresenter(outputFormat, tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors() && !p.follow,
			})

			if p.follow {
				return runFollowLogs(ctx, app, p)
			}
			return runListLogs(ctx, app, p)
		},
	}

	cmd.Flags().BoolVarP(&p.follow, "follow", "f", false, "stream new events")
	cmd.Flags().BoolVar(&p.live, "live", false, "interactive full-screen TUI monitor")
	cmd.Flags().DurationVar(&p.interval, "interval", 2*time.Second, "poll interval for follow mode")
	cmd.Flags().StringVar(&p.since, "since", "", "show events since (e.g., \"1h\", \"2d\", \"2025-01-15\")")
	cmd.Flags().StringVar(&p.until, "until", "", "show events until")
	cmd.Flags().BoolVar(&p.today, "today", false, "shorthand for since midnight")
	cmd.Flags().IntVar(&p.limit, "limit", 50, "maximum events")
	cmd.Flags().StringVar(&p.session, "session", "", "filter by session ID")
	cmd.Flags().StringVar(&p.agent, "agent", "", "filter by agent")
	cmd.Flags().StringVar(&p.format, "format", "table", "output format: table, json, jsonl")

	return cmd
}

func runLiveLogs(app *App, p logParams) error {
	sinceTime, err := parseSinceTime(p)
	if err != nil {
		return err
	}

	opts := livelog.Options{
		Store:        app.Store,
		PollInterval: p.interval,
		AgentFilter:  p.agent,
		InitialLimit: p.limit,
		Since:        sinceTime,
	}

	prog := tea.NewProgram(livelog.New(opts), tea.WithAltScreen())
	_, err = prog.Run()

	return err
}

func parseSinceTime(p logParams) (time.Time, error) {
	if p.today {
		now := time.Now()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return midnight.UTC(), nil
	}
	if p.since != "" {
		t, err := parseDuration(p.since)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid --since value: %w", err)
		}
		return t, nil
	}
	return time.Now().UTC().Add(-24 * time.Hour), nil
}

func buildEventFilter(p logParams) (*events.EventFilter, error) {
	filter := events.NewEventFilter().WithLimit(p.limit)

	sinceTime, err := parseSinceTime(p)
	if err != nil {
		return nil, err
	}
	filter = filter.WithSince(sinceTime)

	if p.until != "" {
		untilTime, err := parseDuration(p.until)
		if err != nil {
			return nil, fmt.Errorf("invalid --until value: %w", err)
		}
		filter = filter.WithUntil(untilTime)
	}

	if p.agent != "" {
		filter = filter.WithAgents(p.agent)
	}

	if p.session != "" {
		sessionID, err := uuid.Parse(p.session)
		if err != nil {
			return nil, fmt.Errorf("invalid --session value: %w", err)
		}
		filter = filter.WithSession(sessionID)
	}

	return filter, nil
}

func runListLogs(ctx context.Context, app *App, p logParams) error {
	filter, err := buildEventFilter(p)
	if err != nil {
		return err
	}

	evts, err := app.Store.QueryEvents(ctx, filter)
	if err != nil {
		return err
	}

	if len(evts) == 0 {
		return app.Presenter.RenderMessage("No events found. Run 'gryph install' to start logging agent activity.")
	}

	slices.Reverse(evts)

	eventViews := make([]*tui.EventView, len(evts))
	for i, e := range evts {
		eventViews[i] = eventToView(e)
	}

	return app.Presenter.RenderEvents(eventViews)
}

func runFollowLogs(ctx context.Context, app *App, p logParams) error {
	filter, err := buildEventFilter(p)
	if err != nil {
		return err
	}

	evts, err := app.Store.QueryEvents(ctx, filter)
	if err != nil {
		return err
	}

	var lastTimestamp time.Time
	if len(evts) > 0 {
		slices.Reverse(evts)
		eventViews := make([]*tui.EventView, len(evts))
		for i, e := range evts {
			eventViews[i] = eventToView(e)
		}
		if err := app.Presenter.RenderEvents(eventViews); err != nil {
			return err
		}
		lastTimestamp = evts[len(evts)-1].Timestamp
	} else {
		lastTimestamp = time.Now().UTC()
	}

	sigCtx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCtx.Done():
			return nil
		case <-ticker.C:
			pollFilter := events.NewEventFilter().
				WithSince(lastTimestamp.Add(time.Millisecond))

			if p.agent != "" {
				pollFilter = pollFilter.WithAgents(p.agent)
			}

			newEvts, err := app.Store.QueryEvents(sigCtx, pollFilter)
			if err != nil {
				continue
			}

			if len(newEvts) > 0 {
				slices.Reverse(newEvts)
				eventViews := make([]*tui.EventView, len(newEvts))
				for i, e := range newEvts {
					eventViews[i] = eventToView(e)
				}
				if err := app.Presenter.RenderEvents(eventViews); err != nil {
					return err
				}
				lastTimestamp = newEvts[len(newEvts)-1].Timestamp
			}
		}
	}
}

// parseDuration parses a duration string like "1h", "2d", or a date.
func parseDuration(s string) (time.Time, error) {
	// Try parsing as duration
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().UTC().Add(-d), nil
	}

	// Try parsing as relative duration (e.g., "1d", "1w")
	if len(s) > 1 {
		unit := s[len(s)-1]
		value := s[:len(s)-1]
		var multiplier time.Duration
		switch unit {
		case 'd':
			multiplier = 24 * time.Hour
		case 'w':
			multiplier = 7 * 24 * time.Hour
		}
		if multiplier > 0 {
			if d, err := time.ParseDuration(value + "h"); err == nil {
				return time.Now().UTC().Add(-d * time.Duration(multiplier/time.Hour)), nil
			}
		}
	}

	// Try parsing as date
	layouts := []string{
		"2006-01-02",
		"2006-01-02 15:04:05",
		time.RFC3339,
	}

	for _, layout := range layouts {
		if layout == time.RFC3339 {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC(), nil
			}
		} else {
			if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
				return t.UTC(), nil
			}
		}
	}

	// Everything else failed, return an error
	return time.Time{}, fmt.Errorf("failed to parse duration")
}

// eventToView converts an event to a view model.
func eventToView(e *events.Event) *tui.EventView {
	view := &tui.EventView{
		ID:               e.ID.String(),
		ShortID:          tui.FormatShortID(e.ID.String()),
		SessionID:        e.SessionID.String(),
		ShortSessionID:   tui.FormatShortID(e.SessionID.String()),
		Sequence:         e.Sequence,
		Timestamp:        e.Timestamp,
		AgentName:        e.AgentName,
		AgentDisplayName: getAgentDisplayName(e.AgentName),
		ActionType:       string(e.ActionType),
		ActionDisplay:    e.ActionType.DisplayName(),
		ToolName:         e.ToolName,
		ResultStatus:     string(e.ResultStatus),
		ErrorMessage:     e.ErrorMessage,
		IsSensitive:      e.IsSensitive,
		HasDiff:          e.DiffContent != "",
	}

	// Extract path/command from payload
	switch e.ActionType {
	case events.ActionFileRead:
		if p, err := e.GetFileReadPayload(); err == nil && p != nil {
			view.Path = p.DisplayTarget()
		}
	case events.ActionFileWrite:
		if p, err := e.GetFileWritePayload(); err == nil && p != nil {
			view.Path = p.Path
			view.LinesAdded = p.LinesAdded
			view.LinesRemoved = p.LinesRemoved
		}
	case events.ActionCommandExec:
		if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
			view.Command = p.Command
			view.ExitCode = p.ExitCode
			view.DurationMs = p.DurationMs
		}
	}

	return view
}
