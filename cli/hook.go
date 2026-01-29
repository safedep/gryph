package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/cursor"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
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

			// Parse event - parser extracts session_id and converts to deterministic UUID
			event, err := adapter.ParseEvent(ctx, hookType, rawData)
			if err != nil {
				return fmt.Errorf("failed to parse event: %w", err)
			}

			// Get or create session using the session ID from the parsed event
			sess, err := app.Store.GetSession(ctx, event.SessionID)
			if err != nil {
				return fmt.Errorf("failed to get session: %w", err)
			}

			if sess == nil {
				// Create new session with the ID from the event
				sess = session.NewSessionWithID(event.SessionID, agentName)
				sess.AgentSessionID = event.AgentSessionID // Store original agent session ID for correlation
				sess.WorkingDirectory = event.WorkingDirectory
				sess.ProjectName = detectProjectName(event.WorkingDirectory)
				if err := app.Store.SaveSession(ctx, sess); err != nil {
					return fmt.Errorf("failed to save session: %w", err)
				}
			}

			// Set sequence number
			event.Sequence = sess.TotalActions + 1

			// Save event
			if err := app.Store.SaveEvent(ctx, event); err != nil {
				return fmt.Errorf("failed to save event: %w", err)
			}

			// Update session counts
			sess.TotalActions++
			switch event.ActionType {
			case events.ActionFileRead:
				sess.FilesRead++
			case events.ActionFileWrite:
				sess.FilesWritten++
			case events.ActionCommandExec:
				sess.CommandsExecuted++
			}
			if event.ResultStatus == events.ResultError {
				sess.Errors++
			}
			if err := app.Store.UpdateSession(ctx, sess); err != nil {
				return fmt.Errorf("failed to update session: %w", err)
			}

			// Handle session end events
			if (agentName == agent.AgentCursor && hookType == "stop") ||
				(agentName == agent.AgentClaudeCode && hookType == "SessionEnd") {
				sess.End()
				if err := app.Store.UpdateSession(ctx, sess); err != nil {
					return fmt.Errorf("failed to end session: %w", err)
				}
			}

			// For Cursor hooks, output allow response
			if agentName == agent.AgentCursor {
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

// detectProjectName returns the project name from the working directory basename.
func detectProjectName(workDir string) string {
	if workDir == "" {
		return ""
	}
	return filepath.Base(workDir)
}
