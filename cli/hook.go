package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/agent/codex"
	"github.com/safedep/gryph/agent/cursor"
	"github.com/safedep/gryph/agent/gemini"
	"github.com/safedep/gryph/agent/openclaw"
	"github.com/safedep/gryph/agent/opencode"
	"github.com/safedep/gryph/agent/piagent"
	"github.com/safedep/gryph/agent/windsurf"
	"github.com/safedep/gryph/config"
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

			if err := app.InitStore(ctx); err != nil {
				return ErrDatabase("failed to open database", err)
			}

			defer func() {
				err := app.Close()
				if err != nil {
					log.Errorf("failed to close app: %v", err)
				}
			}()

			rawData, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}

			hookErr := runHook(ctx, app, agentName, hookType, rawData)
			if hookErr != nil && !isExitError(hookErr) {
				logHookError(ctx, app, agentName, hookType, len(rawData), rawData, hookErr)
			}

			return hookErr
		},
	}

	return cmd
}

// runHook executes the core hook logic.
func runHook(ctx context.Context, app *App, agentName, hookType string, rawData []byte) error {
	adapter, ok := app.Registry.Get(agentName)
	if !ok {
		return fmt.Errorf("unknown agent: %s", agentName)
	}

	event, err := adapter.ParseEvent(ctx, hookType, rawData)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Order matters: redact configured patterns before the level filter strips
	// fields, so we never persist or log unredacted user content.
	agent.RedactEvent(event, app.PrivacyChecker)
	agent.ApplyLoggingLevel(event, app.Config.GetAgentLoggingLevel(agentName))

	sess, err := app.Store.GetSession(ctx, event.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	if sess == nil {
		sess = session.NewSessionWithID(event.SessionID, agentName)
		sess.AgentSessionID = event.AgentSessionID
		sess.WorkingDirectory = event.WorkingDirectory
		sess.TranscriptPath = event.TranscriptPath

		if event.WorkingDirectory != "" {
			if info, err := projectdetection.DetectProject(event.WorkingDirectory); err == nil && info != nil && info.Name != "" {
				sess.ProjectName = info.Name
			} else {
				sess.ProjectName = filepath.Base(event.WorkingDirectory)
			}
		}

		if err := app.Store.SaveSession(ctx, sess); err != nil {
			existing, getErr := app.Store.GetSession(ctx, event.SessionID)
			if getErr != nil || existing == nil {
				return fmt.Errorf("failed to save session: %w", err)
			}

			sess = existing
		}
	}

	if sess.TranscriptPath == "" && event.TranscriptPath != "" {
		sess.TranscriptPath = event.TranscriptPath
	}

	securityResult := app.Security.Evaluate(session.WithSession(ctx, sess), event)
	if !securityResult.IsAllowed() {
		event.ResultStatus = events.ResultBlocked
		event.ErrorMessage = securityResult.BlockReason
		event.Sequence = sess.TotalActions + 1

		if err := app.Store.SaveEvent(ctx, event); err != nil {
			log.Errorf("failed to save blocked event: %v", err)
		}

		sess.TotalActions++
		sess.BlockedActions++
		if event.IsSensitive {
			sess.SensitiveActions++
		}

		if err := app.Store.UpdateSession(ctx, sess); err != nil {
			log.Errorf("failed to update session for blocked event: %v", err)
		}

		return sendSecurityBlockedResponse(agentName, hookType, securityResult)
	}

	event.Sequence = sess.TotalActions + 1

	if err := app.Store.SaveEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}

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

	if event.IsSensitive {
		sess.SensitiveActions++
	}

	if err := app.Store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	if event.ActionType == events.ActionSessionEnd {
		sess.End()
		collectSessionCost(sess)
		if err := app.Store.UpdateSession(ctx, sess); err != nil {
			return fmt.Errorf("failed to end session: %w", err)
		}
	}

	if securityResult.FinalDecision == security.DecisionGuidance {
		return sendSecurityGuidanceResponse(agentName, hookType, securityResult)
	}
	return sendHookResponse(agentName, hookType)
}

