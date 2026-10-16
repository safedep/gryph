//go:build linux || darwin

package supervisor

import (
	"context"
	"errors"
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

func setBeforeOpen(srv *Server, fn func(uid uint32) error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.beforeOpen = fn
}

func TestServer_WelcomeDoesNotWaitForTheOpen(t *testing.T) {
	srv, state, _ := startServerWith(t, config.Default())
	const delay = 3 * time.Second
	setBeforeOpen(srv, func(uint32) error {
		time.Sleep(delay)
		return nil
	})
	ctx := context.Background()
	start := time.Now()
	client, err := ipc.Dial(ctx, filepath.Join(filepath.Dir(state), "hook.sock"), ipc.DialOptions{Version: "test", Handshake: delay / 3, Decision: 30 * time.Second})
	require.NoError(t, err, "the welcome comes before the partition opens")
	defer func() { _ = client.Close() }()
	assert.Less(t, time.Since(start), delay)

	d, err := client.Handle(ctx, ipc.Handle{Agent: "claude-code", HookType: "PreToolUse", RawPayload: []byte(readPayload)}, nil)
	require.NoError(t, err, "the first request waits for the open")
	assert.Equal(t, decision.Verdict("allow"), d.Decision)
}

func TestServer_SlowOpenHoldsNoOtherAccount(t *testing.T) {
	srv, _, _ := startServerWith(t, config.Default())
	release := make(chan struct{})
	setBeforeOpen(srv, func(uid uint32) error {
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
	case <-slow:
		t.Fatal("the slow open ended before its release")
	default:
	}
	close(release)
	require.NoError(t, <-slow)
}

func TestServer_FailedOpenIsTriedAgain(t *testing.T) {
	srv, _, _ := startServerWith(t, config.Default())
	var calls atomic.Int32
	setBeforeOpen(srv, func(uint32) error {
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
