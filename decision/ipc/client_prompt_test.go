//go:build !windows

package ipc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A prompt in place of the decision goes to the prompt function, and its
// reply goes back on the same connection before the decision.
func TestClient_HandleAnswersPrompt(t *testing.T) {
	var got PromptReply
	sock := serveFake(t, true, HandlerFunc(func(_ context.Context, f *Frame, body Body) (*Frame, error) {
		switch b := body.(type) {
		case *Handle:
			return MustFrame(TypePrompt, Prompt{Nonce: "n1", ActionDigest: "sha256:x", Deadline: time.Now().Add(time.Second), RequestID: "r1"}), nil
		case *PromptReply:
			got = *b
			return MustFrame(TypeDecision, Decision{Decision: "allow"}), nil
		}
		return ErrorFrame(CodeInvalid, "unexpected"), nil
	}))
	c, err := Dial(context.Background(), sock, DialOptions{})
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	d, err := c.Handle(context.Background(), Handle{Agent: "a", HookType: "x", RawPayload: []byte("{}")}, func(ctx context.Context, p *Prompt) PromptReply {
		deadline, ok := ctx.Deadline()
		assert.True(t, ok)
		assert.WithinDuration(t, p.Deadline, deadline, time.Millisecond)
		return PromptReply{Nonce: "wrong", Decision: PromptApprove, Note: "yes"}
	})
	require.NoError(t, err)
	assert.Equal(t, "allow", string(d.Decision))
	assert.Equal(t, PromptReply{Nonce: "n1", Decision: PromptApprove, Note: "yes"}, got, "the client answers with the nonce of the prompt")

	// The prompt ends before the deadline of the exchange, so the reply fits.
	hctx, hcancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer hcancel()
	d, err = c.Handle(hctx, Handle{Agent: "a", HookType: "x", RawPayload: []byte("{}")}, func(ctx context.Context, p *Prompt) PromptReply {
		deadline, ok := ctx.Deadline()
		assert.True(t, ok)
		limit, _ := hctx.Deadline()
		assert.WithinDuration(t, limit.Add(-PromptReplyMargin), deadline, 5*time.Millisecond)
		<-ctx.Done()
		return PromptReply{Decision: PromptNone}
	})
	require.NoError(t, err)
	assert.Equal(t, "allow", string(d.Decision))

	// Without a prompt function the client answers none.
	d, err = c.Handle(context.Background(), Handle{Agent: "a", HookType: "x", RawPayload: []byte("{}")}, nil)
	require.NoError(t, err)
	assert.Equal(t, "allow", string(d.Decision))
	assert.Equal(t, PromptNone, got.Decision)
}