// logHookError logs a self-audit entry when hook processing fails.
func logHookError(ctx context.Context, app *App, agentName, hookType string, rawDataSize int, rawData []byte, hookErr error) {
	if app.Store == nil {
		return
	}

	details := map[string]interface{}{
		"hook_type":     hookType,
		"raw_data_size": rawDataSize,
	}

	loggingLevel := app.Config.GetAgentLoggingLevel(agentName)
	if loggingLevel.IsAtLeast(config.LoggingFull) {
		const maxRawEventSize = 64 * 1024
		rawEvent := string(rawData)
		if len(rawEvent) > maxRawEventSize {
			rawEvent = rawEvent[:maxRawEventSize]
		}

		if app.PrivacyChecker != nil {
			rawEvent = app.PrivacyChecker.Redact(rawEvent)
		}

		details["raw_event"] = rawEvent
	}

	if err := logSelfAudit(ctx, app.Store, SelfAuditActionHookError,
		agentName, details, SelfAuditResultError, hookErr.Error()); err != nil {
		log.Errorf("failed to log hook error: %v", err)
	}
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
		// Block/guidance paths are handled upstream by the security evaluator
		// (see runHook); this is the allow path.
		response := claudecode.NewAllowResponse()
		return handleClaudeCodeResponse(response)

	case agent.AgentCursor:
		// Cursor: JSON response to stdout
		// Different hooks have different response schemas
		response := generateCursorResponse(hookType)
		if _, err := os.Stdout.Write(response); err != nil {
			log.Errorf("failed to write to stdout: %v", err)
		}

		return nil

	case agent.AgentGemini:
		// Gemini CLI: same exit code semantics as Claude Code (0=allow, 2=block, 1=error)
		// BeforeTool: JSON response to stdout
		// Other hooks: empty JSON to stdout
		if hookType == "BeforeTool" {
			resp := gemini.NewAllowResponse()
			if _, err := os.Stdout.Write(resp.JSON()); err != nil {
				log.Errorf("failed to write to stdout: %v", err)
			}
		} else {
			if _, err := os.Stdout.Write([]byte("{}")); err != nil {
				log.Errorf("failed to write to stdout: %v", err)
			}
		}
		return nil

	case agent.AgentOpenCode:
		// OpenCode: exit code semantics (0=allow, 2=block via thrown Error in JS shim)
		// Only tool.execute.before needs a blocking response
		if hookType == "tool.execute.before" {
			resp := opencode.NewAllowResponse()
			return handleOpenCodeResponse(resp)
		}
		return nil

	case agent.AgentOpenClaw:
		// OpenClaw: exit code semantics (0=allow, 2=block via thrown Error in TS plugin)
		// Only before_tool_call needs a blocking response
		if hookType == "before_tool_call" {
			resp := openclaw.NewAllowResponse()
			return handleOpenClawResponse(resp)
		}
		return nil

	case agent.AgentWindsurf:
		// Windsurf: exit code semantics (0=allow, 2=block for pre-hooks)
		// Pre-hooks can block via exit code 2; post-hooks just exit 0
		return handleWindsurfResponse(windsurf.NewAllowResponse())

	case agent.AgentPiAgent:
		// Pi Agent: exit code semantics (0=allow, 2=block, 1=error)
		// tool_call hooks can block via exit code 2
		if hookType == "tool_call" {
			resp := piagent.NewAllowResponse()
			return handlePiAgentResponse(resp)
		}
		return nil

	case agent.AgentCodex:
		// Codex: PreToolUse supports deny via JSON on stdout or exit code 2.
		// Allow is signaled by exit 0 with no output (Codex does not yet
		// support the "allow" permissionDecision value).
		return nil

	default:
		// Unknown agent, just succeed
		return nil
	}
}

// guidanceResponse is implemented by per-agent guidance response types that
// expose both a JSON channel and a stderr fallback.
type guidanceResponse interface {
	JSON() []byte
	Stderr() string
}

