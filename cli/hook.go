package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/decision"
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

// runHook parses the agent payload, gets a decision from the decision
// service, and renders the response.
func runHook(ctx context.Context, app *App, agentName, hookType string, rawData []byte) error {
	adapter, ok := app.Registry.Get(agentName)
	if !ok {
		return fmt.Errorf("unknown agent: %s", agentName)
	}

	event, err := adapter.ParseEvent(ctx, hookType, rawData)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	resp, err := app.DecisionService().Handle(ctx, decision.NewHookRequest(agentName, event))
	if err != nil {
		return err
	}

	switch resp.Decision {
	case security.DecisionAllow:
		return sendResponse(adapter, hookType, agent.DecisionAllow, "")
	case security.DecisionBlock:
		return sendResponse(adapter, hookType, agent.DecisionBlock, resp.Reason)
	case security.DecisionGuidance:
		return sendResponse(adapter, hookType, agent.DecisionGuidance, resp.Guidance)
	default:
		// A decision this binary does not know comes from a newer service.
		// Fail closed.
		return sendResponse(adapter, hookType, agent.DecisionBlock,
			fmt.Sprintf("gryph: unknown decision %q", resp.Decision.String()))
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
