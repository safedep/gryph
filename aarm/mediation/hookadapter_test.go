package mediation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHookAdapter_Normalize(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	sessID := uuid.New()
	evtID := uuid.New()

	sess := &session.Session{
		ID:               sessID,
		AgentName:        "claude-code",
		AgentSessionID:   "agent-sess-123",
		WorkingDirectory: "/work/proj",
		ProjectName:      "proj",
	}

	tests := []struct {
		name      string
		event     *events.Event
		sess      *session.Session
		assertion func(t *testing.T, a *aarm.Action)
	}{
		{
			name: "file_read with path",
			event: mustEvent(t, evtID, sessID, events.ActionFileRead, "Read", now,
				events.FileReadPayload{Path: "/work/proj/main.go", SizeBytes: 1024}),
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "/work/proj/main.go", a.Parameters.Path)
				assert.Equal(t, int64(1024), a.Parameters.SizeBytes)
			},
		},
		{
			name: "file_read falls back to pattern",
			event: mustEvent(t, evtID, sessID, events.ActionFileRead, "Glob", now,
				events.FileReadPayload{Pattern: "**/*.go"}),
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "**/*.go", a.Parameters.Path)
			},
		},
		{
			name: "file_write",
			event: mustEvent(t, evtID, sessID, events.ActionFileWrite, "Edit", now,
				events.FileWritePayload{
					Path:           "/work/proj/x.go",
					SizeBytes:      512,
					LinesAdded:     10,
					LinesRemoved:   2,
					ContentPreview: "package main",
				}),
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "/work/proj/x.go", a.Parameters.Path)
				assert.Equal(t, 10, a.Parameters.LinesAdded)
				assert.Equal(t, 2, a.Parameters.LinesRemoved)
				assert.Equal(t, "package main", a.Parameters.Content)
			},
		},
		{
			name: "file_delete",
			event: mustEvent(t, evtID, sessID, events.ActionFileDelete, "Bash", now,
				events.FileDeletePayload{Path: "/tmp/foo"}),
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "/tmp/foo", a.Parameters.Path)
			},
		},
		{
			name: "command_exec with args",
			event: mustEvent(t, evtID, sessID, events.ActionCommandExec, "Bash", now,
				events.CommandExecPayload{
					Command:       "kubectl get pods",
					Args:          []string{"get", "pods"},
					StdoutPreview: "NAME READY",
				}),
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "kubectl get pods", a.Parameters.Command)
				assert.Equal(t, []string{"get", "pods"}, a.Parameters.Args)
				assert.Equal(t, "NAME READY", a.Parameters.Content)
			},
		},
		{
			name: "tool_use surfaces url/path/command from input",
			event: mustEvent(t, evtID, sessID, events.ActionToolUse, "WebFetch", now,
				events.ToolUsePayload{
					ToolName: "WebFetch",
					Input:    json.RawMessage(`{"url":"https://example.com","prompt":"hi"}`),
				}),
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "https://example.com", a.Parameters.URL)
				require.NotNil(t, a.Parameters.Raw)
				assert.Equal(t, "hi", a.Parameters.Raw["prompt"])
			},
		},
		{
			name: "tool_use with file_path",
			event: mustEvent(t, evtID, sessID, events.ActionToolUse, "Read", now,
				events.ToolUsePayload{
					ToolName: "Read",
					Input:    json.RawMessage(`{"file_path":"/etc/hosts"}`),
				}),
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "/etc/hosts", a.Parameters.Path)
			},
		},
		{
			name: "session_start has empty parameters",
			event: mustEvent(t, evtID, sessID, events.ActionSessionStart, "", now,
				events.SessionPayload{Source: "startup"}),
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Empty(t, a.Parameters.Path)
				assert.Empty(t, a.Parameters.Command)
			},
		},
		{
			name: "no payload yields empty parameters",
			event: &events.Event{
				ID:         evtID,
				SessionID:  sessID,
				Timestamp:  now,
				AgentName:  "claude-code",
				ActionType: events.ActionNotification,
			},
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, aarm.Parameters{}, a.Parameters)
			},
		},
		{
			name: "session fields backfill missing event fields",
			event: &events.Event{
				ID:         evtID,
				SessionID:  sessID,
				Timestamp:  now,
				AgentName:  "claude-code",
				ActionType: events.ActionFileRead,
				Payload: mustMarshal(t, events.FileReadPayload{Path: "/a"}),
			},
			sess: sess,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "agent-sess-123", a.AgentSessionID)
				assert.Equal(t, "/work/proj", a.WorkingDir)
				assert.Equal(t, "proj", a.Project)
			},
		},
		{
			name: "no session is tolerated",
			event: mustEvent(t, evtID, sessID, events.ActionFileRead, "Read", now,
				events.FileReadPayload{Path: "/x"}),
			sess: nil,
			assertion: func(t *testing.T, a *aarm.Action) {
				assert.Equal(t, "/x", a.Parameters.Path)
				assert.Empty(t, a.Project)
			},
		},
	}

	adapter := NewHookAdapter()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, err := adapter.Normalize(context.Background(), tc.event, tc.sess)
			require.NoError(t, err)
			require.NotNil(t, action)

			assert.NotEqual(t, uuid.Nil, action.ID, "Action ID should be generated")
			assert.Equal(t, tc.event.ID, action.EventID)
			assert.Equal(t, tc.event.SessionID, action.SessionID)
			assert.Equal(t, tc.event.Timestamp, action.Timestamp)
			assert.Equal(t, tc.event.ActionType, action.Type)
			assert.Equal(t, tc.event.AgentName, action.Agent)
			assert.Equal(t, tc.event.ToolName, action.Tool)

			tc.assertion(t, action)
		})
	}
}

func TestHookAdapter_Normalize_NilEvent(t *testing.T) {
	_, err := NewHookAdapter().Normalize(context.Background(), nil, nil)
	require.Error(t, err)
}

func TestHookAdapter_Normalize_GeneratesUniqueIDs(t *testing.T) {
	now := time.Now()
	evt := mustEvent(t, uuid.New(), uuid.New(), events.ActionFileRead, "Read", now,
		events.FileReadPayload{Path: "/x"})

	adapter := NewHookAdapter()
	a1, err := adapter.Normalize(context.Background(), evt, nil)
	require.NoError(t, err)
	a2, err := adapter.Normalize(context.Background(), evt, nil)
	require.NoError(t, err)

	assert.NotEqual(t, a1.ID, a2.ID, "each normalization gets its own Action ID")
}

func mustEvent(t *testing.T, id, sessID uuid.UUID, at events.ActionType, tool string, ts time.Time, payload any) *events.Event {
	t.Helper()
	return &events.Event{
		ID:         id,
		SessionID:  sessID,
		Timestamp:  ts,
		AgentName:  "claude-code",
		ToolName:   tool,
		ActionType: at,
		Payload:    mustMarshal(t, payload),
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
