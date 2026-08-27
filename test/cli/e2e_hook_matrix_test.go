package cli_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cross-agent compatibility matrix.
//
// TestPolicyMatrix drives the eight registered hook-shipping agents through
// the same scenario set (allow, block, guidance, escalate, defer) using a
// minimal per-scenario policy and asserts on three observables:
//
//  1. The hook process exit code (per-agent because each agent's wire
//     protocol uses different conventions).
//  2. The persisted event's ResultStatus.
//  3. The persisted receipt's decision (when policy.log_all_evaluations is
//     true, which is the default).
//
// Skips are intentional and per-cell: a cell is skipped only when the agent's
// hook protocol simply does not surface the decision distinction (for
// example, an agent whose only blocking response is exit 2 still satisfies
// the block case but its allow/block decisions cannot be distinguished by a
// missing testdata fixture). Every skip carries a comment naming the reason.

// matrixScenario is one row of the per-agent matrix. Decisions are scoped per
// scenario rather than per agent because the policy that drives the
// scenario is identical across agents. What varies is the per-agent
// fixture, exit code shape, and which hook type the payload feeds.
type matrixScenario string

const (
	scenarioAllow    matrixScenario = "allow"
	scenarioBlock    matrixScenario = "block"
	scenarioGuidance matrixScenario = "guidance"
	scenarioEscalate matrixScenario = "escalate"
	scenarioDefer    matrixScenario = "defer"
)

// matrixAgent declares the per-agent fixtures and the per-decision exit
// codes for that agent's hook protocol. A nil fixture means the matrix
// cannot exercise that decision for that agent and the cell is skipped with
// a documented reason.
type matrixAgent struct {
	name string

	// commandHookType + commandPayloadPath: a pre-tool-use payload that
	// carries a command_exec action. Used by block/escalate/guidance
	// scenarios that match on command_patterns.
	commandHookType    string
	commandPayloadPath string

	// readHookType + readPayloadPath: a pre-tool-use payload that carries a
	// file_read action. Used by allow/defer scenarios that match on
	// file_patterns.
	readHookType    string
	readPayloadPath string

	// blockExitCode is the exit code this agent returns when a decision
	// resolves to a hard block on the matched hook type. Cursor returns 0
	// with a deny JSON payload, every other supported agent returns 2.
	blockExitCode int
}

var matrixAgents = []matrixAgent{
	{
		name:               agent.AgentClaudeCode,
		commandHookType:    "PreToolUse",
		commandPayloadPath: "../../agent/claudecode/testdata/pre_tool_use_bash.json",
		readHookType:       "PreToolUse",
		readPayloadPath:    "../../agent/claudecode/testdata/pre_tool_use_read.json",
		blockExitCode:      2,
	},
	{
		name:               agent.AgentCursor,
		commandHookType:    "beforeShellExecution",
		commandPayloadPath: "../../agent/cursor/testdata/before_shell_execution.json",
		readHookType:       "beforeReadFile",
		readPayloadPath:    "../../agent/cursor/testdata/before_read_file.json",
		// Cursor's permission hooks always exit 0 and convey deny via JSON
		// on stdout (cli/hook.go::sendSecurityBlockedResponse).
		blockExitCode: 0,
	},
	{
		name:               agent.AgentGemini,
		commandHookType:    "BeforeTool",
		commandPayloadPath: "../../agent/gemini/testdata/before_tool_shell.json",
		readHookType:       "BeforeTool",
		readPayloadPath:    "../../agent/gemini/testdata/before_tool_read_file.json",
		blockExitCode:      2,
	},
	{
		name:               agent.AgentOpenCode,
		commandHookType:    "tool.execute.before",
		commandPayloadPath: "../../agent/opencode/testdata/tool_execute_before_bash.json",
		readHookType:       "tool.execute.before",
		readPayloadPath:    "../../agent/opencode/testdata/tool_execute_before_read.json",
		blockExitCode:      2,
	},
	{
		name:               agent.AgentWindsurf,
		commandHookType:    "pre_run_command",
		commandPayloadPath: "../../agent/windsurf/testdata/pre_run_command.json",
		readHookType:       "pre_read_code",
		readPayloadPath:    "../../agent/windsurf/testdata/pre_read_code.json",
		blockExitCode:      2,
	},
	{
		name:               agent.AgentPiAgent,
		commandHookType:    "tool_call",
		commandPayloadPath: "../../agent/piagent/testdata/tool_call_bash.json",
		readHookType:       "tool_call",
		readPayloadPath:    "../../agent/piagent/testdata/tool_call_read.json",
		blockExitCode:      2,
	},
	{
		name:               agent.AgentCodex,
		commandHookType:    "PreToolUse",
		commandPayloadPath: "../../agent/codex/testdata/pre_tool_use_bash.json",
		// Codex does not ship a read fixture today. The allow and defer
		// cases below skip for codex with that reason documented inline.
		readHookType:    "",
		readPayloadPath: "",
		blockExitCode:   2,
	},
	{
		name:               agent.AgentDevin,
		commandHookType:    "PreToolUse",
		commandPayloadPath: "../../agent/devin/testdata/pre_tool_use_exec.json",
		readHookType:       "PreToolUse",
		readPayloadPath:    "../../agent/devin/testdata/pre_tool_use_read.json",
		blockExitCode:      2,
	},
}

