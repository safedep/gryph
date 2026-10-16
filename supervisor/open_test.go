//go:build linux || darwin

package supervisor

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setOpenHook(srv *Server, fn func(ctx context.Context, uid uint32) error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.openHook = fn
}

// holdOpens makes every partition open wait until the test closes the
// returned channel.
func holdOpens(t *testing.T, srv *Server) chan struct{} {
	t.Helper()
	release := make(chan struct{})
	setOpenHook(srv, func(context.Context, uint32) error {
		<-release
		return nil
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	return release
}

func TestServer_WelcomeDoesNotWaitForTheOpen(t *testing.T) {
	srv, state, _ := startServerWith(t, config.Default())
	release := holdOpens(t, srv)

	ctx := context.Background()
	client, err := ipc.Dial(ctx, filepath.Join(filepath.Dir(state), "hook.sock"), ipc.DialOptions{Version: "test", Decision: 30 * time.Second})
	require.NoError(t, err, "the welcome comes within the hook budget while the partition opens")
	defer func() { _ = client.Close() }()

	close(release)
	d, err := client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}, nil)
	require.NoError(t, err, "the first request waits for the open")
	assert.Equal(t, decision.Verdict("allow"), d.Decision)
}

func TestServer_SlowOpenHoldsNoOtherAccount(t *testing.T) {
	srv, _, _ := startServerWith(t, config.Default())
	release := make(chan struct{})
	setOpenHook(srv, func(_ context.Context, uid uint32) error {
		if uid == 4242 {
			<-release
		}
		return nil
	})
	ctx := context.Background()
	slow := make(chan error, 1)
	go func() {
		_, err := srv.partition(ctx, 4242)
		slow <- err
	}()

	_, err := srv.partition(ctx, 4243)
	require.NoError(t, err, "another account opens while the first one waits")
	select {
	case err := <-slow:
		require.Failf(t, "the slow open ended before its release", "error: %v", err)
	default:
	}
	close(release)
	require.NoError(t, <-slow)
}

func TestServer_FailedOpenIsTriedAgain(t *testing.T) {
	srv, _, _ := startServerWith(t, config.Default())
	var calls atomic.Int32
	setOpenHook(srv, func(context.Context, uint32) error {
		if calls.Add(1) == 1 {
			return errors.New("disk full")
		}
		return nil
	})
	ctx := context.Background()
	_, err := srv.partition(ctx, 4244)
	require.ErrorContains(t, err, "disk full")

	p, err := srv.partition(ctx, 4244)
	require.NoError(t, err, "the next contact opens the partition again")
	assert.NotNil(t, p.rt)
}

func TestServer_FailedOpenEndsTheConnection(t *testing.T) {
	srv, state, _ := startServerWith(t, config.Default())
	setOpenHook(srv, func(context.Context, uint32) error { return errors.New("disk full") })

	conn := dial(t, filepath.Join(filepath.Dir(state), "hook.sock"))
	hello(t, conn)
	reply := exchange(t, conn, ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}))
	assert.Equal(t, ipc.TypeError, reply.Type)
	assert.Contains(t, string(reply.Body), ipc.CodeInternal)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err := ipc.ReadFrame(conn)
	var ne net.Error
	assert.False(t, errors.As(err, &ne) && ne.Timeout(), "the server ends the connection: %v", err)
}

func TestServer_RefusalDoesNotWaitForTheOpen(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConns = 1
	srv, state, _ := startServerWithLimits(t, config.Default(), limits)
	holdOpens(t, srv)
	sock := filepath.Join(filepath.Dir(state), "hook.sock")
	hello(t, dial(t, sock))

	refused := dial(t, sock)
	reply, err := ipc.ReadFrame(refused)
	require.NoError(t, err)
	assert.Contains(t, string(reply.Body), ipc.CodeRateLimited)
	require.NoError(t, refused.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = ipc.ReadFrame(refused)
	var ne net.Error
	assert.False(t, errors.As(err, &ne) && ne.Timeout(), "the refused connection closes while the partition opens: %v", err)
}

func TestServer_StuckOpenEndsTheRequest(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConns = 1
	limits.OpenWait = 200 * time.Millisecond
	srv, state, _ := startServerWithLimits(t, config.Default(), limits)
	setOpenHook(srv, func(ctx context.Context, _ uint32) error {
		<-ctx.Done()
		return ctx.Err()
	})
	sock := filepath.Join(filepath.Dir(state), "hook.sock")

	conn := dial(t, sock)
	hello(t, conn)
	reply := exchange(t, conn, ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}))
	assert.Contains(t, string(reply.Body), ipc.CodeDeadline, "the request does not wait past OpenWait")

	require.Eventually(t, func() bool {
		next, err := net.Dial("unix", sock)
		if err != nil {
			return false
		}
		defer func() { _ = next.Close() }()
		if err := ipc.WriteFrame(next, ipc.MustFrame(ipc.TypeHello, ipc.Hello{Proto: ipc.Proto})); err != nil {
			return false
		}
		reply, err := ipc.ReadFrame(next)
		return err == nil && reply.Type == ipc.TypeWelcome
	}, 5*time.Second, 20*time.Millisecond, "the ended request frees its slot while the open is stuck")
}

// TestServer_StopsWhileAPartitionOpens starts an open that ends only with
// the server. The cleanup of startServerWith fails the test when Serve does
// not return.
func TestServer_StopsWhileAPartitionOpens(t *testing.T) {
	srv, _, _ := startServerWith(t, config.Default())
	started := make(chan struct{})
	setOpenHook(srv, func(ctx context.Context, _ uint32) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	srv.partitionOf(4245)
	<-started
}

func TestServer_LimitHoldsAcrossAFailedOpen(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxConns = 2
	srv, state, _ := startServerWithLimits(t, config.Default(), limits)
	setOpenHook(srv, func(context.Context, uint32) error { return errors.New("disk full") })
	sock := filepath.Join(filepath.Dir(state), "hook.sock")
	hello(t, dial(t, sock))
	hello(t, dial(t, sock))
	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		p, ok := srv.partitions[uint32(os.Getuid())]
		return ok && p.failed()
	}, 5*time.Second, 10*time.Millisecond)

	reply, err := ipc.ReadFrame(dial(t, sock))
	require.NoError(t, err)
	assert.Contains(t, string(reply.Body), ipc.CodeRateLimited, "the replacement partition does not reset the count of the account")
}

func TestServer_HandleWaitsForTheOpenAsLongAsAHook(t *testing.T) {
	srv, state, _ := startServerWith(t, config.Default())
	holdOpens(t, srv)
	conn := dial(t, filepath.Join(filepath.Dir(state), "hook.sock"))
	hello(t, conn)

	start := time.Now()
	reply := exchange(t, conn, ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}))
	assert.Contains(t, string(reply.Body), ipc.CodeDeadline)
	assert.Less(t, time.Since(start), srv.limits.OpenWait, "a Handle waits the decision deadline of a hook, not OpenWait")
}
