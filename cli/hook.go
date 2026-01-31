package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/agent/cursor"
	"github.com/safedep/gryph/agent/gemini"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/utils/projectdetection"
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
				return ErrDatabase("failed to open database", err)
			}

			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %w", err)
				}
			}()

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

			// Apply logging level filtering before saving
			loggingLevel := app.Config.GetAgentLoggingLevel(agentName)
			agent.ApplyLoggingLevel(event, loggingLevel)

			// Evaluate security checks
			securityResult := app.Security.Evaluate(ctx, event)
			if !securityResult.IsAllowed() {
				return sendSecurityBlockedResponse(agentName, hookType, securityResult)
			}

			// Get or create session using the session ID from the parsed event
			sess, err := app.Store.GetSession(ctx, event.SessionID)
			if err != nil {
				return fmt.Errorf("failed to get session: %w", err)
			}

			if sess == nil {
				// Create new session with the ID from the event
				sess = session.NewSessionWithID(event.SessionID, agentName)
				sess.AgentSessionID = event.AgentSessionID
				sess.WorkingDirectory = event.WorkingDirectory

				if event.WorkingDirectory != "" {
					if info, err := projectdetection.DetectProject(event.WorkingDirectory); err == nil && info != nil && info.Name != "" {
						sess.ProjectName = info.Name
					} else {
						sess.ProjectName = filepath.Base(event.WorkingDirectory)
					}
				}

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
			if event.ActionType == events.ActionSessionEnd {
				sess.End()
				if err := app.Store.UpdateSession(ctx, sess); err != nil {
					return fmt.Errorf("failed to end session: %w", err)
				}
			}

			// Send response to agent
			// For now, always allow. Future: add policy-based blocking here.
			return sendHookResponse(agentName, hookType)
		},
	}

	return cmd
}

// sendHookResponse sends the appropriate response to the agent.
// Returns nil for success (exit code 0), or an error that triggers non-zero exit.
func sendHookResponse(agentName, hookType string) error {
	switch agentName {
	case agent.AgentClaudeCode:
		// Claude Code exit codes:
		//   0 = allow (success)
		//   2 = block (blocking error, stderr shown to Claude)
		//   1 = non-blocking error (stderr shown to user in verbose mode)
		// For now, always allow. Future: add policy-based blocking here.
		response := claudecode.NewAllowResponse()
		return handleClaudeCodeResponse(response)

	case agent.AgentCursor:
		// Cursor: JSON response to stdout
		// Different hooks have different response schemas
		response := generateCursorResponse(hookType)
		if _, err := os.Stdout.Write(response); err != nil {
			log.Errorf("failed to write to stdout: %w", err)
		}

		return nil

	case agent.AgentGemini:
		// Gemini CLI: same exit code semantics as Claude Code (0=allow, 2=block, 1=error)
		// BeforeTool: JSON response to stdout
		// Other hooks: empty JSON to stdout
		if hookType == "BeforeTool" {
			resp := gemini.NewAllowResponse()
			if _, err := os.Stdout.Write(resp.JSON()); err != nil {
				log.Errorf("failed to write to stdout: %w", err)
			}
		} else {
			if _, err := os.Stdout.Write([]byte("{}")); err != nil {
				log.Errorf("failed to write to stdout: %w", err)
			}
		}
		return nil

	default:
		// Unknown agent, just succeed
		return nil
	}
}

// sendSecurityBlockedResponse sends a blocked response based on security evaluation.
func sendSecurityBlockedResponse(agentName, hookType string, result *security.Result) error {
	switch agentName {
	case agent.AgentClaudeCode:
		response := claudecode.NewBlockResponse(result.BlockReason)
		return handleClaudeCodeResponse(response)

	case agent.AgentCursor:
		denyResponse := cursor.NewDenyResponse(result.BlockReason)
		output := generateCursorBlockedResponse(hookType, denyResponse)
		if _, err := os.Stdout.Write(output); err != nil {
			log.Errorf("failed to write to stdout: %w", err)
		}

		return nil

	case agent.AgentGemini:
		response := gemini.NewBlockResponse(result.BlockReason)
		return handleGeminiResponse(response)

	default:
		return nil
	}
}

// generateCursorBlockedResponse generates the appropriate blocked response for a Cursor hook type.
func generateCursorBlockedResponse(hookType string, response *cursor.HookResponse) []byte {
	switch hookType {
	case "preToolUse":
		return cursor.GeneratePreToolUseResponse(response)

	case "beforeShellExecution", "beforeMCPExecution", "beforeReadFile", "beforeTabFileRead":
		return cursor.GeneratePermissionResponse(response)

	case "beforeSubmitPrompt", "sessionStart":
		return cursor.GenerateContinueResponse(false, response.Reason)

	default:
		// For hooks that don't support blocking, return empty JSON
		return []byte("{}")
	}
}

// generateCursorResponse generates the appropriate response for a Cursor hook type.
func generateCursorResponse(hookType string) []byte {
	// Create an allow response for all hooks (policy enforcement can be added later)
	allowResponse := cursor.NewAllowResponse()

	switch hookType {
	case "preToolUse":
		// preToolUse uses decision: allow/deny
		return cursor.GeneratePreToolUseResponse(allowResponse)

	case "beforeShellExecution", "beforeMCPExecution", "beforeReadFile", "beforeTabFileRead":
		// Permission hooks use permission: allow/deny/ask
		return cursor.GeneratePermissionResponse(allowResponse)

	case "beforeSubmitPrompt", "sessionStart":
		// Continue hooks use continue: true/false
		return cursor.GenerateContinueResponse(true, "")

	case "stop", "subagentStop":
		// Stop hooks can have optional followup_message
		return cursor.GenerateStopResponse("")

	case "postToolUse", "postToolUseFailure", "afterFileEdit", "afterTabFileEdit",
		"afterShellExecution", "afterMCPExecution", "afterAgentThought",
		"afterAgentResponse", "sessionEnd", "subagentStart", "preCompact":
		// Post-action hooks don't require specific responses, return empty JSON
		return []byte("{}")

	default:
		// Unknown hook type, return empty JSON
		return []byte("{}")
	}
}

// handleGeminiResponse processes a Gemini CLI hook response.
func handleGeminiResponse(response *gemini.HookResponse) error {
	switch response.Decision {
	case gemini.HookBlock:
		return &exitError{code: 2, message: response.Message}
	case gemini.HookError:
		return &exitError{code: 1, message: response.Message}
	default:
		return nil
	}
}

// handleClaudeCodeResponse processes a Claude Code hook response.
// Returns an exitError with the appropriate code and message for non-allow decisions.
func handleClaudeCodeResponse(response *claudecode.HookResponse) error {
	switch response.Decision {
	case claudecode.HookBlock:
		// Exit code 2: blocking error, message shown to Claude
		return &exitError{code: 2, message: response.Message}

	case claudecode.HookError:
		// Exit code 1: non-blocking error, message shown to user in verbose mode
		return &exitError{code: 1, message: response.Message}

	default:
		// Exit code 0: allow
		return nil
	}
}

// exitError is an error that carries a specific exit code.
// It implements the ExitCoder interface expected by main.
type exitError struct {
	code    int
	message string
}

// Validate that exitError implements the ExitCoder interface.
var _ ExitCoder = &exitError{}

func (e *exitError) Error() string {
	return e.message
}

// ExitCode returns the exit code for this error.
func (e *exitError) ExitCode() int {
	return e.code
}

// Message returns the message to write to stderr.
func (e *exitError) Message() string {
	return e.message
}