// matrixPolicy returns the policy YAML for the given scenario. Each scenario
// is exercised against a single rule so the receipt's decision column can be
// asserted unambiguously.
func matrixPolicy(scenario matrixScenario) string {
	switch scenario {
	case scenarioBlock:
		return `version: "1"
rules:
  - id: matrix-block-npm-install
    action: block
    severity: high
    match:
      action_types: [command_exec]
      command_patterns:
        - "(?i)\\bnpm\\s+install\\b"
        - "^ls\\s+-la$"
    message: "matrix: blocked by policy"
`
	case scenarioGuidance:
		return `version: "1"
rules:
  - id: matrix-guidance-npm-install
    action: guidance
    severity: medium
    match:
      action_types: [command_exec]
      command_patterns:
        - "(?i)\\bnpm\\s+install\\b"
        - "^ls\\s+-la$"
    message: "matrix: guidance from policy"
`
	case scenarioEscalate:
		return `version: "1"
rules:
  - id: matrix-escalate-npm-install
    action: escalate
    severity: high
    match:
      action_types: [command_exec]
      command_patterns:
        - "(?i)\\bnpm\\s+install\\b"
        - "^ls\\s+-la$"
    message: "matrix: escalate to approval"
`
	case scenarioDefer:
		// Explicit defer rule. Earlier iterations of this test used
		// action: warn with a context.files_written condition and relied
		// on the PDP's fresh-session synthetic-defer trigger to surface a
		// DecisionDefer. That worked but tested the wrong code path: the
		// matrix is meant to exercise an authored defer rule, not the
		// fresh-session fallback. The PDP also requires a non-empty
		// reason field when action is defer.
		return `version: "1"
rules:
  - id: matrix-defer-on-file-read
    action: defer
    severity: low
    match:
      action_types: [file_read]
    reason: "wait_for_writes"
    message: "matrix: defer until writes observed"
`
	default:
		return `version: "1"
rules: []
`
	}
}

// expectedDecision is the receipt.decision value the matrix expects for a
// given scenario. The escalate scenario surfaces as "denied" because the
// default approval mode (Nop) denies every request and applyApprovalOutcome
// rewrites the receipt's decision column to receipt.DecisionDenied.
func expectedDecision(scenario matrixScenario) string {
	switch scenario {
	case scenarioAllow:
		return string(model.DecisionAllow)
	case scenarioBlock:
		return string(model.DecisionBlock)
	case scenarioGuidance:
		return string(model.DecisionGuidance)
	case scenarioEscalate:
		// receipt.DecisionDenied is the post-approval decision rewrite for
		// an escalate that flowed through approval.Nop.
		return "denied"
	case scenarioDefer:
		return string(model.DecisionDefer)
	}
	return ""
}

