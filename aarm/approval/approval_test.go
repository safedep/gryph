package approval

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNop_DeniesEverything(t *testing.T) {
	svc := NewNop()
	out, err := svc.Request(context.Background(), &Request{Timeout: 5 * time.Second})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, DecisionDeny, out.Decision)
	assert.Equal(t, "nop", out.Approver)
	assert.False(t, out.DecidedAt.IsZero())
}

type fakeTTY struct {
	r      io.Reader
	w      *strings.Builder
	closer io.Closer
}

func (f *fakeTTY) Read(p []byte) (int, error)  { return f.r.Read(p) }
func (f *fakeTTY) Write(p []byte) (int, error) { return f.w.Write(p) }
func (f *fakeTTY) Close() error {
	if f.closer != nil {
		return f.closer.Close()
	}
	return nil
}

func newFakeTTY(input string) *fakeTTY {
	return &fakeTTY{r: strings.NewReader(input), w: &strings.Builder{}}
}

func TestCLIPrompt_NoTTYFallsBackToDeny(t *testing.T) {
	svc := NewCLIPrompt()
	svc.openTTY = func() (io.ReadWriteCloser, error) {
		return nil, ErrNoControllingTerminal
	}

	out, err := svc.Request(context.Background(), &Request{Timeout: time.Second})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoControllingTerminal)
	require.NotNil(t, out)
	assert.Equal(t, DecisionDeny, out.Decision)
	assert.Contains(t, out.Note, "no controlling terminal")
}

func TestCLIPrompt_ParsesYesAndNo(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Decision
	}{
		{"y", "y\n", DecisionApprove},
		{"yes", "yes\n", DecisionApprove},
		{"Y uppercase", "Y\n", DecisionApprove},
		{"n", "n\n", DecisionDeny},
		{"empty", "\n", DecisionDeny},
		{"random", "maybe\n", DecisionDeny},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tty := newFakeTTY(tc.in)
			svc := NewCLIPrompt(WithApprover("tester"))
			svc.openTTY = func() (io.ReadWriteCloser, error) { return tty, nil }

			out, err := svc.Request(context.Background(), &Request{
				Action:  &model.Action{Type: model.ActionFileWrite, Tool: "Edit"},
				Timeout: 5 * time.Second,
			})
			require.NoError(t, err)
			require.NotNil(t, out)
			assert.Equal(t, tc.want, out.Decision)
			assert.Equal(t, "tester", out.Approver)
		})
	}
}

func TestCLIPrompt_TimesOut(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	tty := &fakeTTY{r: pr, w: &strings.Builder{}, closer: pr}
	svc := NewCLIPrompt()
	svc.openTTY = func() (io.ReadWriteCloser, error) { return tty, nil }

	start := time.Now()
	out, err := svc.Request(context.Background(), &Request{Timeout: 50 * time.Millisecond})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, DecisionTimeout, out.Decision)
	assert.True(t, time.Since(start) >= 50*time.Millisecond)

	done := make(chan struct{})
	go func() {
		_, _ = pr.Read(make([]byte, 1))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader goroutine did not unblock after timeout; tty close did not propagate")
	}
}

func TestCLIPrompt_RequireNote_ApproveWithEmptyNoteDenies(t *testing.T) {
	tty := newFakeTTY("y\n\n")
	svc := NewCLIPrompt(WithRequireNote(true), WithApprover("tester"))
	svc.openTTY = func() (io.ReadWriteCloser, error) { return tty, nil }

	out, err := svc.Request(context.Background(), &Request{Timeout: time.Second})
	require.NoError(t, err)
	assert.Equal(t, DecisionDeny, out.Decision)
	assert.Contains(t, out.Note, "no note supplied")
}

func TestCLIPrompt_RequireNote_ApproveWithNote(t *testing.T) {
	tty := newFakeTTY("y\nrubber-stamp\n")
	svc := NewCLIPrompt(WithRequireNote(true), WithApprover("tester"))
	svc.openTTY = func() (io.ReadWriteCloser, error) { return tty, nil }

	out, err := svc.Request(context.Background(), &Request{Timeout: time.Second})
	require.NoError(t, err)
	assert.Equal(t, DecisionApprove, out.Decision)
	assert.Equal(t, "rubber-stamp", out.Note)
}

func TestCLIPrompt_NilRequest(t *testing.T) {
	svc := NewCLIPrompt()
	out, err := svc.Request(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, out)
}

type erroringTTY struct{}

func (erroringTTY) Read([]byte) (int, error)  { return 0, errors.New("boom") }
func (erroringTTY) Write([]byte) (int, error) { return 0, nil }
func (erroringTTY) Close() error              { return nil }

func TestCLIPrompt_ReadError(t *testing.T) {
	svc := NewCLIPrompt()
	svc.openTTY = func() (io.ReadWriteCloser, error) { return erroringTTY{}, nil }

	out, err := svc.Request(context.Background(), &Request{Timeout: time.Second})
	require.Error(t, err)
	assert.Nil(t, out)
}

func TestOSUsernameOrDefault_EnvWalkOrder(t *testing.T) {
	t.Run("USER wins over LOGNAME and USERNAME", func(t *testing.T) {
		t.Setenv("USER", "user-val")
		t.Setenv("LOGNAME", "logname-val")
		t.Setenv("USERNAME", "username-val")
		assert.Equal(t, "user-val", OSUsernameOrDefault("fallback"))
	})

	t.Run("LOGNAME wins when USER is empty", func(t *testing.T) {
		t.Setenv("USER", "")
		t.Setenv("LOGNAME", "logname-val")
		t.Setenv("USERNAME", "username-val")
		assert.Equal(t, "logname-val", OSUsernameOrDefault("fallback"))
	})

	t.Run("USERNAME wins when USER and LOGNAME are empty (Windows case)", func(t *testing.T) {
		t.Setenv("USER", "")
		t.Setenv("LOGNAME", "")
		t.Setenv("USERNAME", "windows-user")
		assert.Equal(t, "windows-user", OSUsernameOrDefault("fallback"))
	})

	t.Run("fallback wins when all env vars are empty", func(t *testing.T) {
		t.Setenv("USER", "")
		t.Setenv("LOGNAME", "")
		t.Setenv("USERNAME", "")
		assert.Equal(t, "fallback", OSUsernameOrDefault("fallback"))
	})
}
