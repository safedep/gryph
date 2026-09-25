package storage

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/accumulator/contextchain"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func appendEntry(t *testing.T, store *SQLiteStore, sessionID uuid.UUID, tool string, delta *ContextStateDelta) *ContextEntryRow {
	t.Helper()
	row := &ContextEntryRow{
		SessionID:  sessionID,
		Kind:       "action",
		Timestamp:  time.Now().UTC(),
		ActionType: "file_read",
		Tool:       tool,
		Decision:   "allow",
	}
	require.NoError(t, store.AppendContextEntry(context.Background(), row, delta))
	return row
}

func sessionEntries(t *testing.T, store *SQLiteStore, sessionID uuid.UUID) []*ContextEntryRow {
	t.Helper()
	rows, err := store.QueryContextEntries(context.Background(), &ContextEntryFilter{
		SessionID: &sessionID, Limit: -1, Ascending: true,
	})
	require.NoError(t, err)
	return rows
}

func chainRows(rows []*ContextEntryRow) []contextchain.Row {
	out := make([]contextchain.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, contextchain.Row{
			SessionID: r.SessionID, Sequence: r.Sequence, Version: r.HashVersion,
			PrevHash: r.PrevHash, Hash: r.Hash, Fields: ContextChainInput(r),
		})
	}
	return out
}

func TestAppendContextEntry_Chain(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	sessionID := uuid.New()

	for range 3 {
		appendEntry(t, store, sessionID, "Read", nil)
	}

	rows := sessionEntries(t, store, sessionID)
	require.Len(t, rows, 3)
	for i, r := range rows {
		assert.Equal(t, int64(i+1), r.Sequence)
		assert.Equal(t, contextchain.Version, r.HashVersion)
		assert.Equal(t, "pending", r.ResultStatus)
		if i == 0 {
			assert.Empty(t, r.PrevHash)
		} else {
			assert.Equal(t, rows[i-1].Hash, r.PrevHash)
		}
	}

	verified, breaks := contextchain.Verify(chainRows(rows))
	assert.Equal(t, 3, verified)
	assert.Empty(t, breaks)

	rows[1].Decision = "block"
	_, breaks = contextchain.Verify(chainRows(rows))
	assert.NotEmpty(t, breaks, "a changed decision breaks the chain")
}

func TestAppendContextEntry_EmptyListsVerify(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	sessionID := uuid.New()
	row := &ContextEntryRow{
		SessionID: sessionID, Kind: "action", ActionType: "file_read",
		Tags: []string{}, Classifications: []string{}, MatchedRuleIDs: []string{},
	}
	require.NoError(t, store.AppendContextEntry(context.Background(), row, nil))

	_, breaks := contextchain.Verify(chainRows(sessionEntries(t, store, sessionID)))
	assert.Empty(t, breaks, "an empty list reads back as nil and must hash the same")
}

func TestAppendContextEntry_ConcurrentSameSession(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	sessionID := uuid.New()
	const writers = 16

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, writers)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			row := &ContextEntryRow{SessionID: sessionID, Kind: "action", ActionType: "file_read", Tool: "Read"}
			if err := store.AppendContextEntry(context.Background(), row, &ContextStateDelta{Tools: []string{"Read"}}); err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	rows := sessionEntries(t, store, sessionID)
	require.Len(t, rows, writers)
	var prevHash []byte
	for i, r := range rows {
		assert.Equal(t, int64(i+1), r.Sequence)
		if i > 0 {
			assert.True(t, bytes.Equal(r.PrevHash, prevHash), "entry %d prev_hash mismatch", i)
		}
		prevHash = r.Hash
	}
}

func TestAppendContextEntry_CrossSessionIsolation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	a, b := uuid.New(), uuid.New()

	appendEntry(t, store, a, "Read", nil)
	appendEntry(t, store, b, "Read", nil)
	appendEntry(t, store, a, "Read", nil)

	assert.Len(t, sessionEntries(t, store, a), 2)
	rowsB := sessionEntries(t, store, b)
	require.Len(t, rowsB, 1)
	assert.Equal(t, int64(1), rowsB[0].Sequence)

	ids, err := store.ListContextSessionIDs(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{a, b}, ids)
}