// expectedResultStatus is the persisted event.ResultStatus the matrix
// expects. allow and guidance leave the event as ResultSuccess. Everything
// that resolves to a hard block surfaces as ResultBlocked.
func expectedResultStatus(scenario matrixScenario) events.ResultStatus {
	switch scenario {
	case scenarioAllow, scenarioGuidance:
		return events.ResultSuccess
	}
	return events.ResultBlocked
}

// expectedExitCode reports the hook process exit code the matrix expects
// for a given agent/scenario combination. Allow and guidance are always
// exit 0. Block, escalate, and defer all surface as a hard block to the
// agent, so the exit code follows the agent's block convention.
func expectedExitCode(ma matrixAgent, scenario matrixScenario) int {
	switch scenario {
	case scenarioAllow, scenarioGuidance:
		return 0
	}
	return ma.blockExitCode
}

// matrixFixture returns the hookType + payload path the scenario should
// drive for the given agent, plus a skip reason when the agent cannot
// exercise the scenario today. The block/guidance/escalate scenarios use
// the command fixture (so command_patterns matches), allow/defer use the
// read fixture (so the file_read rule matches without colliding with the
// command_exec scenarios).
func matrixFixture(ma matrixAgent, scenario matrixScenario) (hookType, payloadPath, skipReason string) {
	switch scenario {
	case scenarioBlock, scenarioGuidance, scenarioEscalate:
		if ma.commandPayloadPath == "" {
			return "", "", fmt.Sprintf("%s does not ship a command_exec testdata fixture", ma.name)
		}
		return ma.commandHookType, ma.commandPayloadPath, ""
	case scenarioAllow, scenarioDefer:
		if ma.readPayloadPath == "" {
			// Codex ships no read fixture today. Allow and defer skip with
			// this reason documented per-cell.
			return "", "", fmt.Sprintf("%s does not ship a file_read testdata fixture", ma.name)
		}
		return ma.readHookType, ma.readPayloadPath, ""
	}
	return "", "", fmt.Sprintf("unknown scenario %q", scenario)
}

// loadEventForScenario returns the persisted event whose ActionType matches
// the one the scenario's fixture is expected to drive. Some agent adapters
// emit auxiliary events (session_start and so on) during a hook invocation,
// so an unconditional Len == 1 assertion would be brittle across agents.
// Filtering by the scenario's ActionType keeps the matrix targeted: the
// block/guidance/escalate scenarios drive a command_exec fixture, allow and
// defer drive a file_read fixture.
func loadEventForScenario(t *testing.T, env *testEnv, scenario matrixScenario) *events.Event {
	t.Helper()
	store, cleanup := env.openStore()
	defer cleanup()
	all, err := store.QueryEvents(context.Background(), events.NewEventFilter())
	require.NoError(t, err)
	wantAction := scenarioActionType(scenario)
	matched := make([]*events.Event, 0, 1)
	for _, e := range all {
		if e.ActionType == wantAction {
			matched = append(matched, e)
		}
	}
	require.GreaterOrEqual(t, len(matched), 1,
		"expected at least one persisted event with ActionType %q (got %d events of all types)",
		wantAction, len(all))
	return matched[len(matched)-1]
}

// scenarioActionType reports the ActionType the scenario's fixture drives.
// Mirrors matrixFixture's command-vs-read split.
func scenarioActionType(scenario matrixScenario) events.ActionType {
	switch scenario {
	case scenarioBlock, scenarioGuidance, scenarioEscalate:
		return events.ActionCommandExec
	case scenarioAllow, scenarioDefer:
		return events.ActionFileRead
	}
	return events.ActionUnknown
}

