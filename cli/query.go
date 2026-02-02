package cli

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewQueryCmd creates the query command.
func NewQueryCmd() *cobra.Command {
	var (
		since       string
		until       string
		today       bool
		yesterday   bool
		agents      []string
		session     string
		actions     []string
		filePattern string
		cmdPattern  string
		status      string
		showDiff    bool
		format      string
		limit       int
		offset      int
		count       bool
	)

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query audit logs with filters",
		Long: `Query audit logs with filters.

Provides comprehensive filtering capabilities to search
through the audit history.`,
		Example: `  gryph query --file "src/**/*.ts"
  gryph query --since "1w" --agent claude-code
  gryph query --action file_write --today
  gryph query --command "npm *"`,
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
			filter := events.NewEventFilter().WithLimit(limit).WithOffset(offset)

			if today {
				tf := events.Today()
				filter = filter.WithSince(*tf.Since)
			} else if yesterday {
				tf := events.Yesterday()
				filter = filter.WithSince(*tf.Since).WithUntil(*tf.Until)
			} else if since != "" {
				if sinceTime, err := parseDuration(since); err == nil {
					filter = filter.WithSince(sinceTime)
				}
			}

			if until != "" {
				if untilTime, err := parseDuration(until); err == nil {
					filter = filter.WithUntil(untilTime)
				}
			}

			if len(agents) > 0 {
				filter = filter.WithAgents(agents...)
			}

			if session != "" {
				sessionID, err := uuid.Parse(session)
				if err != nil {
					s, err := app.Store.GetSessionByPrefix(ctx, session)
					if err != nil {
						return fmt.Errorf("session not found: %s", session)
					}
					if s == nil {
						return fmt.Errorf("session not found: %s", session)
					}
					sessionID = s.ID
				}
				filter = filter.WithSession(sessionID)
			}

			if status != "" {
				rs := events.ResultStatus(status)
				if !rs.IsValid() {
					return fmt.Errorf("invalid status: %s", status)
				}
				filter = filter.WithStatuses(rs)
			}

			if len(actions) > 0 {
				actionTypes := make([]events.ActionType, len(actions))
				for i, a := range actions {
					at, err := events.ParseActionType(a)
					if err != nil {
						return err
					}
					actionTypes[i] = at
				}
				filter = filter.WithActions(actionTypes...)
			}

			if filePattern != "" {
				filter = filter.WithFilePattern(filePattern)
			}

			if cmdPattern != "" {
				filter = filter.WithCommandPattern(cmdPattern)
			}

			// Handle count-only mode
			if count {
				n, err := app.Store.CountEvents(ctx, filter)
				if err != nil {
					return err
				}
				return app.Presenter.RenderMessage(tui.FormatNumber(n) + " events")
			}

			// Query events
			evts, err := app.Store.QueryEvents(ctx, filter)
			if err != nil {
				return err
			}

			if len(evts) == 0 {
				return app.Presenter.RenderMessage("No events found matching the query.")
			}

			// Convert to view models
			eventViews := make([]*tui.EventView, len(evts))
			for i, e := range evts {
				eventViews[i] = eventToView(e)
			}

			return app.Presenter.RenderEvents(eventViews)
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "start time")
	cmd.Flags().StringVar(&until, "until", "", "end time")
	cmd.Flags().BoolVar(&today, "today", false, "filter to today")
	cmd.Flags().BoolVar(&yesterday, "yesterday", false, "filter to yesterday")
	cmd.Flags().StringArrayVar(&agents, "agent", nil, "filter by agent (repeatable)")
	cmd.Flags().StringVar(&session, "session", "", "filter by session ID (prefix match)")
	cmd.Flags().StringArrayVar(&actions, "action", nil, "filter by action type (repeatable)")
	cmd.Flags().StringVar(&filePattern, "file", "", "filter by file path (glob)")
	cmd.Flags().StringVar(&cmdPattern, "command", "", "filter by command (glob)")
	cmd.Flags().StringVar(&status, "status", "", "filter by result status")
	cmd.Flags().BoolVar(&showDiff, "show-diff", false, "include diff content in output")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json, jsonl, csv")
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum results")
	cmd.Flags().IntVar(&offset, "offset", 0, "skip first n results")
	cmd.Flags().BoolVar(&count, "count", false, "show count only")

	return cmd
}
