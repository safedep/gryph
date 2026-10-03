//go:build linux || darwin

package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/receipt"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/cost"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/spool"
	"github.com/safedep/gryph/storage"
	"github.com/safedep/gryph/storage/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readPayload = `{"session_id":"test-session-123","cwd":"/home/user/project","hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/home/user/project/main.go"},"tool_use_id":"tool-use-r1"}`

func startServer(t *testing.T, limits Limits) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "hook.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	cfg := config.Default()
	state := filepath.Join(dir, "state")
	require.NoError(t, os.Mkdir(state, 0o700))
	srv := New(cfg, Options{StateDir: state, Limits: limits, Version: "test", SpoolDir: filepath.Join(dir, "spool"), IngestInterval: -1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the server did not stop")
		}
	})
	return sock, state
}

// startServerWithSpool is startServer with the server in hand, for a test
// that runs a spool pass itself.
func startServerWithSpool(t *testing.T) (*Server, string, string) {
	t.Helper()
	return startServerWith(t, config.Default())
}

// startServerWith is startServerWithSpool for one configuration.
func startServerWith(t *testing.T, cfg *config.Config) (*Server, string, string) {
	t.Helper()
	return startServerWithLimits(t, cfg, Limits{})
}

// startServerWithLimits is startServerWith with limits of its own. A
// zero value takes the defaults.
func startServerWithLimits(t *testing.T, cfg *config.Config, limits Limits) (*Server, string, string) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "hook.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	state := filepath.Join(dir, "state")
	require.NoError(t, os.Mkdir(state, 0o700))
	spoolDir := filepath.Join(dir, "spool")
	require.NoError(t, spool.EnsureRoot(spoolDir))
	srv := New(cfg, Options{StateDir: state, Limits: limits, Version: "test", SpoolDir: spoolDir, IngestInterval: -1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the server did not stop")
		}
	})
	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.rootDir != nil
	}, 5*time.Second, 10*time.Millisecond)
	return srv, state, spoolDir
}

func dial(t *testing.T, sock string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func exchange(t *testing.T, conn net.Conn, f *ipc.Frame) *ipc.Frame {
	t.Helper()
	require.NoError(t, ipc.WriteFrame(conn, f))
	reply, err := ipc.ReadFrame(conn)
	require.NoError(t, err)
	return reply
}

func hello(t *testing.T, conn net.Conn) *ipc.Welcome {
	t.Helper()
	reply := exchange(t, conn, ipc.MustFrame(ipc.TypeHello, ipc.Hello{Proto: ipc.Proto, ClientVersion: "test"}))
	require.Equal(t, ipc.TypeWelcome, reply.Type, string(reply.Body))
	body, err := ipc.Decode(reply)
	require.NoError(t, err)
	return body.(*ipc.Welcome)
}

func TestServer_HandleInOwnPartition(t *testing.T) {
	sock, state := startServer(t, DefaultLimits())
	conn := dial(t, sock)
	welcome := hello(t, conn)
	assert.Equal(t, "enforce", welcome.Mode)
	assert.Equal(t, "test", welcome.ServerVersion)

	reply := exchange(t, conn, ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload), Project: decision.ProjectClaim{Name: "project"}}))
	require.Equal(t, ipc.TypeDecision, reply.Type, string(reply.Body))
	var d ipc.Decision
	require.NoError(t, json.Unmarshal(reply.Body, &d))
	assert.Equal(t, decision.Verdict("allow"), d.Decision)

	partition := filepath.Join(state, "users", strconv.Itoa(os.Getuid()))
	info, err := os.Stat(partition)
	require.NoError(t, err, "the partition of the peer uid exists")
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	_, err = os.Stat(filepath.Join(partition, "audit.db"))
	assert.NoError(t, err)

	reply = exchange(t, conn, ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "no-such-agent", HookType: "x", RawPayload: []byte("{}")}))
	assert.Equal(t, ipc.TypeError, reply.Type)
	assert.Contains(t, string(reply.Body), ipc.CodeInvalid)

	reply = exchange(t, conn, ipc.MustFrame(ipc.TypeReportHookError, decision.HookError{Agent: "claude-code", HookType: "PreToolUse", RawSize: 3, Message: "parse"}))
	assert.Equal(t, ipc.TypeAck, reply.Type)

	reply = exchange(t, conn, ipc.MustFrame(ipc.TypeQuery, ipc.Query{Kind: "sessions"}))
	assert.Equal(t, ipc.TypeQueryResult, reply.Type)
	assert.Contains(t, string(reply.Body), `"agent_name":"claude-code"`)

	reply = exchange(t, conn, ipc.MustFrame(ipc.TypePromptReply, ipc.PromptReply{Nonce: "n", Decision: "allow"}))
	assert.Equal(t, ipc.TypeError, reply.Type)
	assert.Contains(t, string(reply.Body), ipc.CodeUnsupported)
}

