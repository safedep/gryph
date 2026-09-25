package events

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimOrigin(t *testing.T) {
	event := func(at ActionType, tool string, payload any) *Event {
		e := NewEvent(uuid.New(), "test", at)
		e.ToolName = tool
		e.WorkingDirectory = "/work/project"
		if payload != nil {
			require.NoError(t, e.SetPayload(payload))
		}
		return e
	}
	cases := []struct {
		name       string
		event      *Event
		wantOrigin privacy.Origin
		wantSource string
	}{
		{"mcp tool", event(ActionToolUse, "mcp__github__create_issue", nil), privacy.OriginMCP, "github"},
		{"web fetch", event(ActionToolUse, "WebFetch", nil), privacy.OriginWeb, ""},
		{"gemini web search", event(ActionToolUse, "google_web_search", nil), privacy.OriginWeb, ""},
		{"browser tool", event(ActionToolUse, "browser_navigate", nil), privacy.OriginWeb, ""},
		{"network request", event(ActionNetworkRequest, "curl", nil), privacy.OriginWeb, ""},
		{"shell command", event(ActionCommandExec, "Bash", nil), privacy.OriginCommand, ""},
		{"read in the project", event(ActionFileRead, "Read", FileReadPayload{Path: "/work/project/main.go"}), privacy.OriginFileProject, ""},
		{"relative read", event(ActionFileRead, "Read", FileReadPayload{Path: "main.go"}), privacy.OriginFileProject, ""},
		{"read outside the project", event(ActionFileRead, "Read", FileReadPayload{Path: "/etc/hosts"}), privacy.OriginFileExternal, ""},
		{"read of a sibling directory", event(ActionFileRead, "Read", FileReadPayload{Path: "/work/project-other/a"}), privacy.OriginFileExternal, ""},
		{"relative read that leaves the project", event(ActionFileRead, "Read", FileReadPayload{Path: "../../etc/passwd"}), privacy.OriginFileExternal, ""},
		{"home read", event(ActionFileRead, "Read", FileReadPayload{Path: "~/.aws/credentials"}), privacy.OriginFileExternal, ""},
		{"read with no path", event(ActionFileRead, "Glob", FileReadPayload{Pattern: "**/*.go"}), privacy.OriginUnknown, ""},
		{"write", event(ActionFileWrite, "Write", nil), privacy.OriginAgent, ""},
		{"other tool", event(ActionToolUse, "TodoWrite", nil), privacy.OriginUnknown, ""},
		{"unlabeled prompt", event(ActionUserPrompt, "", nil), privacy.OriginUnknown, ""},
		{"session start", event(ActionSessionStart, "", nil), "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.event.ClaimOrigin()
			assert.Equal(t, tc.wantOrigin, tc.event.Origin)
			assert.Equal(t, tc.wantSource, tc.event.OriginSource)
		})
	}

	t.Run("read with no working directory", func(t *testing.T) {
		e := event(ActionFileRead, "Read", FileReadPayload{Path: "/etc/passwd"})
		e.WorkingDirectory = ""
		e.ClaimOrigin()
		assert.Equal(t, privacy.OriginUnknown, e.Origin)
	})

	t.Run("ambiguous mcp server", func(t *testing.T) {
		e := event(ActionToolUse, "mcp__github__x__get", nil)
		e.ClaimOrigin()
		assert.Equal(t, privacy.OriginMCP, e.Origin)
		assert.Empty(t, e.OriginSource, "a server that holds __ cannot pass as another server")
	})

	t.Run("adapter claim wins", func(t *testing.T) {
		e := event(ActionToolUse, "WebFetch", nil)
		e.Origin = privacy.OriginMCP
		e.ClaimOrigin()
		assert.Equal(t, privacy.OriginMCP, e.Origin)
	})
}

func TestSplitMCPTool(t *testing.T) {
	server, tool, ok := SplitMCPTool("mcp__linear__save_issue")
	assert.True(t, ok)
	assert.Equal(t, "linear", server)
	assert.Equal(t, "save_issue", tool)

	for _, name := range []string{"Read", "mcp__", "mcp____tool", "mcp__server"} {
		_, _, ok := SplitMCPTool(name)
		assert.False(t, ok, name)
	}
}

func TestObserveOutput(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		response any
		want     string
	}{
		{"string", "", "AKIA0000", "AKIA0000"},
		{"map in key order", "", map[string]any{"stdout": "b", "stderr": "a", "code": 1.0}, "a\nb"},
		{"nested", "", map[string]any{"file": map[string]any{"content": "secret"}}, "secret"},
		{"list", "", []any{"x", map[string]any{"text": "y"}}, "x\ny"},
		{"keeps write content", "written", "ok", "written"},
		{"nil", "", nil, ""},
		{"capped", "", []any{strings.Repeat("a", MaxObservedBytes), "b"}, strings.Repeat("a", MaxObservedBytes)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Event{FullContent: tc.existing}
			e.ObserveOutput(tc.response)
			assert.Equal(t, tc.want, e.FullContent)
		})
	}
}
