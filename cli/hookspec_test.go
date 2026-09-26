package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/openclaw"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	coverageDocPath  = "../docs/agent-enforcement-coverage.md"
	coverageDocBegin = "<!-- BEGIN generated hook table: go test ./cli -run TestEnforcementCoverageDoc -->"
	coverageDocEnd   = "<!-- END generated hook table -->"
)

// coverageAdapters returns every registered adapter sorted by display name,
// then the inactive OpenClaw adapter, which the doc lists as planned.
func coverageAdapters() []agent.Adapter {
	registry := agent.NewRegistry()
	registerAdapters(registry, nil, config.Default())
	adapters := registry.All()
	sort.Slice(adapters, func(i, j int) bool { return adapters[i].DisplayName() < adapters[j].DisplayName() })
	return append(adapters, openclaw.New(nil, config.LoggingStandard, false))
}

func TestAdapterHooks_Valid(t *testing.T) {
	for _, a := range coverageAdapters() {
		t.Run(a.Name(), func(t *testing.T) {
			hooks := a.Hooks()
			require.NotEmpty(t, hooks)
			seen := map[events.HookType]bool{}
			for _, h := range hooks {
				assert.NotEmpty(t, h.Type)
				assert.False(t, seen[h.Type], "duplicate hook %q", h.Type)
				seen[h.Type] = true
				assert.Contains(t, []events.Phase{events.PhasePre, events.PhasePost, events.PhaseUnknown}, h.Phase, "hook %q", h.Type)
				if h.Blocking {
					assert.Equal(t, events.PhasePre, h.Phase, "a blocking hook %q must be a pre hook", h.Type)
				}
			}
		})
	}
}

// TestAdapterHooks_Phases pins the phase of some hooks. The generated table
// in TestEnforcementCoverageDoc pins every hook.
func TestAdapterHooks_Phases(t *testing.T) {
	registry := agent.NewRegistry()
	registerAdapters(registry, nil, config.Default())
	registry.Register(openclaw.New(nil, config.LoggingStandard, false))

	cases := []struct {
		agent string
		hook  events.HookType
		want  events.Phase
	}{
		{agent.AgentClaudeCode, "PreToolUse", events.PhasePre},
		{agent.AgentClaudeCode, "PostToolUse", events.PhasePost},
		{agent.AgentClaudeCode, "PostToolUseFailure", events.PhasePost},
		{agent.AgentClaudeCode, "SessionStart", events.PhaseUnknown},
		{agent.AgentClaudeCode, "Notification", events.PhaseUnknown},
		{agent.AgentCursor, "beforeShellExecution", events.PhasePre},
		{agent.AgentCursor, "afterFileEdit", events.PhasePost},
		{agent.AgentWindsurf, "pre_run_command", events.PhasePre},
		{agent.AgentWindsurf, "post_write_code", events.PhasePost},
		{agent.AgentOpenCode, "tool.execute.before", events.PhasePre},
		{agent.AgentOpenCode, "tool.execute.after", events.PhasePost},
		{agent.AgentPiAgent, "tool_call", events.PhasePre},
		{agent.AgentPiAgent, "tool_result", events.PhasePost},
		{agent.AgentGemini, "BeforeTool", events.PhasePre},
		{agent.AgentGemini, "AfterTool", events.PhasePost},
		{agent.AgentOpenClaw, "before_tool_call", events.PhasePre},
		{agent.AgentOpenClaw, "after_tool_call", events.PhasePost},
	}
	for _, tc := range cases {
		spec, ok := registry.HookSpec(tc.agent, tc.hook)
		require.Truef(t, ok, "%s does not declare %q", tc.agent, tc.hook)
		assert.Equalf(t, tc.want, spec.Phase, "%s %q", tc.agent, tc.hook)
	}

	_, ok := registry.HookSpec(agent.AgentClaudeCode, "NotAHook")
	assert.False(t, ok)
	_, ok = registry.HookSpec("no-such-agent", "PreToolUse")
	assert.False(t, ok)
}

