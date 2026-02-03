package cli_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedFileWriteWithDiff(env *testEnv) string {
	var eventID string
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		evt := events.NewEvent(sessID, "claude-code", events.ActionFileWrite)
		evt.Sequence = 1
		evt.Timestamp = time.Now().UTC().Add(-30 * time.Minute)
		evt.ResultStatus = events.ResultSuccess
		evt.ToolName = "Write"
		evt.DiffContent = "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,4 @@\n package main\n+\n+import \"fmt\"\n"
		payload := &events.FileWritePayload{Path: "/tmp/project/main.go", LinesAdded: 2, LinesRemoved: 0}
		require.NoError(env.t, evt.SetPayload(payload))
		require.NoError(env.t, store.SaveEvent(ctx, evt))

		sess.TotalActions = 1
		sess.FilesWritten = 1
		require.NoError(env.t, store.UpdateSession(ctx, sess))

		eventID = evt.ID.String()
	})

	return eventID
}

func seedFileWriteNoDiff(env *testEnv) string {
	var eventID string
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		evt := events.NewEvent(sessID, "claude-code", events.ActionFileWrite)
		evt.Sequence = 1
		evt.Timestamp = time.Now().UTC().Add(-30 * time.Minute)
		evt.ResultStatus = events.ResultSuccess
		evt.ToolName = "Write"
		payload := &events.FileWritePayload{Path: "/tmp/project/main.go", LinesAdded: 5}
		require.NoError(env.t, evt.SetPayload(payload))
		require.NoError(env.t, store.SaveEvent(ctx, evt))

		sess.TotalActions = 1
		require.NoError(env.t, store.UpdateSession(ctx, sess))

		eventID = evt.ID.String()
	})
	return eventID
}

func seedSensitiveFileWrite(env *testEnv) string {
	var eventID string
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		evt := events.NewEvent(sessID, "claude-code", events.ActionFileWrite)
		evt.Sequence = 1
		evt.Timestamp = time.Now().UTC().Add(-30 * time.Minute)
		evt.ResultStatus = events.ResultSuccess
		evt.ToolName = "Write"
		evt.IsSensitive = true
		payload := &events.FileWritePayload{Path: "/tmp/project/.env"}
		require.NoError(env.t, evt.SetPayload(payload))
		require.NoError(env.t, store.SaveEvent(ctx, evt))

		sess.TotalActions = 1
		require.NoError(env.t, store.UpdateSession(ctx, sess))

		eventID = evt.ID.String()
	})
	return eventID
}

func seedFileRead(env *testEnv) string {
	var eventID string
	env.seedStore(func(ctx context.Context, store storage.Store) {
		sessID := uuid.New()
		sess := session.NewSessionWithID(sessID, "claude-code")
		sess.StartedAt = time.Now().UTC().Add(-1 * time.Hour)
		sess.WorkingDirectory = "/tmp/project"
		require.NoError(env.t, store.SaveSession(ctx, sess))

		evt := events.NewEvent(sessID, "claude-code", events.ActionFileRead)
		evt.Sequence = 1
		evt.Timestamp = time.Now().UTC().Add(-30 * time.Minute)
		evt.ResultStatus = events.ResultSuccess
		evt.ToolName = "Read"
		payload := &events.FileReadPayload{Path: "/tmp/project/main.go"}
		require.NoError(env.t, evt.SetPayload(payload))
		require.NoError(env.t, store.SaveEvent(ctx, evt))

		sess.TotalActions = 1
		require.NoError(env.t, store.UpdateSession(ctx, sess))

		eventID = evt.ID.String()
	})
	return eventID
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(env *testEnv) string
		args   func(eventID string) []string
		assert func(t *testing.T, stdout string, err error)
	}{
		{
			name:  "shows_diff_content",
			setup: seedFileWriteWithDiff,
			args:  func(id string) []string { return []string{"diff", id} },
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "--- a/")
			},
		},
		{
			name:  "diff_not_captured",
			setup: seedFileWriteNoDiff,
			args:  func(id string) []string { return []string{"diff", id} },
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "Diff not captured")
			},
		},
		{
			name:  "sensitive_event",
			setup: seedSensitiveFileWrite,
			args:  func(id string) []string { return []string{"diff", id} },
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "SENSITIVE")
			},
		},
		{
			name:  "wrong_action_type",
			setup: seedFileRead,
			args:  func(id string) []string { return []string{"diff", id} },
			assert: func(t *testing.T, stdout string, err error) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "file_write")
			},
		},
		{
			name:  "prefix_match",
			setup: seedFileWriteWithDiff,
			args: func(id string) []string {
				return []string{"diff", id[:8]}
			},
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "--- a/")
			},
		},
		{
			name:  "json_format",
			setup: seedFileWriteWithDiff,
			args:  func(id string) []string { return []string{"diff", id, "--format", "json"} },
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				var result json.RawMessage
				assert.NoError(t, json.Unmarshal([]byte(stdout), &result))
			},
		},
		{
			name: "not_found",
			setup: func(env *testEnv) string {
				return "00000000-0000-0000-0000-000000000000"
			},
			args: func(id string) []string { return []string{"diff", id} },
			assert: func(t *testing.T, stdout string, err error) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "not found")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			eventID := tt.setup(env)
			args := tt.args(eventID)
			stdout, _, err := env.run(args...)
			tt.assert(t, stdout, err)
		})
	}
}
