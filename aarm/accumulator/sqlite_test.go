package accumulator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSQLiteAccumulator(t *testing.T) (*SQLiteAccumulator, *storage.SQLiteStore) {
	t.Helper()
	store := storagetest.NewStore(t)
	return NewSQLite(store), store
}

func newEntry(sessionID uuid.UUID, kind events.Kind, at model.ActionType, tool string) *model.ContextEntry {
	return &model.ContextEntry{
		ID:         uuid.New(),
		SessionID:  sessionID,
		Kind:       kind,
		Timestamp:  time.Now().UTC(),
		ActionType: at,
		Tool:       tool,
		Decision:   model.DecisionAllow,
	}
}

func saveSession(t *testing.T, store *storage.SQLiteStore, sess *session.Session) {
	t.Helper()
	require.NoError(t, store.SaveSession(context.Background(), sess))
}

func TestSQLiteAccumulator_SnapshotAddsPendingAction(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sess := session.NewSession("claude-code")
	sess.TotalActions = 2
	sess.CommandsExecuted = 1
	sess.FilesRead = 1
	sess.Errors = 1
	saveSession(t, store, sess)
	require.NoError(t, acc.Append(ctx, newEntry(sess.ID, events.KindAction, model.ActionFileRead, "Read")))

	failedPost := newEntry(sess.ID, events.KindObservation, model.ActionCommandExec, "Bash")
	failedPost.Result = model.ResultError

	cases := []struct {
		name         string
		pending      *model.ContextEntry
		wantTotal    int
		wantCommands int
		wantErrors   int
		wantTools    []string
	}{
		{"no pending entry", nil, 2, 1, 1, []string{"Read"}},
		{"pending action counts", newEntry(sess.ID, events.KindAction, model.ActionCommandExec, "Bash"), 3, 2, 1, []string{"Read", "Bash"}},
		{"pending observation does not count", newEntry(sess.ID, events.KindObservation, model.ActionCommandExec, "Bash"), 2, 1, 1, []string{"Read"}},
		{"pending failed observation counts an error", failedPost, 2, 1, 2, []string{"Read"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap, err := acc.Snapshot(ctx, sess.ID, tc.pending)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTotal, snap.TotalActions)
			assert.Equal(t, tc.wantCommands, snap.CommandsExecuted)
			assert.Equal(t, 1, snap.FilesRead)
			assert.Equal(t, tc.wantErrors, snap.Errors)
			assert.Equal(t, tc.wantTools, snap.ToolsUsed)
		})
	}
}

func TestSQLiteAccumulator_SnapshotNewSession(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	snap, err := acc.Snapshot(context.Background(), uuid.New(), nil)
	require.NoError(t, err)
	assert.Zero(t, snap.TotalActions)
	assert.Empty(t, snap.ToolsUsed)
	assert.Zero(t, snap.SessionDuration)
}

func TestSQLiteAccumulator_SnapshotSessionDuration(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	sess := session.NewSession("claude-code")
	sess.StartedAt = time.Now().UTC().Add(-10 * time.Minute)
	saveSession(t, store, sess)

	snap, err := acc.Snapshot(context.Background(), sess.ID, nil)
	require.NoError(t, err)
	assert.InDelta(t, (10 * time.Minute).Seconds(), snap.SessionDuration.Seconds(), 5)
}

func TestSQLiteAccumulator_AppendWritesEntryAndState(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessionID := uuid.New()

	first := newEntry(sessionID, events.KindAction, model.ActionFileRead, "Read")
	first.Classifications = []privacy.Class{privacy.ClassSecret}
	first.Decision = model.DecisionBlock
	first.MatchedRuleIDs = []string{"r1"}
	require.NoError(t, acc.Append(ctx, first))
	assert.Equal(t, int64(1), first.Sequence)

	observation := newEntry(sessionID, events.KindObservation, model.ActionCommandExec, "Bash")
	observation.Classifications = []privacy.Class{privacy.ClassConfig}
	require.NoError(t, acc.Append(ctx, observation))

	state, err := store.GetContextState(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Read"}, state.ToolsUsed, "only an action adds a tool")
	assert.Equal(t, []string{"secret", "config"}, state.ClassificationsSeen)

	rows, err := store.QueryContextEntries(ctx, &storage.ContextEntryFilter{SessionID: &sessionID, Ascending: true})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, first.ID, rows[0].ID)
	assert.Equal(t, "block", rows[0].Decision)
	assert.Equal(t, []string{"r1"}, rows[0].MatchedRuleIDs)
	assert.Equal(t, "observation", rows[1].Kind)
}

