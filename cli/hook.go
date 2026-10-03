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
	"github.com/safedep/gryph/hookside"
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

			// The payload comes first: a client needs it before it talks
			// to the service, and nothing else needs the store yet.
			rawData, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}

			cfg, err := config.Load(globalFlags.ConfigPath)
			if err != nil {
				log.Warnf("config: %v. Gryph uses the default config.", err)
				cfg = config.Default()
			}
			if clientMode(cfg) {
				return runClientHook(ctx, cfg, agentName, hookType, rawData)
			}

			app, err := NewApp(cfg)
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

			svc := app.DecisionService()
			hookErr := runHook(ctx, app.Registry, svc, agentName, hookType, rawData)
			if hookErr != nil && !isExitError(hookErr) {
				report := &decision.HookError{
					Agent:    agentName,
					HookType: hookType,
					RawSize:  len(rawData),
					RawEvent: rawData,
					Message:  hookErr.Error(),
				}
				if err := svc.ReportHookError(ctx, report); err != nil {
					log.Errorf("failed to log hook error: %v", err)
				}
			}

			return hookErr
		},
	}

	return cmd
}

// runHook parses the agent payload, gets a decision from the decision
// service, and renders the response.
func runHook(ctx context.Context, registry *agent.Registry, svc decision.Service, agentName, hookType string, rawData []byte) error {
	adapter, ok := registry.Get(agentName)
	if !ok {
		return fmt.Errorf("unknown agent: %s", agentName)
	}

	event, err := adapter.ParseEvent(ctx, hookType, rawData)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	resp, err := svc.Handle(ctx, hookside.NewRequest(ctx, event))
	if err != nil {
		return err
	}

	hookDecision, detail := renderDecision(resp)
	return sendResponse(adapter, hookType, hookDecision, detail)
}

// renderDecision maps a service response to the agent decision. A decision
// that this binary does not know comes from a newer service, and a missing
// decision comes from a broken one. Both block.
func renderDecision(resp *decision.HookResponse) (agent.HookDecision, string) {
	if resp.Decision == "" {
		return agent.DecisionBlock, "gryph: the decision service returned no decision"
	}
	d, ok := resp.Decision.Decision()
	if !ok {
		return agent.DecisionBlock, fmt.Sprintf("gryph: unknown decision %q", string(resp.Decision))
	}

	switch d {
	case security.DecisionAllow:
		return agent.DecisionAllow, ""
	case security.DecisionGuidance:
		return agent.DecisionGuidance, resp.Guidance
	default:
		return agent.DecisionBlock, resp.Reason
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