func TestServer_ConnectionLimit(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConns = 2
	sock, _ := startServer(t, limits)
	first := dial(t, sock)
	hello(t, first)
	second := dial(t, sock)
	hello(t, second)

	third := dial(t, sock)
	reply, err := ipc.ReadFrame(third)
	require.NoError(t, err, "the newest connection above the limit gets one error")
	assert.Equal(t, ipc.TypeError, reply.Type)
	assert.Contains(t, string(reply.Body), ipc.CodeRateLimited)

	require.NoError(t, first.Close())
	require.Eventually(t, func() bool {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return false
		}
		defer func() { _ = conn.Close() }()
		if err := ipc.WriteFrame(conn, ipc.MustFrame(ipc.TypeHello, ipc.Hello{Proto: ipc.Proto})); err != nil {
			return false
		}
		reply, err := ipc.ReadFrame(conn)
		return err == nil && reply.Type == ipc.TypeWelcome
	}, 2*time.Second, 20*time.Millisecond, "a closed connection frees its slot")
}

func TestServer_RequestRate(t *testing.T) {
	limits := DefaultLimits()
	limits.Rate = 1
	limits.Burst = 2
	sock, _ := startServer(t, limits)
	conn := dial(t, sock)
	hello(t, conn)
	handle := ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)})
	assert.Equal(t, ipc.TypeDecision, exchange(t, conn, handle).Type)
	assert.Equal(t, ipc.TypeDecision, exchange(t, conn, handle).Type)
	reply := exchange(t, conn, handle)
	assert.Equal(t, ipc.TypeError, reply.Type)
	assert.Contains(t, string(reply.Body), ipc.CodeRateLimited)
}

func TestBucket(t *testing.T) {
	now := time.Now()
	b := newBucket(2, 2, now)
	assert.True(t, b.take(now))
	assert.True(t, b.take(now))
	assert.False(t, b.take(now))
	assert.True(t, b.take(now.Add(500*time.Millisecond)), "half a second refills one token at two per second")
	assert.False(t, b.take(now.Add(500*time.Millisecond)))
	assert.True(t, b.take(now.Add(10*time.Second)))
	assert.True(t, b.take(now.Add(10*time.Second)))
	assert.False(t, b.take(now.Add(10*time.Second)), "the bucket never holds more than the burst")
}

func TestServer_StateDirectoryMustExist(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("unix", filepath.Join(dir, "hook.sock"))
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	srv := New(config.Default(), Options{StateDir: filepath.Join(dir, "missing"), Version: "test"})
	err = srv.Serve(context.Background(), ln)
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Contains(t, err.Error(), "state directory")
}

func TestServer_PartitionLinkRefused(t *testing.T) {
	sock, state := startServer(t, DefaultLimits())
	elsewhere := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(state, "users"), 0o700))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(state, "users", strconv.Itoa(os.Getuid()))))

	conn := dial(t, sock)
	reply, err := ipc.ReadFrame(conn)
	require.NoError(t, err)
	assert.Equal(t, ipc.TypeError, reply.Type)
	assert.Contains(t, string(reply.Body), ipc.CodeInternal)
	entries, err := os.ReadDir(elsewhere)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing was written through the link")
}

