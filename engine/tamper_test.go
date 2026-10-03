package engine

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/selfprotect"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tamperFixture(t *testing.T) (*TamperRecorder, *storage.SQLiteStore) {
	t.Helper()
	store := storagetest.NewStore(t)
	return newTamperRecorder(store, receipt.NewSQLite(store), "42"), store
}

func hookConfig(agent string, level selfprotect.Level, drift string) selfprotect.AssetStatus {
	return selfprotect.AssetStatus{Asset: selfprotect.AssetHookConfig, Agent: agent, Level: level, Provider: "user", Drift: drift, Detail: "/home/u/." + agent}
}

func store(level selfprotect.Level) selfprotect.AssetStatus {
	return selfprotect.AssetStatus{Asset: selfprotect.AssetStore, Level: level, Provider: "user", Detail: "/home/u/audit.db"}
}

func payloadOf(t *testing.T, e *events.Event) events.TamperPayload {
	t.Helper()
	var p events.TamperPayload
	require.NoError(t, json.Unmarshal(e.Payload, &p))
	return p
}

func TestTamperRecorder_RecordChanges(t *testing.T) {
	ctx := context.Background()
	rec, st := tamperFixture(t)

	clean := []selfprotect.AssetStatus{hookConfig("claude-code", selfprotect.LevelDetect, ""), store(selfprotect.LevelMediated)}
	recorded, err := rec.RecordChanges(ctx, clean)
	require.NoError(t, err)
	assert.Empty(t, recorded, "a clean state with no history records nothing")
	sess, err := st.GetSession(ctx, rec.SessionID())
	require.NoError(t, err)
	assert.Nil(t, sess, "no system session until the first event")

	drifted := []selfprotect.AssetStatus{hookConfig("claude-code", selfprotect.LevelDetect, "hooks not installed"), store(selfprotect.LevelMediated)}
	recorded, err = rec.RecordChanges(ctx, drifted)
	require.NoError(t, err)
	require.Len(t, recorded, 1)
	p := payloadOf(t, recorded[0])
	assert.Equal(t, events.TamperPayload{Operation: "drift", Asset: "hook_config", Agent: "claude-code", LevelBefore: "detect", LevelAfter: "detect",
		Drift: "hooks not installed", Provider: "user", Detail: "/home/u/.claude-code"}, p)
	assert.Equal(t, events.ActionTamper, recorded[0].ActionType)
	assert.Equal(t, session.SystemAgentName, recorded[0].AgentName)
	assert.Equal(t, rec.SessionID(), recorded[0].SessionID)

	sess, err = st.GetSession(ctx, rec.SessionID())
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, session.SystemAgentName, sess.AgentName)
	assert.Equal(t, "gryph-system:42", sess.AgentSessionID)

	recorded, err = rec.RecordChanges(ctx, drifted)
	require.NoError(t, err)
	assert.Empty(t, recorded, "the same drift again records nothing")

	changed := []selfprotect.AssetStatus{hookConfig("claude-code", selfprotect.LevelDetect, "hooks are invalid: PreToolUse"), store(selfprotect.LevelMediated)}
	recorded, err = rec.RecordChanges(ctx, changed)
	require.NoError(t, err)
	require.Len(t, recorded, 1)
	assert.Equal(t, "hooks are invalid: PreToolUse", payloadOf(t, recorded[0]).Drift)

	recorded, err = rec.RecordChanges(ctx, clean)
	require.NoError(t, err)
	require.Len(t, recorded, 1)
	p = payloadOf(t, recorded[0])
	assert.Empty(t, p.Drift)
	assert.Equal(t, "resolved", p.Operation)
	assert.Equal(t, "hook_config claude-code resolved: level detect to detect", p.Summary())

	lowered := []selfprotect.AssetStatus{hookConfig("claude-code", selfprotect.LevelMediated, ""), store(selfprotect.LevelNone)}
	recorded, err = rec.RecordChanges(ctx, lowered)
	require.NoError(t, err)
	require.Len(t, recorded, 1, "the store has no history, so only the hook config level change records")
	p = payloadOf(t, recorded[0])
	assert.Equal(t, "level", p.Operation)
	assert.Equal(t, "detect", p.LevelBefore)
	assert.Equal(t, "mediated", p.LevelAfter)

	rows, err := st.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: ptr(rec.SessionID()), Limit: -1})
	require.NoError(t, err)
	require.Len(t, rows, 4, "one receipt per recorded event")
	sort.Slice(rows, func(i, j int) bool { return rows[i].Sequence < rows[j].Sequence })
	for i, row := range rows {
		assert.Equal(t, int64(i+1), row.Sequence)
		assert.Equal(t, "tamper", row.ActionType)
		assert.Equal(t, receipt.DecisionTamper, row.Decision)
		assert.Equal(t, "recorded", row.ResultStatus)
		assert.Equal(t, session.SystemAgentName, row.Agent)
		assert.Equal(t, "hook_config", row.Tool)
	}
	assert.Equal(t, "hook_config claude-code drift: level detect to detect, hooks not installed", rows[0].Message)
	assert.Equal(t, "high", rows[0].Severity)
	assert.Equal(t, map[string]interface{}{"operation": "drift"}, rows[0].ActionPayload)
	assert.Equal(t, "info", rows[2].Severity)
	assert.Equal(t, "medium", rows[3].Severity)

	chain := make([]receipt.ChainRow, 0, len(rows))
	for _, row := range rows {
		chain = append(chain, receipt.ChainRowFromReceipt(row))
	}
	assert.Empty(t, receipt.VerifyChain(chain), "the existing verifier covers tamper receipts")
}

func TestTamperRecorder_Record_ReceiptFailure(t *testing.T) {
	st := storagetest.NewStore(t)
	rec := newTamperRecorder(st, receipt.NewSQLite(nil), "42")
	_, err := rec.Record(context.Background(), events.TamperPayload{Operation: "level", Asset: "store", LevelBefore: "mediated", LevelAfter: "none"})
	require.Error(t, err)
	assert.ErrorIs(t, err, receipt.ErrInsert)
}

func TestRuntime_TamperRecorder(t *testing.T) {
	rt, err := New(config.Default())
	require.NoError(t, err)
	_, err = rt.TamperRecorder()
	require.Error(t, err, "needs an open store")

	rt.Store = storagetest.NewStore(t)
	rec, err := rt.TamperRecorder()
	require.NoError(t, err)
	assert.NotEqual(t, [16]byte{}, [16]byte(rec.SessionID()))
}

func ptr[T any](v T) *T { return &v }
