//go:build linux || darwin

package supervisor

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/decision/ipc"
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
	srv := New(cfg, Options{StateDir: state, Limits: limits, Version: "test"})
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