func TestServer_IngestsSpoolIntoTheOwnerPartition(t *testing.T) {
	srv, state, spoolDir := startServerWithSpool(t)
	me := strconv.Itoa(os.Getuid())
	handle := ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload), Project: decision.ProjectClaim{Name: "project"}})
	_, err := spool.Write(spoolDir, me, spool.Entry{Verdict: "allow", Reason: "ipc: cannot reach the decision service", Frame: handle})
	require.NoError(t, err)
	_, err = spool.Write(spoolDir, me, spool.Entry{Kind: spool.KindTamper, Verdict: "block", Reason: "socket is not owned by root"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(spoolDir, me, "9-stray.json"), []byte("nope"), 0o600))

	ctx := context.Background()
	require.NoError(t, srv.IngestOnce(ctx))
	entries, err := os.ReadDir(filepath.Join(spoolDir, me))
	require.NoError(t, err)
	assert.Empty(t, entries, "every file is taken or refused")

	srv.mu.Lock()
	part := srv.partitions[uint32(os.Getuid())]
	srv.mu.Unlock()
	require.NotNil(t, part, "the pass opened the partition of the owner")
	require.NoError(t, srv.closePartitions())

	store, err := storage.NewSQLiteStore(filepath.Join(state, "users", me, "audit.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: "unverified"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "claude-code", rows[0].Agent)
	assert.Equal(t, "recorded", rows[0].ResultStatus)
	assert.Contains(t, rows[0].Message, "client verdict allow without the decision service: ipc: cannot reach")

	system := session.SystemSessionID(me)
	tamper, err := store.GetEventsBySession(ctx, system)
	require.NoError(t, err)
	var ops []string
	for _, e := range tamper {
		var payload events.TamperPayload
		require.NoError(t, json.Unmarshal(e.Payload, &payload))
		ops = append(ops, payload.Operation)
		switch payload.Operation {
		case events.TamperServerIdentity:
			assert.Equal(t, "socket is not owned by root", payload.Detail)
		case events.TamperSpoolRefused:
			assert.Contains(t, payload.Detail, "1 file(s) refused: 9-stray.json (not an entry")
		case events.TamperDegraded:
			assert.Equal(t, "1 hook call(s) decided without the service while it was running", payload.Detail)
		}
	}
	assert.ElementsMatch(t, []string{events.TamperServerIdentity, events.TamperSpoolRefused, events.TamperDegraded}, ops)
}

func TestServer_SpoolBeforeStartIsNotDegraded(t *testing.T) {
	srv, state, spoolDir := startServerWithSpool(t)
	me := strconv.Itoa(os.Getuid())
	handle := ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)})
	_, err := spool.Write(spoolDir, me, spool.Entry{RecordedAt: srv.started.Add(-time.Minute), Verdict: "allow", Reason: "down", Frame: handle})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, srv.IngestOnce(ctx))
	require.NoError(t, srv.closePartitions())

	store, err := storage.NewSQLiteStore(filepath.Join(state, "users", me, "audit.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	tamper, err := store.GetEventsBySession(ctx, session.SystemSessionID(me))
	require.NoError(t, err)
	assert.Empty(t, tamper, "an entry from before the start is an outage, not a flood")
	rows, err := store.QueryReceipts(ctx, &storage.ReceiptFilter{Decision: "unverified"})
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}

func TestServer_SignsWithTheMachineKeyAndReloads(t *testing.T) {
	cfg := config.Default()
	cfg.Policy.Enabled = true
	cfg.Policy.LogAllEvaluations = true
	srv, state, _ := startServerWith(t, cfg)
	cfg.Supervisor.StateDir = state
	first, err := receipt.ReadPrivateKeyFile(cfg.Supervisor.ReceiptKeyPath())
	require.NoError(t, err, "the server made the machine key at start")

	sock := filepath.Join(filepath.Dir(state), "hook.sock")
	conn := dial(t, sock)
	hello(t, conn)
	reply := exchange(t, conn, ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}))
	require.Equal(t, ipc.TypeDecision, reply.Type, string(reply.Body))
	require.NoError(t, conn.Close())

	rotated, err := engine.RotateMachineKey(cfg, "rotation")
	require.NoError(t, err)
	srv.Reload()
	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.partitions) == 0
	}, 5*time.Second, 10*time.Millisecond)

	conn = dial(t, sock)
	hello(t, conn)
	reply = exchange(t, conn, ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}))
	require.Equal(t, ipc.TypeDecision, reply.Type, string(reply.Body))
	require.NoError(t, conn.Close())
	require.NoError(t, srv.closePartitions())

	store, err := storage.NewSQLiteStore(filepath.Join(state, "users", strconv.Itoa(os.Getuid()), "audit.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	rows, err := store.QueryReceipts(context.Background(), &storage.ReceiptFilter{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	var keyIDs []string
	for _, r := range rows {
		assert.Equal(t, receipt.KeyScopeSupervisor, r.SignerKeyScope)
		keyIDs = append(keyIDs, r.SignerKeyID)
	}
	assert.ElementsMatch(t, []string{first.KeyID, rotated.KeyID}, keyIDs, "the row before the reload carries the old key, the one after the new key")
}

func TestServer_ReadsComeFromTheOwnPartition(t *testing.T) {
	cfg := config.Default()
	cfg.Policy.Enabled = true
	cfg.Policy.LogAllEvaluations = true
	_, state, _ := startServerWithLimits(t, cfg, Limits{MaxConns: 16, Rate: 1000, Burst: 1000, IdleTimeout: 30 * time.Second})
	sock := filepath.Join(filepath.Dir(state), "hook.sock")

	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	reads := remote.New(client)
	ctx := context.Background()

	sessions, err := reads.QuerySessions(ctx, session.NewSessionFilter())
	require.NoError(t, err)
	assert.Empty(t, sessions, "nothing recorded yet")

	for i := range 70 {
		payload := fmt.Sprintf(`{"session_id":"test-session-123","cwd":"/home/user/project","hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/home/user/project/f%d.go"},"tool_use_id":"tu-%d"}`, i, i)
		d, err := client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(payload)})
		require.NoError(t, err)
		require.Equal(t, decision.Verdict("allow"), d.Decision)
	}

	sessions, err = reads.QuerySessions(ctx, session.NewSessionFilter())
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	sess := sessions[0]
	assert.Equal(t, "claude-code", sess.AgentName)

	evts, err := reads.GetEventsBySession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Len(t, evts, 70, "a long answer comes back over more than one page")
	n, err := reads.CountEvents(ctx, events.NewEventFilter())
	require.NoError(t, err)
	assert.Equal(t, 70, n)
	filter := events.NewEventFilter()
	filter.Limit = 5
	some, err := reads.QueryEvents(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, some, 5, "the filter of the client runs at the service")

	byID, err := reads.GetEvent(ctx, evts[0].ID)
	require.NoError(t, err)
	require.NotNil(t, byID)
	assert.Equal(t, evts[0].ID, byID.ID)
	byPrefix, err := reads.GetEventByPrefix(ctx, evts[0].ID.String()[:8])
	require.NoError(t, err)
	require.NotNil(t, byPrefix)
	missing, err := reads.GetEventByPrefix(ctx, "ffffffff")
	require.NoError(t, err)
	assert.Nil(t, missing, "no row is no error, as on the local store")
	bySess, err := reads.GetSessionByPrefix(ctx, sess.ID.String()[:8])
	require.NoError(t, err)
	require.NotNil(t, bySess)
	later, err := reads.QueryEventsAfter(ctx, evts[0].Timestamp, evts[0].ID, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, later)

	receipts, err := reads.QueryReceipts(ctx, &storage.ReceiptFilter{Limit: -1})
	require.NoError(t, err)
	assert.Len(t, receipts, 70)
	ids, err := reads.ListReceiptSessionIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{sess.ID}, ids)
	deferred, err := reads.QueryDeferredActions(ctx, &storage.DeferredActionFilter{})
	require.NoError(t, err)
	assert.Empty(t, deferred)
	state2, err := reads.GetContextStateByPrefix(ctx, sess.ID.String()[:8])
	require.NoError(t, err)
	assert.NotNil(t, state2)

	require.NoError(t, client.SessionCost(ctx, ipc.SessionCost{SessionID: sess.ID, Cost: &cost.SessionCost{SessionID: sess.ID, TotalCost: 1.5, Currency: "USD", Source: cost.CostSourceTranscript, Usage: cost.SessionUsage{InputTokens: 10}}}))
	sess, err = reads.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "client_reported:transcript", sess.CostSource)
	assert.Equal(t, int64(10), sess.InputTokens)
	err = client.SessionCost(ctx, ipc.SessionCost{SessionID: uuid.New(), Cost: &cost.SessionCost{Currency: "USD"}})
	assert.Error(t, err, "a session of no partition of this account is refused")

	_, err = client.Query(ctx, ipc.Query{Kind: "no-such-kind"})
	var serr *ipc.ServerError
	require.ErrorAs(t, err, &serr)
	assert.Equal(t, ipc.CodeInvalid, serr.Code)
}