func TestAppendContextEntry_MergesState(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	appendEntry(t, store, sessionID, "Read", &ContextStateDelta{
		Tools: []string{"Read"}, Classifications: []string{"config"}, Tags: []string{"secret_read"},
		Origins: []string{"file_project"}, Entities: []string{"path:/a"}, EgressHosts: []string{"a.example"},
	})
	second := appendEntry(t, store, sessionID, "Bash", &ContextStateDelta{
		Tools: []string{"Bash", "Read"}, Classifications: []string{"secret"}, Tags: []string{"secret_read", "egress"},
		Origins: []string{"mcp:github"}, Intent: true,
	})

	state, err := store.GetContextState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, []string{"Read", "Bash"}, state.ToolsUsed)
	assert.Equal(t, []string{"config", "secret"}, state.ClassificationsSeen)
	assert.Equal(t, map[string]int64{"secret_read": 1, "egress": 2}, state.TagsSeen)
	assert.Equal(t, []string{"file_project", "mcp:github"}, state.OriginsSeen)
	assert.Equal(t, []string{"path:/a"}, state.EntitiesSeen)
	assert.Equal(t, []string{"a.example"}, state.EgressHosts)
	require.NotNil(t, state.LastIntentSeq)
	assert.Equal(t, second.Sequence, *state.LastIntentSeq)
	require.NotNil(t, state.LastIntentAt)
	assert.False(t, state.StartedAt.IsZero(), "the state joins the session row")
}

func TestAppendContextEntry_CapsSets(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	sessionID := uuid.New()

	tools := make([]string, 0, capTools+10)
	for i := range capTools + 10 {
		tools = append(tools, uuid.NewString()[:8]+string(rune('a'+i%26)))
	}
	appendEntry(t, store, sessionID, "Read", &ContextStateDelta{Tools: tools})

	state, err := store.GetContextState(context.Background(), sessionID)
	require.NoError(t, err)
	assert.Len(t, state.ToolsUsed, capTools)
}

func TestGetContextState_JoinsSessionCounters(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	for _, at := range []events.ActionType{events.ActionCommandExec, events.ActionCommandExec, events.ActionNetworkRequest, events.ActionFileRead} {
		event := events.NewEvent(sessionID, "claude-code", at)
		require.NoError(t, store.RecordEvent(ctx, event, session.EventCounts(event)))
	}

	state, err := store.GetContextState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, state, "a session without entries still has counters")
	assert.Equal(t, 4, state.TotalActions)
	assert.Equal(t, 2, state.CommandsExecuted)
	assert.Equal(t, 1, state.NetworkRequests)
	assert.Empty(t, state.ToolsUsed)

	missing, err := store.GetContextState(ctx, uuid.New())
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestSQLiteStore_GetContextStateByPrefix(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.MustParse("aaaabbbb-0000-0000-0000-000000000001")
	appendEntry(t, store, sessionID, "Read", nil)

	state, err := store.GetContextStateByPrefix(ctx, "aaaabbbb")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, sessionID, state.SessionID)

	none, err := store.GetContextStateByPrefix(ctx, "ffff")
	require.NoError(t, err)
	assert.Nil(t, none)

	appendEntry(t, store, uuid.MustParse("aaaabbbb-0000-0000-0000-000000000002"), "Read", nil)
	_, err = store.GetContextStateByPrefix(ctx, "aaaabbbb")
	assert.ErrorContains(t, err, "ambiguous")
}

func TestUpdateContextEntryResult(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()
	row := appendEntry(t, store, sessionID, "Bash", nil)

	require.NoError(t, store.UpdateContextEntryResult(ctx, row.ID, "error", 42, "exit 1"))
	require.NoError(t, store.UpdateContextEntryResult(ctx, uuid.New(), "success", 0, ""), "an unknown entry is not an error")

	rows := sessionEntries(t, store, sessionID)
	require.Len(t, rows, 1)
	assert.Equal(t, "error", rows[0].ResultStatus)
	require.NotNil(t, rows[0].DurationMS)
	assert.Equal(t, int64(42), *rows[0].DurationMS)
	assert.Equal(t, "exit 1", rows[0].ErrorMessage)

	_, breaks := contextchain.Verify(chainRows(rows))
	assert.Empty(t, breaks, "the result is not part of the hash")
}

