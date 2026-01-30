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

// NewDiffCmd creates the diff command.
func NewDiffCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "diff <event-id>",
		Short: "View the diff content for a specific file_write event",
		Long: `View the diff content for a specific file_write event.

Shows the unified diff for a file write event if it was captured
during full logging mode. Returns an error if the event is not
a file_write action or if diff was not captured.`,
		Example: `  gryph diff a1b2c3d4
  gryph diff a1b2c3d4-e5f6-7890-abcd-ef1234567890
  gryph diff a1b2c3d4 --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			eventIDArg := args[0]

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

			// Parse event ID
			eventID, err := uuid.Parse(eventIDArg)
			if err != nil {
				// Could be a prefix - for now just error
				return fmt.Errorf("invalid event ID: %s", eventIDArg)
			}

			// Get event
			event, err := app.Store.GetEvent(ctx, eventID)
			if err != nil {
				return fmt.Errorf("failed to get event: %w", err)
			}
			if event == nil {
				return fmt.Errorf("event not found: %s", eventIDArg)
			}

			// Check if it's a file_write action
			if event.ActionType != events.ActionFileWrite {
				return fmt.Errorf("event %s is not a file_write action (type: %s)", eventIDArg, event.ActionType)
			}

			// Build diff view
			view := &tui.DiffView{
				EventID:   event.ID.String(),
				SessionID: event.SessionID.String(),
				Timestamp: event.Timestamp,
			}

			// Get file path from payload
			if payload, err := event.GetFileWritePayload(); err == nil && payload != nil {
				view.FilePath = payload.Path
			}

			// Check for diff content
			if event.IsSensitive {
				view.Available = false
				view.Message = "[SENSITIVE - content not logged]"
			} else if event.DiffContent == "" {
				view.Available = false
				view.Message = "Diff not captured (logging level may have been minimal or standard)"
			} else {
				view.Available = true
				view.Content = event.DiffContent
			}

			return app.Presenter.RenderDiff(view)
		},
	}

	cmd.Flags().StringVar(&format, "format", "unified", "output format: unified, json")

	return cmd
}
