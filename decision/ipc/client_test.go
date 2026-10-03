//go:build !windows

package ipc

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveFake answers every connection with handler after the handshake, or
// holds the connection without a word when welcome is false.
func serveFake(t *testing.T, welcome bool, handler Handler) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				if !welcome {
					buf := make([]byte, 1)
					for {
						if _, err := conn.Read(buf); err != nil {
							return
						}
					}
				}
				_ = ServeConn(context.Background(), conn, Welcome{Proto: Proto, ServerVersion: "fake", Mode: "enforce"}, handler)
			}()
		}
	}()
	return sock
}

func TestDial_NoSocketIsConnect(t *testing.T) {
	_, err := Dial(context.Background(), filepath.Join(t.TempDir(), "none.sock"), DialOptions{})
	assert.ErrorIs(t, err, ErrConnect)
}

func TestDial_NoWelcomeIsConnect(t *testing.T) {
	sock := serveFake(t, false, nil)
	start := time.Now()
	_, err := Dial(context.Background(), sock, DialOptions{Handshake: 200 * time.Millisecond})
	assert.ErrorIs(t, err, ErrConnect)
	assert.Less(t, time.Since(start), time.Second, "the handshake budget bounds the wait")
}

func TestClient_HandleAndErrors(t *testing.T) {
	sock := serveFake(t, true, HandlerFunc(func(_ context.Context, f *Frame, body Body) (*Frame, error) {
		h := body.(*Handle)
		switch h.HookType {
		case "slow":
			time.Sleep(400 * time.Millisecond)
			return MustFrame(TypeDecision, Decision{Decision: "allow"}), nil
		case "limited":
			return ErrorFrame(CodeRateLimited, "too many"), nil
		case "odd":
			return MustFrame(TypeAck, Ack{}), nil
		}
		return MustFrame(TypeDecision, Decision{Decision: "block", Reason: "no"}), nil
	}))
	c, err := Dial(context.Background(), sock, DialOptions{Version: "t"})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	assert.Equal(t, "enforce", c.Welcome().Mode)

	d, err := c.Handle(context.Background(), Handle{Agent: "a", HookType: "x", RawPayload: []byte("{}")})
	require.NoError(t, err)
	assert.Equal(t, "block", string(d.Decision))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = c.Handle(ctx, Handle{Agent: "a", HookType: "slow", RawPayload: []byte("{}")})
	assert.ErrorIs(t, err, ErrDeadline)

	// The connection is out of step after a deadline, so a new one.
	c2, err := Dial(context.Background(), sock, DialOptions{})
	require.NoError(t, err)
	defer func() { _ = c2.Close() }()
	_, err = c2.Handle(context.Background(), Handle{Agent: "a", HookType: "limited", RawPayload: []byte("{}")})
	var se *ServerError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, CodeRateLimited, se.Code)

	_, err = c2.Handle(context.Background(), Handle{Agent: "a", HookType: "odd", RawPayload: []byte("{}")})
	assert.ErrorIs(t, err, ErrProtocol)
}
