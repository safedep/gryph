package cli

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

// NewSessionCmd creates the session command.
func NewSessionCmd() *cobra.Command {
	var (
		format   string
		showDiff bool
	)

	cmd := &cobra.Command{
		Use:   "session <id>",
		Short: "Show detailed view of a specific session",
		Long: `Show detailed view of a specific session.

Displays all actions performed during the session in
chronological order with full metadata.`,
		Example: `  gryph session abc123
  gryph session 7f3a2b1c-d4e5-6f7a-8b9c-0d1e2f3a4b5c
  gryph session abc123 --show-diff`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			sessionIDArg := args[0]

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
				return err
			}
			defer app.Close()

			// Try to parse as full UUID first
			var sess *interface{}
			sessionID, err := uuid.Parse(sessionIDArg)
			if err != nil {
				// Try prefix match
				s, err := app.Store.GetSessionByPrefix(ctx, sessionIDArg)
				if err != nil {
					return fmt.Errorf("session not found: %s", sessionIDArg)
				}
				if s == nil {
					return fmt.Errorf("session not found: %s", sessionIDArg)
				}
				_ = sess // placeholder
				sessionID = s.ID
			}

			// Get session
			session, err := app.Store.GetSession(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("failed to get session: %w", err)
			}
			if session == nil {
				return fmt.Errorf("session not found: %s", sessionIDArg)
			}

			// Get events for session
			evts, err := app.Store.GetEventsBySession(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("failed to get events: %w", err)
			}

			// Convert to view models
			sessionView := sessionToView(session)
			eventViews := make([]*tui.EventView, len(evts))
			for i, e := range evts {
				eventViews[i] = eventToView(e)
			}

			return app.Presenter.RenderSession(sessionView, eventViews)
		},
	}

	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json")
	cmd.Flags().BoolVar(&showDiff, "show-diff", false, "include diff content for file_write events")

	return cmd
}
