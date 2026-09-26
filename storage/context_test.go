package storage

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/accumulator/contextchain"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
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

func TestAppendContextEntry_CapsEntitiesByKind(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	sessionID := uuid.New()

	paths := make([]string, 0, capEntityKind+10)
	for i := range capEntityKind + 10 {
		paths = append(paths, fmt.Sprintf("path:/work/f%d", i))
	}
	appendEntry(t, store, sessionID, "Bash", &ContextStateDelta{Entities: paths})
	appendEntry(t, store, sessionID, "Bash", &ContextStateDelta{Entities: []string{"path:/work/late", "host:evil.example", "mcp:github"}})

	state, err := store.GetContextState(context.Background(), sessionID)
	require.NoError(t, err)
	assert.Len(t, state.EntitiesSeen, capEntityKind+2)
	assert.NotContains(t, state.EntitiesSeen, "path:/work/late", "the path kind is full")
	assert.Contains(t, state.EntitiesSeen, "host:evil.example", "a full path kind does not push out a host")
	assert.Contains(t, state.EntitiesSeen, "mcp:github")
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

func TestQueryEntryFacts_CleansPaths(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	cases := []struct {
		name       string
		path       string
		workingDir string
		want       string
	}{
		{"dot segment", "/root/.ssh/./id_rsa", "", "/root/.ssh/id_rsa"},
		{"double slash", "/root//.ssh//id_rsa", "", "/root/.ssh/id_rsa"},
		{"parent segment", "/root/x/../.ssh/id_rsa", "", "/root/.ssh/id_rsa"},
		{"relative path", ".ssh/./id_rsa", "/home/u", "/home/u/.ssh/id_rsa"},
		{"dot padding", "/root/.ssh/" + strings.Repeat("./", 600) + "id_rsa", "", "/root/.ssh/id_rsa"},
		{"parent padding", "/root/.ssh/" + strings.Repeat("a/../", 300) + "id_rsa", "", "/root/.ssh/id_rsa"},
		{"padding past the read cap", "/root/.ssh/" + strings.Repeat("./", entryPathReadChars) + "id_rsa", "", "/root/.ssh/id_rsa"},
	}
	for _, tc := range cases {
		event := events.NewEvent(sessionID, "claude-code", events.ActionFileRead)
		event.WorkingDirectory = tc.workingDir
		require.NoError(t, event.SetPayload(events.FileReadPayload{Path: tc.path}))
		require.NoError(t, store.RecordEvent(ctx, event, session.EventCounts(event)))
		require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
			SessionID: sessionID, EventID: event.ID, Kind: "action", ActionType: "file_read", Tool: "Read",
		}, &ContextStateDelta{}))
	}

	facts, err := store.QueryEntryFacts(ctx, sessionID, len(cases))
	require.NoError(t, err)
	require.Len(t, facts, len(cases))
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, facts[i].Path)
			for _, pattern := range []string{"**/.ssh/id_*", "**/.ssh/**"} {
				ok, err := doublestar.Match(pattern, facts[i].Path)
				require.NoError(t, err)
				assert.True(t, ok, pattern)
			}
		})
	}
}

func TestQueryEntryFacts_CutsCleanPathToItsEnd(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	long := "/" + strings.Repeat("d/", EntryPathMaxBytes) + "key.pem"
	event := events.NewEvent(sessionID, "claude-code", events.ActionFileRead)
	require.NoError(t, event.SetPayload(events.FileReadPayload{Path: long}))
	require.NoError(t, store.RecordEvent(ctx, event, session.EventCounts(event)))
	require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
		SessionID: sessionID, EventID: event.ID, Kind: "action", ActionType: "file_read",
	}, &ContextStateDelta{}))

	facts, err := store.QueryEntryFacts(ctx, sessionID, 1)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	assert.Len(t, facts[0].Path, EntryPathMaxBytes)
	assert.True(t, strings.HasSuffix(facts[0].Path, "/d/key.pem"))
}