func TestDeleteContextBefore_WholeSessions(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	old, recent := uuid.New(), uuid.New()
	past := time.Now().UTC().Add(-48 * time.Hour)

	for _, ts := range []time.Time{past, past.Add(time.Minute)} {
		require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
			SessionID: old, Kind: "action", ActionType: "file_read", Timestamp: ts,
		}, &ContextStateDelta{}))
	}
	appendEntry(t, store, recent, "Read", nil)

	cutoff := time.Now().UTC().Add(-time.Hour)
	n, err := store.CountContextBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	deleted, err := store.DeleteContextBefore(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)
	assert.Empty(t, sessionEntries(t, store, old))
	assert.Len(t, sessionEntries(t, store, recent), 1)

	state, err := store.GetContextState(ctx, old)
	require.NoError(t, err)
	assert.Nil(t, state, "the state row of a purged session is removed")
}

// retiredContextStateDDL is the aarm_context_states table of gryph v0.9.0.
const retiredContextStateDDL = "CREATE TABLE `aarm_context_states` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, " +
	"`session_id` uuid NOT NULL, `first_seen_at` datetime NOT NULL, `last_action_at` datetime NOT NULL, " +
	"`total_actions` integer NOT NULL DEFAULT (0), `files_read` integer NOT NULL DEFAULT (0), " +
	"`files_written` integer NOT NULL DEFAULT (0), `commands_executed` integer NOT NULL DEFAULT (0), " +
	"`network_requests` integer NOT NULL DEFAULT (0), `errors` integer NOT NULL DEFAULT (0), " +
	"`tools_used` json NULL, `classifications_seen` json NULL, `entities_seen` json NULL, " +
	"`semantic_drift` real NOT NULL DEFAULT (0))"

func TestInit_DropsRetiredContextTables(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	sessionID := uuid.New()
	require.NoError(t, store.SaveSession(ctx, &session.Session{ID: sessionID, AgentName: "claude-code", StartedAt: time.Now().UTC()}))
	_, err := store.db.ExecContext(ctx, "CREATE TABLE aarm_context_actions (id TEXT)")
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, retiredContextStateDDL)
	require.NoError(t, err)
	now := time.Now().UTC()
	_, err = store.db.ExecContext(ctx, `
INSERT INTO aarm_context_states (session_id, first_seen_at, last_action_at, network_requests, tools_used, classifications_seen)
VALUES (?, ?, ?, 3, '["Bash","Read"]', '["secret"]')`, sessionID.String(), now, now)
	require.NoError(t, err)

	require.NoError(t, store.Init(ctx))
	require.NoError(t, store.Init(ctx), "a second Init finds no retired table")

	for _, table := range retiredTables {
		var n int
		require.NoError(t, store.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n))
		assert.Zero(t, n, "table %s", table)
	}

	state, err := store.GetContextState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, state, "the old state of a running session is copied")
	assert.Equal(t, []string{"Bash", "Read"}, state.ToolsUsed)
	assert.Equal(t, []string{"secret"}, state.ClassificationsSeen)
	assert.Equal(t, 3, state.NetworkRequests)

	appendEntry(t, store, sessionID, "Write", &ContextStateDelta{Tools: []string{"Write"}})
	state, err = store.GetContextState(ctx, sessionID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Bash", "Read", "Write"}, state.ToolsUsed, "a new entry merges into the copied state")
	assert.Equal(t, []string{"secret"}, state.ClassificationsSeen)
}

func TestInit_KeepsUnknownColumns(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN future_col TEXT")
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, "CREATE INDEX sessions_future_col ON sessions (future_col)")
	require.NoError(t, err)
	require.NoError(t, store.Init(ctx))

	var n int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'future_col'`).Scan(&n))
	assert.Equal(t, 1, n, "an older binary must not drop the column of a newer one")
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'sessions_future_col'`).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestContextState_ActionsSinceIntent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()

	appendRow := func(kind, actionType string, intent bool) {
		require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
			SessionID: sessionID, Kind: kind, ActionType: actionType,
		}, &ContextStateDelta{Intent: intent}))
	}
	appendRow("action", "file_read", false)
	appendRow("intent", "user_prompt", true)
	appendRow("action", "file_read", false)
	appendRow("observation", "file_read", false)
	appendRow("action", "command_exec", false)

	state, err := store.GetContextState(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, state.LastIntentSeq)
	assert.Equal(t, 2, state.ActionsSinceIntent)

	all, err := store.QueryAllContextStates(ctx, 10)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, 2, all[0].ActionsSinceIntent, "the list view counts the same as the session view")
}
