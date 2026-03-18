package cli

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/tui"
	"github.com/safedep/gryph/tui/component/query"
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
		format      string
		limit       int
		offset      int
		count       bool
		sensitive   bool
		interactive bool
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

			if interactive {
				return runInteractiveQuery(app, since, until, today, yesterday, agents, actions,
					filePattern, cmdPattern, status, session, sensitive)
			}

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

			if sensitive {
				filter = filter.WithSensitive(true)
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

			slices.Reverse(evts)

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
	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json, jsonl, csv")
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum results")
	cmd.Flags().IntVar(&offset, "offset", 0, "skip first n results")
	cmd.Flags().BoolVar(&count, "count", false, "show count only")
	cmd.Flags().BoolVar(&sensitive, "sensitive", false, "filter to events involving sensitive file access")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "launch interactive TUI browser")

	return cmd
}

func runInteractiveQuery(app *App, since, until string, today, yesterday bool,
	agents, actions []string, filePattern, cmdPattern, status, sessionID string, sensitive bool) error {

	opts := query.Options{
		Store:       app.Store,
		Agents:      agents,
		Actions:     actions,
		FilePattern: filePattern,
		CmdPattern:  cmdPattern,
		Session:     sessionID,
		Sensitive:   sensitive,
	}

	searcher, ok := app.Store.(storage.Searcher)
	if !ok {
		return fmt.Errorf("store does not support search, interactive mode requires FTS")
	}
	opts.Searcher = searcher

	if today {
		now := time.Now()
		opts.Since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
	} else if yesterday {
		now := time.Now()
		opts.Since = time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location()).UTC()
		opts.Until = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
	} else if since != "" {
		if t, err := parseDuration(since); err == nil {
			opts.Since = t
		}
	}

	if until != "" {
		if t, err := parseDuration(until); err == nil {
			opts.Until = t
		}
	}

	if status != "" {
		opts.Statuses = []string{status}
	}

	prog := tea.NewProgram(query.New(opts), tea.WithAltScreen())
	_, err := prog.Run()
	return err
}
