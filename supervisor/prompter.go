package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safedep/gryph/decision/ipc"
)

// prompter puts one prompt on the connection of the hook that asked. The
// serve loop is in the handler while the prompt is open, so nothing else
// reads the connection. A reply counts once, on this connection, with the
// nonce of the prompt, before the deadline. When the connection closes
// first, the prompter goes with it, and the nonce never matches again.
type prompter struct {
	conn *idleConn
	wait time.Duration
	// audit is the login identity of the process that asked, for the
	// comparison with the one that answers later.
	audit string
}

type prompterKey struct{}

func withPrompter(ctx context.Context, pr *prompter) context.Context {
	return context.WithValue(ctx, prompterKey{}, pr)
}

func prompterFrom(ctx context.Context) *prompter {
	pr, _ := ctx.Value(prompterKey{}).(*prompter)
	return pr
}

// ask writes the prompt and reads the reply, until the deadline.
func (pr *prompter) ask(p *ipc.Prompt, deadline time.Time) (*ipc.PromptReply, error) {
	pr.conn.until = deadline
	defer func() { pr.conn.until = time.Time{} }()
	frame, err := ipc.NewFrame(ipc.TypePrompt, p)
	if err != nil {
		return nil, err
	}
	if err := ipc.WriteFrame(pr.conn, frame); err != nil {
		return nil, fmt.Errorf("prompt not sent: %w", err)
	}
	reply, err := ipc.ReadFrame(pr.conn)
	if err != nil {
		return nil, fmt.Errorf("no prompt reply: %w", err)
	}
	body, err := ipc.Decode(reply)
	if err != nil {
		return nil, fmt.Errorf("prompt reply: %w", err)
	}
	answer, ok := body.(*ipc.PromptReply)
	if !ok {
		return nil, errors.New("prompt reply: the client sent " + reply.Type)
	}
	if answer.Nonce != p.Nonce {
		return nil, errors.New("prompt reply: the nonce is not the one of the prompt")
	}
	return answer, nil
}
