package hookside

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/decision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const transcriptLine = `{"type":"assistant","message":{"model":"claude-sonnet-4-20250514","usage":{"input_tokens":1500,"output_tokens":700,"cache_read_input_tokens":400,"cache_creation_input_tokens":200}}}` + "\n"

func writeTranscript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestClaimProject(t *testing.T) {
	manifest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(manifest, "package.json"), []byte(`{"name":"acme-web"}`), 0o600))
	plain := filepath.Join(t.TempDir(), "plain-dir")
	require.NoError(t, os.Mkdir(plain, 0o700))

	cases := []struct {
		name string
		dir  string
		want decision.ProjectClaim
	}{
		{name: "manifest names the project", dir: manifest, want: decision.ProjectClaim{Name: "acme-web"}},
		{name: "directory name without a manifest", dir: plain, want: decision.ProjectClaim{Name: "plain-dir"}},
		{name: "missing directory uses its name", dir: "/no/such/dir", want: decision.ProjectClaim{Name: "dir"}},
		{name: "empty directory makes no claim", dir: "", want: decision.ProjectClaim{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ClaimProject(tc.dir))
		})
	}
}

func TestCollectCost(t *testing.T) {
	sessionID := uuid.New()
	cases := []struct {
		name       string
		agent      string
		transcript string
		wantNil    bool
	}{
		{name: "claude code transcript", agent: agent.AgentClaudeCode, transcript: writeTranscript(t, transcriptLine)},
		{name: "empty transcript", agent: agent.AgentClaudeCode, transcript: writeTranscript(t, ""), wantNil: true},
		{name: "missing transcript", agent: agent.AgentClaudeCode, transcript: filepath.Join(t.TempDir(), "none.jsonl"), wantNil: true},
		{name: "no transcript path", agent: agent.AgentClaudeCode, wantNil: true},
		{name: "agent without a collector", agent: agent.AgentCursor, transcript: writeTranscript(t, transcriptLine), wantNil: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := CollectCost(context.Background(), tc.agent, tc.transcript, sessionID)
			if tc.wantNil {
				assert.Nil(t, sc)
				return
			}
			require.NotNil(t, sc)
			assert.Equal(t, sessionID, sc.SessionID)
			assert.Equal(t, cost.CostSourceTranscript, sc.Source)
			assert.Equal(t, int64(1500), sc.Usage.InputTokens)
			assert.Equal(t, int64(700), sc.Usage.OutputTokens)
			assert.Equal(t, int64(400), sc.Usage.CacheReadTokens)
			assert.Equal(t, int64(200), sc.Usage.CacheWriteTokens)
			assert.Positive(t, sc.TotalCost)
			assert.False(t, sc.ComputedAt.IsZero())
		})
	}
}

func TestNewRequest(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/acme-svc\n"), 0o600))
	transcript := writeTranscript(t, transcriptLine)

	cases := []struct {
		name     string
		action   events.ActionType
		wantCost bool
	}{
		{name: "session end carries the cost", action: events.ActionSessionEnd, wantCost: true},
		{name: "other actions carry no cost", action: events.ActionFileRead},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := events.NewEvent(uuid.New(), agent.AgentClaudeCode, tc.action)
			event.WorkingDirectory = dir
			event.TranscriptPath = transcript

			req := NewRequest(context.Background(), event)
			assert.Equal(t, decision.ProjectClaim{Name: "acme-svc"}, req.Project)
			assert.Equal(t, transcript, req.TranscriptPath)
			if !tc.wantCost {
				assert.Nil(t, req.Cost)
				return
			}
			require.NotNil(t, req.Cost)
			assert.Equal(t, int64(2200), req.Cost.Usage.InputTokens+req.Cost.Usage.OutputTokens)
		})
	}
}
