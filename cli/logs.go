package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewLogsCmd creates the logs command.
func NewLogsCmd() *cobra.Command {
	var (
		follow   bool
		interval time.Duration
		since    string
		until    string
		today    bool
		limit    int
		session  string
		agent    string
		format   string
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Display recent agent activity",
		Long: `Display recent agent activity.

Shows audit logs grouped by session. Use filters to narrow down
the results.`,
		Example: `  gryph logs
  gryph logs --follow
  gryph logs --since "1h"
  gryph logs --today
  gryph logs --agent claude-code`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			app, err := loadApp()
			if err != nil {
				return err
			}

			// In follow mode, force JSONL format for streaming compatibility
			outputFormat := getFormat(format)
			if follow {
				outputFormat = tui.FormatJSONL
			}

			// Update presenter format
			app.Presenter = tui.NewPresenter(outputFormat, tui.PresenterOptions{
				Writer:    cmd.OutOrStdout(),
				UseColors: app.Config.ShouldUseColors() && !follow,
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
			filter := events.NewEventFilter().WithLimit(limit)

			if today {
				filter = events.Today().WithLimit(limit)
			} else if since != "" {
				if sinceTime, err := parseDuration(since); err == nil {
					filter = filter.WithSince(sinceTime)
				}
			} else {
				// Default to last 24 hours
				filter = filter.WithSince(time.Now().Add(-24 * time.Hour))
			}

			if until != "" {
				if untilTime, err := parseDuration(until); err == nil {
					filter = filter.WithUntil(untilTime)
				}
			}

			if agent != "" {
				filter = filter.WithAgents(agent)
			}

			// Query initial events
			evts, err := app.Store.QueryEvents(ctx, filter)
			if err != nil {
				return err
			}

			// If not in follow mode, just display results and exit
			if !follow {
				if len(evts) == 0 {
					return app.Presenter.RenderMessage("No events found. Run 'gryph install' to start logging agent activity.")
				}

				eventViews := make([]*tui.EventView, len(evts))
				for i, e := range evts {
					eventViews[i] = eventToView(e)
				}
				return app.Presenter.RenderEvents(eventViews)
			}

			// Follow mode: display initial events, then poll for new ones
			var lastTimestamp time.Time
			if len(evts) > 0 {
				eventViews := make([]*tui.EventView, len(evts))
				for i, e := range evts {
					eventViews[i] = eventToView(e)
				}
				if err := app.Presenter.RenderEvents(eventViews); err != nil {
					return err
				}
				// Events are returned newest first, so the last event has oldest timestamp
				// We want to poll for events newer than the newest (first in slice)
				lastTimestamp = evts[0].Timestamp
			} else {
				lastTimestamp = time.Now()
			}

			// Set up signal handling for graceful exit
			sigCtx, cancel := signal.NotifyContext(ctx, os.Interrupt)
			defer cancel()

			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			// Polling loop
			for {
				select {
				case <-sigCtx.Done():
					return nil
				case <-ticker.C:
					// Query for events newer than last timestamp
					// Use a small delta to avoid missing events due to timestamp precision
					pollFilter := events.NewEventFilter().
						WithSince(lastTimestamp.Add(time.Millisecond))

					if agent != "" {
						pollFilter = pollFilter.WithAgents(agent)
					}

					newEvts, err := app.Store.QueryEvents(sigCtx, pollFilter)
					if err != nil {
						// Log warning but don't exit on transient errors
						continue
					}

					if len(newEvts) > 0 {
						eventViews := make([]*tui.EventView, len(newEvts))
						for i, e := range newEvts {
							eventViews[i] = eventToView(e)
						}
						if err := app.Presenter.RenderEvents(eventViews); err != nil {
							return err
						}
						// Update timestamp to newest event
						lastTimestamp = newEvts[0].Timestamp
					}
				}
			}
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream new events")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "poll interval for follow mode")
	cmd.Flags().StringVar(&since, "since", "", "show events since (e.g., \"1h\", \"2d\", \"2025-01-15\")")
	cmd.Flags().StringVar(&until, "until", "", "show events until")
	cmd.Flags().BoolVar(&today, "today", false, "shorthand for since midnight")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum events")
	cmd.Flags().StringVar(&session, "session", "", "filter by session ID")
	cmd.Flags().StringVar(&agent, "agent", "", "filter by agent")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json, jsonl")

	return cmd
}

// parseDuration parses a duration string like "1h", "2d", or a date.
func parseDuration(s string) (time.Time, error) {
	// Try parsing as duration
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
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
				return time.Now().Add(-d * time.Duration(multiplier/time.Hour)), nil
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
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	// Everything else failed, return an error
	return time.Time{}, fmt.Errorf("failed to parse duration")
}

// eventToView converts an event to a view model.
func eventToView(e *events.Event) *tui.EventView {
	view := &tui.EventView{
		ID:             e.ID.String(),
		ShortID:        tui.FormatShortID(e.ID.String()),
		SessionID:      e.SessionID.String(),
		ShortSessionID: tui.FormatShortID(e.SessionID.String()),
		Sequence:       e.Sequence,
		Timestamp:      e.Timestamp,
		AgentName:      e.AgentName,
		ActionType:     string(e.ActionType),
		ActionDisplay:  actionDisplay(e.ActionType),
		ToolName:       e.ToolName,
		ResultStatus:   string(e.ResultStatus),
		ErrorMessage:   e.ErrorMessage,
		IsSensitive:    e.IsSensitive,
		HasDiff:        e.DiffContent != "",
	}

	// Extract path/command from payload
	switch e.ActionType {
	case events.ActionFileRead:
		if p, err := e.GetFileReadPayload(); err == nil && p != nil {
			view.Path = p.Path
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

// actionDisplay returns a short display name for an action type.
func actionDisplay(action events.ActionType) string {
	switch action {
	case events.ActionFileRead:
		return "read"
	case events.ActionFileWrite:
		return "write"
	case events.ActionFileDelete:
		return "delete"
	case events.ActionCommandExec:
		return "exec"
	case events.ActionNetworkRequest:
		return "http"
	case events.ActionToolUse:
		return "tool"
	case events.ActionSessionStart:
		return "session_start"
	case events.ActionSessionEnd:
		return "session_end"
	case events.ActionNotification:
		return "notification"
	case events.ActionUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}