func TestSQLiteAccumulator_RecordResult(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessionID := uuid.New()
	entry := newEntry(sessionID, events.KindAction, model.ActionCommandExec, "Bash")
	require.NoError(t, acc.Append(ctx, entry))

	require.NoError(t, acc.RecordResult(ctx, entry.ID, model.Result{Status: model.ResultError, Error: "exit 1", Duration: 3 * time.Millisecond}))

	rows, err := store.QueryContextEntries(ctx, &storage.ContextEntryFilter{SessionID: &sessionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "error", rows[0].ResultStatus)
	assert.Equal(t, "exit 1", rows[0].ErrorMessage)
}

func TestSQLiteAccumulator_ConcurrentSessionsDoNotCorrupt(t *testing.T) {
	acc, store := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessions := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	const perSession = 10

	var wg sync.WaitGroup
	for _, sid := range sessions {
		wg.Add(1)
		go func(sid uuid.UUID) {
			defer wg.Done()
			for range perSession {
				assert.NoError(t, acc.Append(ctx, newEntry(sid, events.KindAction, model.ActionFileRead, "Read")))
			}
		}(sid)
	}
	wg.Wait()

	for _, sid := range sessions {
		rows, err := store.QueryContextEntries(ctx, &storage.ContextEntryFilter{SessionID: &sid, Limit: -1, Ascending: true})
		require.NoError(t, err)
		require.Len(t, rows, perSession)
		for i, r := range rows {
			assert.Equal(t, int64(i+1), r.Sequence)
		}
	}
}

func TestSQLiteAccumulator_Intent(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessionID := uuid.New()

	snap, err := acc.Snapshot(ctx, sessionID, newEntry(sessionID, events.KindAction, model.ActionFileRead, "Read"))
	require.NoError(t, err)
	assert.False(t, snap.IntentAvailable)
	assert.Zero(t, snap.ActionsSinceIntent)

	intent := newEntry(sessionID, events.KindIntent, model.ActionUserPrompt, "")
	snap, err = acc.Snapshot(ctx, sessionID, intent)
	require.NoError(t, err)
	assert.True(t, snap.IntentAvailable, "the pending intent counts")
	assert.Zero(t, snap.TotalActions, "an intent is not an action")
	require.NoError(t, acc.Append(ctx, intent))

	for range 2 {
		require.NoError(t, acc.Append(ctx, newEntry(sessionID, events.KindAction, model.ActionFileRead, "Read")))
	}
	require.NoError(t, acc.Append(ctx, newEntry(sessionID, events.KindObservation, model.ActionFileRead, "Read")))

	snap, err = acc.Snapshot(ctx, sessionID, newEntry(sessionID, events.KindAction, model.ActionCommandExec, "Bash"))
	require.NoError(t, err)
	assert.True(t, snap.IntentAvailable)
	assert.Equal(t, 3, snap.ActionsSinceIntent, "two stored actions and the pending one, not the observation")

	snap, err = acc.Snapshot(ctx, sessionID, newEntry(sessionID, events.KindIntent, model.ActionUserPrompt, ""))
	require.NoError(t, err)
	assert.Zero(t, snap.ActionsSinceIntent, "a new intent resets the count")

	blocked := newEntry(sessionID, events.KindIntent, model.ActionUserPrompt, "")
	blocked.Decision = model.DecisionBlock
	require.NoError(t, acc.Append(ctx, blocked))
	snap, err = acc.Snapshot(ctx, sessionID, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, snap.ActionsSinceIntent, "a blocked prompt never reached the agent, so it does not reset the count")
}

func TestSQLiteAccumulator_BlockedIntentOnly(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessionID := uuid.New()

	for _, d := range []model.Decision{model.DecisionBlock, model.DecisionDefer, model.DecisionEscalate} {
		e := newEntry(sessionID, events.KindIntent, model.ActionUserPrompt, "")
		e.Decision = d
		require.NoError(t, acc.Append(ctx, e))
	}
	snap, err := acc.Snapshot(ctx, sessionID, nil)
	require.NoError(t, err)
	assert.False(t, snap.IntentAvailable, "a session whose prompts were all stopped has no intent")
}

func TestSQLiteAccumulator_ConfirmIntent(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessionID := uuid.New()

	require.NoError(t, acc.Append(ctx, newEntry(sessionID, events.KindIntent, model.ActionUserPrompt, "")))
	require.NoError(t, acc.Append(ctx, newEntry(sessionID, events.KindAction, model.ActionFileRead, "Read")))
	escalated := newEntry(sessionID, events.KindIntent, model.ActionUserPrompt, "")
	escalated.Decision = model.DecisionEscalate
	require.NoError(t, acc.Append(ctx, escalated))
	action := newEntry(sessionID, events.KindAction, model.ActionCommandExec, "Bash")
	require.NoError(t, acc.Append(ctx, action))

	snapshotCount := func() int {
		snap, err := acc.Snapshot(ctx, sessionID, nil)
		require.NoError(t, err)
		return snap.ActionsSinceIntent
	}
	assert.Equal(t, 2, snapshotCount(), "an escalated intent waits for the approval")

	require.NoError(t, acc.ConfirmIntent(ctx, action.ID))
	assert.Equal(t, 2, snapshotCount(), "an action entry does not become the intent")

	require.NoError(t, acc.ConfirmIntent(ctx, escalated.ID))
	assert.Equal(t, 1, snapshotCount(), "only the action after the approved intent counts")

	require.NoError(t, acc.Append(ctx, newEntry(sessionID, events.KindIntent, model.ActionUserPrompt, "")))
	require.NoError(t, acc.ConfirmIntent(ctx, escalated.ID))
	assert.Zero(t, snapshotCount(), "a late approval does not move the intent back")

	require.NoError(t, acc.ConfirmIntent(ctx, uuid.New()), "an unknown entry is not an error")
}

func TestSQLiteAccumulator_TagsAndOrigins(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessionID := uuid.New()

	read := newEntry(sessionID, events.KindAction, model.ActionFileRead, "Read")
	read.Tags = []string{"secret_read"}
	read.Origin = privacy.OriginFileProject
	require.NoError(t, acc.Append(ctx, read))

	web := newEntry(sessionID, events.KindObservation, model.ActionToolUse, "mcp__github__get_issue")
	web.Tags = []string{"secret_read", "untrusted_input"}
	web.Origin = privacy.OriginMCP
	web.Target = model.DerivedTarget{MCPServer: "github", MCPTool: "get_issue"}
	require.NoError(t, acc.Append(ctx, web))

	pending := newEntry(sessionID, events.KindAction, model.ActionCommandExec, "Bash")
	pending.Origin = privacy.OriginCommand
	pending.Tags = []string{"ignored"}
	snap, err := acc.Snapshot(ctx, sessionID, pending)
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"secret_read": 1, "untrusted_input": 2}, snap.TagsSeen, "each tag keeps the sequence of its first entry")
	assert.Equal(t, []string{"file_project", "mcp:github", "command"}, snap.OriginsSeen, "the pending origin counts")
}

