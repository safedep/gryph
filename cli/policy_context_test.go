package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAarmSessionID_FullUUID(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	id := uuid.New()

	got, err := resolveAarmSessionID(ctx, store, id.String())
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestResolveAarmSessionID_PrefixFindsContextStateWithoutSession(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	sessionID := uuid.New()
	require.NoError(t, store.AppendContextAction(ctx, &storage.ContextActionRow{
		SessionID:  sessionID,
		Timestamp:  now,
		ActionType: "file_read",
		Tool:       "Read",
		Agent:      "claude-code",
	}))

	sess, err := store.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Nil(t, sess, "sessions row must not exist for this test case")

	got, err := resolveAarmSessionID(ctx, store, sessionID.String()[:8])
	require.NoError(t, err)
	assert.Equal(t, sessionID, got)
}

func TestResolveAarmSessionID_FallsBackToSession(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	sessionID := uuid.New()
	require.NoError(t, store.SaveSession(ctx, session.NewSessionWithID(sessionID, "claude-code")))

	got, err := resolveAarmSessionID(ctx, store, sessionID.String()[:8])
	require.NoError(t, err)
	assert.Equal(t, sessionID, got)
}

func TestResolveAarmSessionID_NoMatch(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	_, err := resolveAarmSessionID(ctx, store, "deadbeef")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no session or context state matches")
}

func appendSampleContextActions(t *testing.T, store *storage.SQLiteStore, sessionID uuid.UUID, n int) {
	t.Helper()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < n; i++ {
		require.NoError(t, store.AppendContextAction(ctx, &storage.ContextActionRow{
			SessionID:  sessionID,
			Timestamp:  base.Add(time.Duration(i) * time.Millisecond),
			ActionType: string(events.ActionFileRead),
			Tool:       "Read",
			Agent:      "claude-code",
		}))
	}
}

func TestRunPolicyContextVerify_CleanChain(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	appendSampleContextActions(t, store, sessionID, 4)

	var buf bytes.Buffer
	c := tui.NewColorizer(false)
	err := runPolicyContextVerify(ctx, &buf, c, store, sessionID.String(), 50, false, "table")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Context chain verification: OK")
}

func TestRunPolicyContextVerify_DetectsTamper(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	appendSampleContextActions(t, store, sessionID, 3)

	_, dbErr := store.DB().ExecContext(ctx,
		`UPDATE aarm_context_actions SET tool = 'tampered' WHERE session_id = ? AND sequence = 2`,
		sessionID,
	)
	require.NoError(t, dbErr)

	var buf bytes.Buffer
	c := tui.NewColorizer(false)
	err := runPolicyContextVerify(ctx, &buf, c, store, sessionID.String(), 50, false, "table")
	require.Error(t, err, "tampered row must surface as a verification failure")
	assert.Contains(t, buf.String(), "Context chain verification: FAILED")

	audits, qErr := store.QuerySelfAudits(ctx, &storage.SelfAuditFilter{Action: SelfAuditActionContextChainBroken})
	require.NoError(t, qErr)
	assert.NotEmpty(t, audits, "chain break must produce a self-audit row")
}

func TestRunPolicyContextVerify_TamperedChainJSONReportsBroken(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	appendSampleContextActions(t, store, sessionID, 3)

	_, dbErr := store.DB().ExecContext(ctx,
		`UPDATE aarm_context_actions SET tool = 'tampered' WHERE session_id = ? AND sequence = 2`,
		sessionID,
	)
	require.NoError(t, dbErr)

	var buf bytes.Buffer
	c := tui.NewColorizer(false)
	err := runPolicyContextVerify(ctx, &buf, c, store, sessionID.String(), 50, false, "json")
	require.Error(t, err, "tampered row must surface as a verification failure even in JSON mode")

	var payload struct {
		Summary contextVerifySummary `json:"summary"`
		Breaks  []contextVerifyBreak `json:"chain_breaks"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload),
		"verify JSON must decode into the documented shape")
	assert.Greater(t, payload.Summary.Broken, 0,
		"JSON summary.broken must mirror the chain_breaks count, got summary=%+v breaks=%+v",
		payload.Summary, payload.Breaks)
	assert.Equal(t, len(payload.Breaks), payload.Summary.Broken,
		"summary.broken must equal len(chain_breaks)")
}

func TestRunPolicyContextVerify_AllSessions(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()

	sessionA := uuid.New()
	sessionB := uuid.New()
	appendSampleContextActions(t, store, sessionA, 3)
	appendSampleContextActions(t, store, sessionB, 3)

	var buf bytes.Buffer
	c := tui.NewColorizer(false)
	err := runPolicyContextVerify(ctx, &buf, c, store, "", 50, true, "json")
	require.NoError(t, err, "two clean sessions must verify successfully")

	var payload struct {
		Actions []contextVerifyActionView `json:"actions"`
		Summary contextVerifySummary      `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.Equal(t, 6, payload.Summary.OK,
		"both chains must contribute to ok count, got %+v", payload.Summary)
	seenSessions := map[string]struct{}{}
	for _, a := range payload.Actions {
		seenSessions[a.SessionID] = struct{}{}
	}
	assert.Contains(t, seenSessions, sessionA.String(), "sessionA must be walked")
	assert.Contains(t, seenSessions, sessionB.String(), "sessionB must be walked")

	_, dbErr := store.DB().ExecContext(ctx,
		`UPDATE aarm_context_actions SET tool = 'tampered' WHERE session_id = ? AND sequence = 2`,
		sessionA,
	)
	require.NoError(t, dbErr)

	buf.Reset()
	err = runPolicyContextVerify(ctx, &buf, c, store, "", 50, true, "json")
	require.Error(t, err, "tampered chain under --all-sessions must surface a verification failure")

	var tampered struct {
		Actions []contextVerifyActionView `json:"actions"`
		Breaks  []contextVerifyBreak      `json:"chain_breaks"`
		Summary contextVerifySummary      `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &tampered))
	assert.NotEmpty(t, tampered.Breaks, "tamper on sessionA must produce at least one break")
	for _, b := range tampered.Breaks {
		assert.Equal(t, sessionA, b.SessionID, "only sessionA was tampered, got %+v", b)
	}
	tamperedSessions := map[string]struct{}{}
	for _, a := range tampered.Actions {
		tamperedSessions[a.SessionID] = struct{}{}
	}
	assert.Contains(t, tamperedSessions, sessionA.String(),
		"sessionA must still be walked when --all-sessions is set")
	assert.Contains(t, tamperedSessions, sessionB.String(),
		"sessionB must still be walked even though sessionA broke")
}

func TestRunPolicyContextVerify_UnchainedRowsDoNotFail(t *testing.T) {
	store := storagetest.NewStore(t)
	ctx := context.Background()
	sessionID := uuid.New()

	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err := store.DB().ExecContext(ctx,
		`INSERT INTO aarm_context_actions (id, session_id, timestamp, action_type, tool, agent, result_status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.New(), sessionID, now, string(events.ActionFileRead), "Read", "claude-code", "pending",
	)
	require.NoError(t, err)

	var buf bytes.Buffer
	c := tui.NewColorizer(false)
	err = runPolicyContextVerify(ctx, &buf, c, store, sessionID.String(), 50, false, "table")
	require.NoError(t, err, "pre-Phase-5a unchained rows must not break verification")
	assert.Contains(t, buf.String(), "Context chain verification: OK")
	assert.Contains(t, buf.String(), "unchained=1")
}