func renderCoverageTable() string {
	var b strings.Builder
	b.WriteString("| Agent | Blocking pre-execution hooks | Post-execution hooks (detection) | Other hooks |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, a := range coverageAdapters() {
		var pre, post, other []string
		for _, h := range a.Hooks() {
			name := "`" + string(h.Type) + "`"
			if h.Prompt {
				name += " (prompt)"
			}
			switch {
			case h.Phase == events.PhasePre && h.Blocking:
				pre = append(pre, name)
			case h.Phase == events.PhasePost:
				post = append(post, name)
			default:
				other = append(other, name)
			}
		}
		display := a.DisplayName()
		if a.Name() == agent.AgentOpenClaw {
			display += " (inactive)"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", display, joinOrNone(pre), joinOrNone(post), joinOrNone(other))
	}
	return b.String()
}

func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// TestEnforcementCoverageDoc keeps the table in
// docs/agent-enforcement-coverage.md equal to the adapters' Hooks() tables.
// Run with GRYPH_UPDATE_DOCS=1 to rewrite the table.
func TestEnforcementCoverageDoc(t *testing.T) {
	data, err := os.ReadFile(coverageDocPath)
	require.NoError(t, err)
	doc := string(data)

	begin := strings.Index(doc, coverageDocBegin)
	end := strings.Index(doc, coverageDocEnd)
	require.True(t, begin >= 0 && end > begin, "the doc must hold the generated table markers")

	want := coverageDocBegin + "\n\n" + renderCoverageTable() + "\n" + coverageDocEnd
	got := doc[begin : end+len(coverageDocEnd)]

	if os.Getenv("GRYPH_UPDATE_DOCS") == "1" {
		require.NoError(t, os.WriteFile(coverageDocPath, []byte(doc[:begin]+want+doc[end+len(coverageDocEnd):]), 0o644))
		return
	}
	assert.Equal(t, want, got, "run GRYPH_UPDATE_DOCS=1 go test ./cli -run TestEnforcementCoverageDoc")
}

func TestAdapters_ParseToolCallID(t *testing.T) {
	registry := agent.NewRegistry()
	registerAdapters(registry, nil, config.Default())

	cases := []struct {
		agent   string
		hook    string
		fixture string
		want    string
	}{
		{agent.AgentClaudeCode, "PreToolUse", "../agent/claudecode/testdata/pre_tool_use_bash.json", "tool-use-001"},
		{agent.AgentClaudeCode, "PostToolUse", "../agent/claudecode/testdata/post_tool_use_read.json", "tool-use-003"},
		{agent.AgentClaudeCode, "PostToolUseFailure", "../agent/claudecode/testdata/post_tool_use_failure.json", "tool-use-004"},
		{agent.AgentCodex, "PreToolUse", "../agent/codex/testdata/pre_tool_use_bash.json", "tool-001"},
		{agent.AgentCodex, "PostToolUse", "../agent/codex/testdata/post_tool_use_bash.json", "tool-001"},
		{agent.AgentCommandCode, "PreToolUse", "../agent/commandcode/testdata/pre_tool_use_shell.json", "toolu_01"},
		{agent.AgentCommandCode, "PostToolUse", "../agent/commandcode/testdata/post_tool_use_edit.json", "toolu_04"},
		{agent.AgentCursor, "preToolUse", "../agent/cursor/testdata/pre_tool_use_shell.json", "tool-use-001"},
		{agent.AgentCursor, "postToolUse", "../agent/cursor/testdata/post_tool_use_read.json", "tool-use-003"},
		{agent.AgentCursor, "postToolUseFailure", "../agent/cursor/testdata/post_tool_use_failure.json", "tool-use-004"},
		{agent.AgentDevin, "PreToolUse", "../agent/devin/testdata/pre_tool_use_exec.json", "devin-call-1"},
		{agent.AgentDevin, "PostToolUse", "../agent/devin/testdata/post_tool_use_exec.json", "devin-call-1"},
		{agent.AgentOpenCode, "tool.execute.before", "../agent/opencode/testdata/tool_execute_before_bash.json", "call-opencode-1"},
		{agent.AgentOpenCode, "tool.execute.after", "../agent/opencode/testdata/tool_execute_after_read.json", "call-opencode-1"},
		{agent.AgentPiAgent, "tool_call", "../agent/piagent/testdata/tool_call_bash.json", "call-789"},
		{agent.AgentPiAgent, "tool_result", "../agent/piagent/testdata/tool_result_error.json", "call-789"},
	}
	for _, tc := range cases {
		t.Run(tc.agent+"/"+tc.hook, func(t *testing.T) {
			data, err := os.ReadFile(tc.fixture)
			require.NoError(t, err)
			adapter, ok := registry.Get(tc.agent)
			require.True(t, ok)

			event, err := adapter.ParseEvent(context.Background(), tc.hook, data)
			require.NoError(t, err)
			assert.Equal(t, tc.want, event.ToolCallID)
			assert.Equal(t, events.HookType(tc.hook), event.HookType)
		})
	}
}