func TestSQLiteAccumulator_ClaimedMCPServerOrigin(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessionID := uuid.New()

	spoofed := newEntry(sessionID, events.KindObservation, model.ActionToolUse, "mcp__github__get_issue")
	spoofed.Origin = privacy.OriginMCP
	spoofed.Target = model.DerivedTarget{MCPServer: "evil", MCPTool: "mcp__github__get_issue"}
	require.NoError(t, acc.Append(ctx, spoofed))

	snap, err := acc.Snapshot(ctx, sessionID, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"mcp:evil"}, snap.OriginsSeen)
	assert.NotContains(t, snap.OriginsSeen, "mcp:github", "the adapter claim wins over the tool name")
}

func TestSQLiteAccumulator_AmbiguousMCPServerOrigins(t *testing.T) {
	acc, _ := newTestSQLiteAccumulator(t)
	ctx := context.Background()
	sessionID := uuid.New()

	evil := newEntry(sessionID, events.KindAction, model.ActionToolUse, "mcp__evil__read__file")
	evil.Origin = privacy.OriginMCP
	evil.Target = model.DerivedTarget{MCPTool: "mcp__evil__read__file"}
	require.NoError(t, acc.Append(ctx, evil))

	claimed := newEntry(sessionID, events.KindAction, model.ActionToolUse, "server/tool")
	claimed.Origin = privacy.OriginMCP
	claimed.Target = model.DerivedTarget{MCPServer: "server", MCPTool: "server/tool"}
	require.NoError(t, acc.Append(ctx, claimed))

	unnamed := newEntry(sessionID, events.KindAction, model.ActionToolUse, "remote")
	unnamed.Origin = privacy.OriginMCP
	snap, err := acc.Snapshot(ctx, sessionID, unnamed)
	require.NoError(t, err)
	assert.Equal(t, []string{"mcp:evil", "mcp:evil__read", "mcp:server", "mcp"}, snap.OriginsSeen,
		"an ambiguous tool name adds every server that it can name")
}
