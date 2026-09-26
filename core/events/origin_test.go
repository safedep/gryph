package events

import (
	"strings"
	"testing"
	"unicode/utf8"

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
		{"mcp tool with __ in the tool part", event(ActionToolUse, "mcp__evil__read__file", nil), privacy.OriginMCP, ""},
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
	cases := []struct {
		name       string
		wantServer string
		wantTool   string
		wantOK     bool
	}{
		{"mcp__linear__save_issue", "linear", "save_issue", true},
		{"mcp__evil__read__file", "", "evil__read__file", true},
		{"Read", "", "", false},
		{"mcp__", "", "", false},
		{"mcp____tool", "", "", false},
		{"mcp__server", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, tool, ok := SplitMCPTool(tc.name)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantServer, server)
			assert.Equal(t, tc.wantTool, tool)
		})
	}
}

func TestMCPServers(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{"mcp__github__create_issue", []string{"github"}},
		{"mcp__evil__read__file", []string{"evil", "evil__read"}},
		{"mcp__a___b", []string{"a", "a_"}},
		{"mcp__server", nil},
		{"mcp____tool", nil},
		{"Read", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, MCPServers(tc.name))
		})
	}
}

func TestTrimMCPTool(t *testing.T) {
	cases := []struct {
		server, name, want string
	}{
		{"github", "mcp__github__get_issue", "get_issue"},
		{"evil", "mcp__github__get_issue", "mcp__github__get_issue"},
		{"evil", "mcp__evil__", "mcp__evil__"},
		{"server", "server/tool", "server/tool"},
	}
	for _, tc := range cases {
		t.Run(tc.server+"/"+tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, TrimMCPTool(tc.server, tc.name))
		})
	}
}

func TestOriginSources(t *testing.T) {
	assert.Equal(t, []string{"evil", "evil__read"}, OriginSources(privacy.OriginMCP, "", "mcp__evil__read__file"))
	assert.Equal(t, []string{"github"}, OriginSources(privacy.OriginMCP, "github", "mcp__evil__read__file"), "the adapter claim wins")
	assert.Nil(t, OriginSources(privacy.OriginWeb, "", "mcp__evil__read__file"))
}

func TestObserveOutput(t *testing.T) {
	cases := []struct {
		name          string
		existing      string
		response      any
		want          string
		wantTruncated bool
	}{
		{"string", "", "AKIA0000", "AKIA0000", false},
		{"map keys and values in key order", "", map[string]any{"stdout": "b", "stderr": "a", "code": 1.0}, "code\nstderr\na\nstdout\nb", false},
		{"nested", "", map[string]any{"file": map[string]any{"content": "secret"}}, "file\ncontent\nsecret", false},
		{"a key with no string value", "", map[string]any{"structuredContent": map[string]any{"AKIAABCDEFGHIJKLMNOP": true}}, "structuredContent\nAKIAABCDEFGHIJKLMNOP", false},
		{"list", "", []any{"x", map[string]any{"text": "y"}}, "x\ntext\ny", false},
		{"keeps write content", "written", "ok", "written", false},
		{"nil", "", nil, "", false},
		{"exactly at the cap", "", strings.Repeat("a", MaxObservedBytes), strings.Repeat("a", MaxObservedBytes), false},
		{"over the cap", "", strings.Repeat("a", MaxObservedBytes+1), strings.Repeat("a", MaxObservedBytes), true},
		{
			"a long value does not push out a short one", "",
			[]any{strings.Repeat("a", MaxObservedBytes), "b"},
			strings.Repeat("a", MaxObservedBytes-2) + "\nb", true,
		},
		{
			"long values share the budget", "",
			map[string]any{"stderr": strings.Repeat("e", MaxObservedBytes), "stdout": strings.Repeat("o", MaxObservedBytes)},
			"stderr\n" + strings.Repeat("e", MaxObservedBytes/2-8) + "\nstdout\n" + strings.Repeat("o", MaxObservedBytes/2-7), true,
		},
		{
			"the cut falls on a rune boundary", "",
			"a" + strings.Repeat("\u00e9", MaxObservedBytes/2),
			"a" + strings.Repeat("\u00e9", MaxObservedBytes/2-1), true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &Event{FullContent: tc.existing}
			e.ObserveOutput(tc.response)
			assert.Equal(t, tc.want, e.FullContent)
			assert.Equal(t, tc.wantTruncated, e.OutputTruncated)
			assert.LessOrEqual(t, len(e.FullContent), MaxObservedBytes)
			assert.True(t, utf8.ValidString(e.FullContent))
		})
	}

	t.Run("a secret after a long stderr stays", func(t *testing.T) {
		e := &Event{}
		e.ObserveOutput(map[string]any{"stderr": strings.Repeat("e", MaxObservedBytes), "stdout": "AKIAABCDEFGHIJKLMNOP"})
		assert.Contains(t, e.FullContent, "AKIAABCDEFGHIJKLMNOP")
		assert.True(t, e.OutputTruncated)
	})
}