// emitGuidanceResponse writes the JSON response when the current hook type
// supports a JSON advisory channel, otherwise surfaces the advisory text on
// stderr at exit 0.
func emitGuidanceResponse(hookType, jsonHookType string, r guidanceResponse) error {
	if jsonHookType != "" && hookType == jsonHookType {
		if _, err := os.Stdout.Write(r.JSON()); err != nil {
			log.Errorf("failed to write to stdout: %v", err)
		}
		return nil
	}
	if msg := r.Stderr(); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}
	return nil
}

// sendSecurityGuidanceResponse emits advisory guidance for non-blocking decisions.
func sendSecurityGuidanceResponse(agentName, hookType string, result *security.Result) error {
	guidance := result.AggregatedGuidance()

	switch agentName {
	case agent.AgentClaudeCode:
		return handleClaudeCodeResponse(claudecode.NewGuidanceResponse(guidance))

	case agent.AgentCursor:
		response := cursor.NewGuidanceResponse(guidance)
		output := generateCursorDecisionResponse(hookType, response, true)
		if _, err := os.Stdout.Write(output); err != nil {
			log.Errorf("failed to write to stdout: %v", err)
		}
		// preToolUse JSON carries decision=allow with no reason field, so route advisory text to stderr.
		if hookType == "preToolUse" && response.Reason != "" {
			fmt.Fprintln(os.Stderr, response.Reason)
		}
		return nil

	case agent.AgentGemini:
		return emitGuidanceResponse(hookType, "BeforeTool", gemini.NewGuidanceResponse(guidance))

	case agent.AgentOpenCode:
		return emitGuidanceResponse(hookType, "tool.execute.before", opencode.NewGuidanceResponse(guidance))

	case agent.AgentOpenClaw:
		return emitGuidanceResponse(hookType, "before_tool_call", openclaw.NewGuidanceResponse(guidance))

	case agent.AgentWindsurf:
		// Windsurf has no JSON advisory channel, fall through to stderr.
		response := windsurf.NewGuidanceResponse(guidance)
		if msg := response.Stderr(); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		return nil

	case agent.AgentPiAgent:
		return emitGuidanceResponse(hookType, "tool_call", piagent.NewGuidanceResponse(guidance))

	case agent.AgentCodex:
		return emitGuidanceResponse(hookType, "PreToolUse", codex.NewGuidanceResponse(guidance))

	default:
		return sendHookResponse(agentName, hookType)
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
		output := generateCursorDecisionResponse(hookType, denyResponse, false)
		if _, err := os.Stdout.Write(output); err != nil {
			log.Errorf("failed to write to stdout: %v", err)
		}

		return nil

	case agent.AgentGemini:
		response := gemini.NewBlockResponse(result.BlockReason)
		if hookType == "BeforeTool" {
			if _, err := os.Stdout.Write(response.JSON()); err != nil {
				log.Errorf("failed to write to stdout: %v", err)
			}
		}
		return handleGeminiResponse(response)

	case agent.AgentOpenCode:
		response := opencode.NewBlockResponse(result.BlockReason)
		if hookType == "tool.execute.before" {
			if _, err := os.Stdout.Write(response.JSON()); err != nil {
				log.Errorf("failed to write to stdout: %v", err)
			}
		}
		return handleOpenCodeResponse(response)

	case agent.AgentOpenClaw:
		response := openclaw.NewBlockResponse(result.BlockReason)
		if hookType == "before_tool_call" {
			if _, err := os.Stdout.Write(response.JSON()); err != nil {
				log.Errorf("failed to write to stdout: %v", err)
			}
		}
		return handleOpenClawResponse(response)

	case agent.AgentWindsurf:
		response := windsurf.NewBlockResponse(result.BlockReason)
		return handleWindsurfResponse(response)

	case agent.AgentPiAgent:
		response := piagent.NewBlockResponse(result.BlockReason)
		if hookType == "tool_call" {
			if _, err := os.Stdout.Write(response.JSON()); err != nil {
				log.Errorf("failed to write to stdout: %v", err)
			}
		}
		return handlePiAgentResponse(response)

	case agent.AgentCodex:
		response := codex.NewBlockResponse(result.BlockReason)
		if hookType == "PreToolUse" {
			if _, err := os.Stdout.Write(response.JSON()); err != nil {
				log.Errorf("failed to write to stdout: %v", err)
			}
		}
		return handleCodexResponse(response)

	default:
		return nil
	}
}

