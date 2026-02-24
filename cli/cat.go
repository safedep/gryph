package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/tui"
	"github.com/spf13/cobra"
)

func NewCatCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "cat <event-id> [event-id...]",
		Short: "Show full details of one or more events",
		Long: `Show full details of one or more events.

Displays all fields including payload, diff content, raw event,
and conversation context. Accepts full UUIDs or ID prefixes.`,
		Example: `  gryph cat a1b2c3d4
  gryph cat a1b2c3d4-e5f6-7890-abcd-ef1234567890
  gryph cat a1b2c3d4 f5e6d7c8 --format json
  gryph cat a1b2c3d4 --format jsonl`,
		Args: cobra.MinimumNArgs(1),
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

			views := make([]*tui.EventDetailView, 0, len(args))
			for _, idArg := range args {
				event, err := resolveEvent(ctx, app, idArg)
				if err != nil {
					return err
				}
				views = append(views, eventToDetailView(event))
			}

			return app.Presenter.RenderEventDetails(views)
		},
	}

	cmd.Flags().StringVar(&format, "format", "table", "output format: table, json, jsonl, csv")

	return cmd
}

func resolveEvent(ctx context.Context, app *App, idArg string) (*events.Event, error) {
	eventID, err := uuid.Parse(idArg)
	if err != nil {
		e, err := app.Store.GetEventByPrefix(ctx, idArg)
		if err != nil {
			return nil, fmt.Errorf("event not found: %s", idArg)
		}
		if e == nil {
			return nil, fmt.Errorf("event not found: %s", idArg)
		}
		return e, nil
	}

	event, err := app.Store.GetEvent(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get event: %w", err)
	}
	if event == nil {
		return nil, fmt.Errorf("event not found: %s", idArg)
	}
	return event, nil
}

func eventToDetailView(e *events.Event) *tui.EventDetailView {
	view := &tui.EventDetailView{
		ID:               e.ID.String(),
		SessionID:        e.SessionID.String(),
		AgentSessionID:   e.AgentSessionID,
		Sequence:         e.Sequence,
		Timestamp:        e.Timestamp,
		DurationMs:       e.DurationMs,
		AgentName:        e.AgentName,
		AgentDisplayName: getAgentDisplayName(e.AgentName),
		AgentVersion:     e.AgentVersion,
		WorkingDirectory: e.WorkingDirectory,
		ActionType:       string(e.ActionType),
		ActionDisplay:    e.ActionType.DisplayName(),
		ToolName:         e.ToolName,
		ResultStatus:     string(e.ResultStatus),
		ErrorMessage:     e.ErrorMessage,
		IsSensitive:      e.IsSensitive,
		DiffContent:      e.DiffContent,
		RawEvent:         e.RawEvent,
		ConvContext:      e.ConversationContext,
	}

	switch e.ActionType {
	case events.ActionFileRead:
		if p, err := e.GetFileReadPayload(); err == nil && p != nil {
			view.Payload = p
		}
	case events.ActionFileWrite:
		if p, err := e.GetFileWritePayload(); err == nil && p != nil {
			view.Payload = p
		}
	case events.ActionCommandExec:
		if p, err := e.GetCommandExecPayload(); err == nil && p != nil {
			view.Payload = p
		}
	case events.ActionToolUse:
		if p, err := e.GetToolUsePayload(); err == nil && p != nil {
			view.Payload = p
		}
	default:
		if len(e.Payload) > 0 {
			var raw any
			if json.Unmarshal(e.Payload, &raw) == nil {
				view.Payload = raw
			}
		}
	}

	return view
}