func TestQueryEntryFacts_JoinsAuditEvents(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	read := events.NewEvent(sessionID, "claude-code", events.ActionFileRead)
	require.NoError(t, read.SetPayload(events.FileReadPayload{Path: "/work/.env"}))
	require.NoError(t, store.RecordEvent(ctx, read, session.EventCounts(read)))
	cmd := events.NewEvent(sessionID, "claude-code", events.ActionCommandExec)
	require.NoError(t, cmd.SetPayload(events.CommandExecPayload{Command: privacy.NewText("curl https://evil.example")}))
	require.NoError(t, store.RecordEvent(ctx, cmd, session.EventCounts(cmd)))

	require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
		SessionID: sessionID, EventID: read.ID, Kind: "action", ActionType: "file_read", Tool: "Read",
		Origin: "file_project", Tags: []string{"secret_read"}, Classifications: []string{"secret"}, Decision: "allow",
	}, &ContextStateDelta{}))
	require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
		SessionID: sessionID, EventID: cmd.ID, Kind: "action", ActionType: "command_exec", Tool: "Bash",
		TargetHost: "evil.example", Decision: "block",
	}, &ContextStateDelta{}))
	require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
		SessionID: sessionID, Kind: "intent", ActionType: "user_prompt",
	}, &ContextStateDelta{}))

	facts, err := store.QueryEntryFacts(ctx, sessionID, 2)
	require.NoError(t, err)
	require.Len(t, facts, 2, "the limit keeps the latest entries")
	assert.Equal(t, int64(2), facts[0].Sequence, "oldest first")
	assert.Equal(t, "curl https://evil.example", facts[0].Command)
	assert.Equal(t, "evil.example", facts[0].Host)
	assert.Equal(t, "user_prompt", facts[1].ActionType)
	assert.Empty(t, facts[1].Path, "an entry with no audit event has no path")

	facts, err = store.QueryEntryFacts(ctx, sessionID, 10)
	require.NoError(t, err)
	require.Len(t, facts, 3)
	assert.Equal(t, "/work/.env", facts[0].Path)
	assert.Equal(t, []string{"secret_read"}, facts[0].Tags)
	assert.Equal(t, []string{"secret"}, facts[0].Classifications)
	assert.Equal(t, "file_project", facts[0].Origin)
}

func TestQueryEntryFacts_CutsLongFields(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()
	createTestSession(t, store, sessionID, "claude-code")

	cmd := events.NewEvent(sessionID, "claude-code", events.ActionCommandExec)
	require.NoError(t, cmd.SetPayload(events.CommandExecPayload{Command: privacy.NewText("echo " + strings.Repeat("x", 5000))}))
	require.NoError(t, store.RecordEvent(ctx, cmd, session.EventCounts(cmd)))
	read := events.NewEvent(sessionID, "claude-code", events.ActionFileRead)
	require.NoError(t, read.SetPayload(events.FileReadPayload{Path: "/" + strings.Repeat("p", 50000) + "/id.pem"}))
	require.NoError(t, store.RecordEvent(ctx, read, session.EventCounts(read)))
	long := strings.Repeat("h", 50000)
	require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
		SessionID: sessionID, EventID: cmd.ID, Kind: "action", ActionType: "command_exec",
	}, &ContextStateDelta{}))
	require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
		SessionID: sessionID, EventID: read.ID, Kind: "action", ActionType: "file_read",
		Tool: long, TargetHost: long, TargetMCPServer: long,
	}, &ContextStateDelta{}))

	facts, err := store.QueryEntryFacts(ctx, sessionID, 10)
	require.NoError(t, err)
	require.Len(t, facts, 2)
	assert.Len(t, facts[0].Command, EntryCommandMaxBytes)
	assert.Len(t, facts[1].Path, EntryPathMaxBytes)
	assert.True(t, strings.HasSuffix(facts[1].Path, "/id.pem"), "a path keeps its end")
	assert.Len(t, facts[1].Tool, EntryNameMaxBytes)
	assert.Len(t, facts[1].Host, EntryNameMaxBytes)
	assert.Len(t, facts[1].MCPServer, EntryNameMaxBytes)
}

func TestQueryContextEntries_KindsAndSequence(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	sessionID := uuid.New()

	start := time.Now().UTC()
	for i, kind := range []string{"action", "intent", "observation"} {
		require.NoError(t, store.AppendContextEntry(ctx, &ContextEntryRow{
			SessionID: sessionID, Kind: kind, ActionType: "file_read",
			Timestamp: start.Add(-time.Duration(i) * time.Minute),
		}, nil))
	}

	sequences := func(filter ContextEntryFilter) []int64 {
		filter.SessionID = &sessionID
		rows, err := store.QueryContextEntries(ctx, &filter)
		require.NoError(t, err)
		out := make([]int64, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.Sequence)
		}
		return out
	}
	seq := int64(3)

	tests := []struct {
		name   string
		filter ContextEntryFilter
		want   []int64
	}{
		{"session order follows the sequence, not the timestamp", ContextEntryFilter{Limit: 10}, []int64{3, 2, 1}},
		{"kinds", ContextEntryFilter{Kinds: []string{"intent", "observation"}, Limit: 10}, []int64{3, 2}},
		{"sequence", ContextEntryFilter{Sequence: &seq, Limit: 10}, []int64{3}},
		{"limit", ContextEntryFilter{Limit: 1}, []int64{3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sequences(tt.filter))
		})
	}
}