func TestServer_ImportsTheUsersRowsMarked(t *testing.T) {
	cfg := config.Default()
	_, state, _ := startServerWithLimits(t, cfg, Limits{MaxConns: 16, Rate: 1000, Burst: 1000, IdleTimeout: 30 * time.Second})
	sock := filepath.Join(filepath.Dir(state), "hook.sock")
	client, err := ipc.Dial(context.Background(), sock, ipc.DialOptions{Version: "test"})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	ctx := context.Background()

	// The user's own database: one session with 70 events and one receipt.
	userDB := filepath.Join(t.TempDir(), "audit.db")
	local, err := storage.NewSQLiteStore(userDB)
	require.NoError(t, err)
	require.NoError(t, local.Init(ctx))
	sess := session.NewSessionWithID(uuid.New(), "claude-code")
	sess.ProjectName = "old-project"
	require.NoError(t, local.SaveSession(ctx, sess))
	for i := range 70 {
		ev := events.NewEvent(sess.ID, "claude-code", events.ActionFileRead)
		ev.ToolName = "Read"
		ev.ResultStatus = events.ResultSuccess
		require.NoError(t, local.RecordEvent(ctx, ev, session.EventCounts(ev)), i)
	}
	require.NoError(t, local.InsertReceipt(ctx, &storage.ReceiptRow{ID: uuid.New(), SessionID: sess.ID, Sequence: 1, RecordedAt: time.Now(), Agent: "claude-code", ActionType: "file_read", Decision: "allow", ResultStatus: "success", Hash: []byte("0123456789abcdef0123456789abcdef"), Signature: []byte("sig"), SignerKeyID: "0123456789abcdef", SignerKeyScope: "user"}))
	stored, err := local.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	evts, err := local.GetEventsBySession(ctx, sess.ID)
	require.NoError(t, err)
	receipts, err := local.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sess.ID, Limit: -1})
	require.NoError(t, err)
	require.NoError(t, local.Close())

	_, err = client.Import(ctx, ipc.TypeImportEvents, &ipc.ImportEvents{SessionID: sess.ID, Events: evts[:1]})
	require.Error(t, err, "the session row comes first")
	taken, err := client.Import(ctx, ipc.TypeImportSession, &ipc.ImportSession{Session: stored})
	require.NoError(t, err)
	assert.Equal(t, 1, taken)
	taken, err = client.Import(ctx, ipc.TypeImportSession, &ipc.ImportSession{Session: stored})
	require.NoError(t, err)
	assert.Equal(t, 0, taken, "a second import of the session changes nothing")
	send := func(evts []*events.Event) int {
		t.Helper()
		taken, err := client.Import(ctx, ipc.TypeImportEvents, &ipc.ImportEvents{SessionID: sess.ID, Events: evts})
		require.NoError(t, err)
		return taken
	}
	assert.Equal(t, 64, send(evts[:64]))
	assert.Equal(t, 6, send(evts[64:]))
	assert.Equal(t, 0, send(evts[:10]), "rows that are there stay")
	taken, err = client.Import(ctx, ipc.TypeImportReceipts, &ipc.ImportReceipts{SessionID: sess.ID, Receipts: receipts})
	require.NoError(t, err)
	assert.Equal(t, 1, taken)

	reads := remote.New(client)
	got, err := reads.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Imported)
	assert.Equal(t, "old-project", got.ProjectName)
	assert.Equal(t, 70, got.EventCount, "the counters of the user's row come along")
	gotEvents, err := reads.GetEventsBySession(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, gotEvents, 70)
	for _, e := range gotEvents {
		assert.True(t, e.Imported)
	}
	assert.Equal(t, evts[0].Sequence, gotEvents[0].Sequence)
	gotReceipts, err := reads.QueryReceipts(ctx, &storage.ReceiptFilter{SessionID: &sess.ID})
	require.NoError(t, err)
	require.Len(t, gotReceipts, 1)
	assert.True(t, gotReceipts[0].Imported)
	assert.Equal(t, []byte("sig"), gotReceipts[0].Signature, "the signature stays the user's")
	assert.Equal(t, "user", gotReceipts[0].SignerKeyScope)

	// A session the service recorded itself refuses the user's rows.
	d, err := client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)})
	require.NoError(t, err)
	require.Equal(t, decision.Verdict("allow"), d.Decision)
	own, err := reads.QuerySessions(ctx, session.NewSessionFilter())
	require.NoError(t, err)
	var ownID uuid.UUID
	for _, s := range own {
		if !s.Imported {
			ownID = s.ID
		}
	}
	require.NotEqual(t, uuid.Nil, ownID)
	ev := events.NewEvent(ownID, "claude-code", events.ActionFileRead)
	_, err = client.Import(ctx, ipc.TypeImportEvents, &ipc.ImportEvents{SessionID: ownID, Events: []*events.Event{ev}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recorded itself")
	wrong := events.NewEvent(uuid.New(), "claude-code", events.ActionFileRead)
	_, err = client.Import(ctx, ipc.TypeImportEvents, &ipc.ImportEvents{SessionID: sess.ID, Events: []*events.Event{wrong}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "belongs to session")
}