// generateCursorDecisionResponse builds the JSON response for a Cursor hook
// type carrying an explicit block (cont=false) or allow-with-advisory
// (cont=true) decision.
func generateCursorDecisionResponse(hookType string, response *cursor.HookResponse, cont bool) []byte {
	switch hookType {
	case "preToolUse":
		return cursor.GeneratePreToolUseResponse(response)

	case "beforeShellExecution", "beforeMCPExecution", "beforeReadFile", "beforeTabFileRead":
		return cursor.GeneratePermissionResponse(response)

	case "beforeSubmitPrompt", "sessionStart":
		return cursor.GenerateContinueResponse(cont, response.Reason)

	default:
		return []byte("{}")
	}
}

func generateCursorResponse(hookType string) []byte {
	allowResponse := cursor.NewAllowResponse()

	switch hookType {
	case "preToolUse":
		return cursor.GeneratePreToolUseResponse(allowResponse)

	case "beforeShellExecution", "beforeMCPExecution", "beforeReadFile", "beforeTabFileRead":
		return cursor.GeneratePermissionResponse(allowResponse)

	case "beforeSubmitPrompt", "sessionStart":
		return cursor.GenerateContinueResponse(true, "")

	case "stop", "subagentStop":
		return cursor.GenerateStopResponse("")

	case "postToolUse", "postToolUseFailure", "afterFileEdit", "afterTabFileEdit",
		"afterShellExecution", "afterMCPExecution", "afterAgentThought",
		"afterAgentResponse", "sessionEnd", "subagentStart", "preCompact":
		return []byte("{}")

	default:
		return []byte("{}")
	}
}

// handleOpenClawResponse processes an OpenClaw hook response.
func handleOpenClawResponse(response *openclaw.HookResponse) error {
	switch response.Decision {
	case openclaw.HookBlock:
		return &exitError{code: 2, message: response.Message}
	case openclaw.HookError:
		return &exitError{code: 1, message: response.Message}
	default:
		return nil
	}
}

// handleWindsurfResponse processes a Windsurf hook response.
func handleWindsurfResponse(response *windsurf.HookResponse) error {
	switch response.Decision {
	case windsurf.HookBlock:
		return &exitError{code: 2, message: response.Message}
	case windsurf.HookError:
		return &exitError{code: 1, message: response.Message}
	default:
		return nil
	}
}

// handleOpenCodeResponse processes an OpenCode hook response.
func handleOpenCodeResponse(response *opencode.HookResponse) error {
	switch response.Decision {
	case opencode.HookBlock:
		return &exitError{code: 2, message: response.Message}
	case opencode.HookError:
		return &exitError{code: 1, message: response.Message}
	default:
		return nil
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

// handlePiAgentResponse processes a Pi Agent hook response.
func handlePiAgentResponse(response *piagent.HookResponse) error {
	switch response.Decision {
	case piagent.HookBlock:
		return &exitError{code: 2, message: response.Message}
	case piagent.HookError:
		return &exitError{code: 1, message: response.Message}
	default:
		return nil
	}
}

// handleCodexResponse processes a Codex hook response.
func handleCodexResponse(response *codex.HookResponse) error {
	switch response.Decision {
	case codex.HookBlock:
		return &exitError{code: 2, message: response.Message}
	case codex.HookError:
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

	case claudecode.HookGuidance:
		// Exit code 0 (allow), but aggregated guidance is written to stderr
		// for the user. Matches the evaluator's three-valued decision model.
		if response.Message != "" {
			fmt.Fprintln(os.Stderr, response.Message)
		}
		return nil

	default:
		// Exit code 0: allow
		return nil
	}
}

// isExitError returns true if the error is an intentional exit code signal
// (e.g. security blocks), not a hook processing failure.
func isExitError(err error) bool {
	var e *exitError
	return errors.As(err, &e)
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
