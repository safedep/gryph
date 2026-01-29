package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/safedep/gryph/agent/cursor"
	"github.com/spf13/cobra"
)

// NewHookCmd creates the internal _hook command.
func NewHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "_hook <agent> <type>",
		Short:  "Internal command invoked by agent hooks",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			agentName := args[0]
			hookType := args[1]

			app, err := loadApp()
			if err != nil {
				return err
			}

			// Initialize store
			if err := app.InitStore(ctx); err != nil {
				return err
			}
			defer app.Close()

			// Read event data from stdin
			rawData, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}

			// Get adapter
			adapter, ok := app.Registry.Get(agentName)
			if !ok {
				return fmt.Errorf("unknown agent: %s", agentName)
			}

			// Parse event
			event, err := adapter.ParseEvent(ctx, hookType, rawData)
			if err != nil {
				return fmt.Errorf("failed to parse event: %w", err)
			}

			// Save event
			if err := app.Store.SaveEvent(ctx, event); err != nil {
				return fmt.Errorf("failed to save event: %w", err)
			}

			// For Cursor hooks, output allow response
			if agentName == "cursor" {
				response := cursor.GenerateResponse(true, "")
				os.Stdout.Write(response)
			}

			return nil
		},
	}

	return cmd
}

// hookResponse is used for responding to agent hooks.
type hookResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// writeHookResponse writes a JSON response to stdout for agents that expect it.
func writeHookResponse(allow bool, message string) {
	resp := hookResponse{
		Status: "allow",
	}
	if !allow {
		resp.Status = "deny"
		resp.Message = message
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}