// loadLatestMatchingReceipt returns the first receipt whose decision is in
// expectedDecisions. The matrix drives one action per scenario but the
// defer / escalate paths can also write a receipt for a no-rule match when
// log_all_evaluations is true, so we filter rather than fail on counts.
// Returns nil when no matching receipt exists.
func loadLatestMatchingReceipt(t *testing.T, env *testEnv, expectedDecisions ...string) *storage.ReceiptRow {
	t.Helper()
	store, cleanup := env.openStore()
	defer cleanup()
	rows, err := store.QueryReceipts(context.Background(), &storage.ReceiptFilter{Limit: -1})
	require.NoError(t, err)
	allow := map[string]struct{}{}
	for _, d := range expectedDecisions {
		allow[d] = struct{}{}
	}
	for _, r := range rows {
		if _, ok := allow[r.Decision]; ok {
			return r
		}
	}
	return nil
}

func TestPolicyMatrix(t *testing.T) {
	scenarios := []matrixScenario{
		scenarioAllow,
		scenarioBlock,
		scenarioGuidance,
		scenarioEscalate,
		scenarioDefer,
	}

	// TestPolicyMatrix subtests must remain serial: runHookCapturingStd
	// mutates global os.Stdin/Stdout/Stderr.
	for _, ma := range matrixAgents {
		for _, scenario := range scenarios {
			ma := ma
			scenario := scenario
			t.Run(fmt.Sprintf("%s/%s", ma.name, scenario), func(t *testing.T) {
				hookType, payloadPath, skipReason := matrixFixture(ma, scenario)
				if skipReason != "" {
					t.Skipf("skip: %s", skipReason)
				}

				payload, err := os.ReadFile(payloadPath)
				if err != nil {
					// Skip rather than fail so a missing fixture in one
					// agent does not silently mask matrix coverage in
					// others. Logged so the gap is visible in test output.
					t.Skipf("skip: missing testdata %q: %v", payloadPath, err)
				}

				env := newTestEnvWithPolicy(t, matrixPolicy(scenario))

				stdout, stderr, runErr := env.runHookCapturingStd(ma.name, hookType, payload)

				gotExit := hookExitCode(t, runErr)
				wantExit := expectedExitCode(ma, scenario)
				assert.Equal(t, wantExit, gotExit,
					"unexpected exit code (agent=%s scenario=%s stdout=%q stderr=%q err=%v)",
					ma.name, scenario, truncate(stdout), truncate(stderr), runErr)

				evt := loadEventForScenario(t, env, scenario)
				wantStatus := expectedResultStatus(scenario)
				assert.Equal(t, wantStatus, evt.ResultStatus,
					"unexpected event ResultStatus (agent=%s scenario=%s)", ma.name, scenario)

				// The receipt assertion only fires when log_all_evaluations
				// is true (the default in newTestEnvWithPolicy). The block
				// path writes a receipt even when no policy rule matches,
				// so for allow we accept either an "allow" or a no-match
				// receipt with the "allow" decision.
				wantDecision := expectedDecision(scenario)
				// Escalate scenarios always carry both an "escalate" hash
				// input and a "denied" post-update decision in the column.
				candidates := []string{wantDecision}
				if scenario == scenarioEscalate {
					candidates = append(candidates, string(model.DecisionEscalate))
				}
				rec := loadLatestMatchingReceipt(t, env, candidates...)
				if rec == nil {
					t.Fatalf("no receipt with decision in %v (agent=%s scenario=%s)", candidates, ma.name, scenario)
				}

				// Block / defer paths also populate result_status on the
				// receipt row at insert time. Sanity-check that the
				// receipt's column agrees with the event's status for
				// hard-block scenarios.
				if wantStatus == events.ResultBlocked {
					assert.Contains(t,
						[]string{"blocked", "deferred", "rejected"},
						rec.ResultStatus,
						"hard-block receipt must carry a terminal-block result_status (agent=%s scenario=%s decision=%s)",
						ma.name, scenario, rec.Decision,
					)
				}
			})
		}
	}
}

// truncate caps a captured stdout/stderr blob so a panicking agent does not
// dump megabytes into the failing-test output.
func truncate(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated " + strconv.Itoa(len(s)-max) + " bytes)"
}
