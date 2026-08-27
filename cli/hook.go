package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/agent"
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

		return sendResponse(adapter, hookType, agent.DecisionBlock, securityResult.BlockReason)
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

	recordAllowedAarmResult(ctx, app, securityResult, event)

	if securityResult.FinalDecision == security.DecisionGuidance {
		return sendResponse(adapter, hookType, agent.DecisionGuidance, securityResult.AggregatedGuidance())
	}
	return sendResponse(adapter, hookType, agent.DecisionAllow, "")
}

// recordAllowedAarmResult fans out the post-hook execution outcome to the
// AARM Mediator on the allow path. The wrapper only sees the hook's own
// exit, so the recorded status is always ResultSuccess. Future post-hook
// adapters surfacing real execution outcomes will plug in here.
func recordAllowedAarmResult(ctx context.Context, app *App, result *security.Result, event *events.Event) {
	if app == nil || result == nil {
		return
	}
	med := app.AarmMediator()
	if med == nil {
		return
	}
	actionID, sessionID, sequence := result.AarmRef()
	if actionID == uuid.Nil && sequence == 0 {
		return
	}
	outcome := model.Result{Status: model.ResultSuccess}
	if event != nil && event.ResultStatus == events.ResultError {
		outcome.Status = model.ResultError
		outcome.Error = event.ErrorMessage
	}
	if err := med.RecordResult(ctx, actionID, sessionID, sequence, outcome); err != nil {
		log.Warnf("aarm: post-hook record result: %v", err)
	}
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

// sendResponse writes the adapter's rendered response. The stderr text
// travels on one channel only: inside the exit error for a non-zero exit,
// where main writes it, or directly for exit zero.
func sendResponse(a agent.Adapter, hookType string, decision agent.HookDecision, detail string) error {
	resp := a.RenderResponse(hookType, decision, detail)

	if out := resp.Stdout(); len(out) > 0 {
		if _, err := os.Stdout.Write(out); err != nil {
			log.Errorf("failed to write to stdout: %v", err)
		}
	}

	if code := resp.ExitCode(); code != 0 {
		return &exitError{code: code, message: resp.Stderr()}
	}

	if msg := resp.Stderr(); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}

	return nil
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
