package approval

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ttyPath is the path used by CLIPrompt to open a controlling terminal that
// bypasses the hook protocol pipes on stdin/stdout. Overridden in tests.
var ttyPath = "/dev/tty"

// ErrNoControllingTerminal is returned by CLIPrompt when /dev/tty cannot be
// opened. Callers should treat this as deny.
var ErrNoControllingTerminal = errors.New("approval: no controlling terminal available")

// CLIPrompt is an interactive approval Service that opens /dev/tty for
// read/write so the hook protocol's stdin/stdout pipes are not disturbed.
// On systems without a controlling terminal the service falls back to deny
// with ErrNoControllingTerminal.
type CLIPrompt struct {
	requireNote bool
	approver    string
	now         func() time.Time
	openTTY     func() (io.ReadWriteCloser, error)
}

// CLIPromptOption configures a CLIPrompt at construction time.
type CLIPromptOption func(*CLIPrompt)

// WithRequireNote forces the operator to enter a non-empty note when
// approving.
func WithRequireNote(require bool) CLIPromptOption {
	return func(c *CLIPrompt) { c.requireNote = require }
}

// WithApprover overrides the recorded approver identity. Defaults to the
// $USER environment variable.
func WithApprover(name string) CLIPromptOption {
	return func(c *CLIPrompt) {
		if strings.TrimSpace(name) != "" {
			c.approver = name
		}
	}
}

// NewCLIPrompt returns a CLIPrompt service.
func NewCLIPrompt(opts ...CLIPromptOption) *CLIPrompt {
	c := &CLIPrompt{
		approver: defaultApprover(),
		now:      func() time.Time { return time.Now().UTC() },
		openTTY: func() (io.ReadWriteCloser, error) {
			f, err := os.OpenFile(ttyPath, os.O_RDWR, 0)
			if err != nil {
				return nil, errors.Join(ErrNoControllingTerminal, err)
			}
			return f, nil
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Request implements Service. Opens /dev/tty, renders the request summary,
// reads a y/n response (and optional note) from the operator. Respects
// r.Timeout via context.WithTimeout: when the timeout fires before the
// operator answers, the outcome is DecisionTimeout.
func (c *CLIPrompt) Request(ctx context.Context, r *Request) (*Outcome, error) {
	if r == nil {
		return nil, fmt.Errorf("approval: nil request")
	}
	return c.Ask(ctx, ViewOf(r))
}

// Assurance implements Channel. The prompt runs on the developer's own
// terminal.
func (c *CLIPrompt) Assurance() Assurance { return AssuranceSameUserTTY }

// PromptView is what the terminal shows for one request. The decision
// service sends it on the wire, so it holds strings only.
type PromptView struct {
	// RequestID names the request in the store of the service. Empty for
	// a prompt that decides in process.
	RequestID string   `json:"request_id,omitempty"`
	Agent     string   `json:"agent,omitempty"`
	Tool      string   `json:"tool,omitempty"`
	Type      string   `json:"type,omitempty"`
	Path      string   `json:"path,omitempty"`
	Command   string   `json:"command,omitempty"`
	URL       string   `json:"url,omitempty"`
	Rules     []string `json:"rules,omitempty"`
	Message   string   `json:"message,omitempty"`
	// Wait is how long the prompt stays open.
	Wait time.Duration `json:"wait,omitempty"`
}

// ViewOf returns the view of r.
func ViewOf(r *Request) PromptView {
	v := PromptView{Wait: r.Timeout}
	if r.Action != nil {
		v.Agent = r.Action.Agent
		v.Tool = r.Action.Tool
		v.Type = string(r.Action.Type)
		v.Path = r.Action.Parameters.Path
		v.Command = r.Action.Parameters.Command
		v.URL = r.Action.Parameters.URL
	}
	if r.Rule != nil {
		v.Rules = r.Rule.MatchedRuleIDs
		v.Message = r.Rule.AgentMessage()
	}
	return v
}

// Ask puts one view to the operator and returns the answer. A wait of
// zero or less takes one minute.
func (c *CLIPrompt) Ask(ctx context.Context, v PromptView) (*Outcome, error) {
	tty, err := c.openTTY()
	if err != nil {
		return &Outcome{
			Decision:  DecisionDeny,
			Approver:  c.approver,
			Note:      "no controlling terminal: denying by default",
			DecidedAt: c.now(),
			Channel:   string(AssuranceSameUserTTY),
			Assurance: AssuranceSameUserTTY,
		}, err
	}
	defer func() { _ = tty.Close() }()

	c.render(tty, v)

	timeout := v.Wait
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	promptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	reader := bufio.NewReader(tty)

	resp, err := readLine(promptCtx, tty, reader)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return c.outcome(DecisionTimeout, fmt.Sprintf("approval timed out after %s", timeout)), nil
		}
		return nil, fmt.Errorf("approval: read response: %w", err)
	}

	if !parseApprove(resp) {
		return c.outcome(DecisionDeny, "operator denied"), nil
	}

	note := ""
	if c.requireNote {
		_, _ = fmt.Fprint(tty, "Note (required): ")
		n, nerr := readLine(promptCtx, tty, reader)
		if nerr != nil {
			if errors.Is(nerr, context.DeadlineExceeded) || errors.Is(nerr, context.Canceled) {
				return c.outcome(DecisionTimeout, fmt.Sprintf("approval timed out after %s", timeout)), nil
			}
			return nil, fmt.Errorf("approval: read note: %w", nerr)
		}
		note = strings.TrimSpace(n)
		if note == "" {
			return c.outcome(DecisionDeny, "operator approved but no note supplied (require_note=true)"), nil
		}
	}

	return c.outcome(DecisionApprove, note), nil
}

func (c *CLIPrompt) outcome(d Decision, note string) *Outcome {
	return &Outcome{
		Decision:  d,
		Approver:  c.approver,
		Note:      note,
		DecidedAt: c.now(),
		Channel:   string(AssuranceSameUserTTY),
		Assurance: AssuranceSameUserTTY,
	}
}

func (c *CLIPrompt) render(w io.Writer, v PromptView) {
	_, _ = fmt.Fprintln(w)
	if v.RequestID != "" {
		rules := strings.Join(v.Rules, ", ")
		if rules == "" {
			rules = "(none)"
		}
		_, _ = fmt.Fprintf(w, "gryph: approval needed for rule %s (request %s). Waiting up to %s.\n", rules, shortID(v.RequestID), v.Wait.Round(time.Second))
	}
	_, _ = fmt.Fprintln(w, "Gryph: action requires approval")
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 48))
	_, _ = fmt.Fprintf(w, "  agent:   %s\n", strOr(v.Agent, "(unknown)"))
	_, _ = fmt.Fprintf(w, "  tool:    %s\n", strOr(v.Tool, "(none)"))
	_, _ = fmt.Fprintf(w, "  type:    %s\n", v.Type)
	if v.Path != "" {
		_, _ = fmt.Fprintf(w, "  path:    %s\n", v.Path)
	}
	if v.Command != "" {
		_, _ = fmt.Fprintf(w, "  command: %s\n", v.Command)
	}
	if v.URL != "" {
		_, _ = fmt.Fprintf(w, "  url:     %s\n", v.URL)
	}
	if len(v.Rules) > 0 {
		_, _ = fmt.Fprintf(w, "  rules:   %s\n", strings.Join(v.Rules, ", "))
	}
	if v.Message != "" {
		_, _ = fmt.Fprintf(w, "  message: %s\n", v.Message)
	}
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 48))
	_, _ = fmt.Fprint(w, "Approve? [y/N]: ")
}

// shortID is the first eight characters of an id, as the CLI prints one.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func parseApprove(resp string) bool {
	r := strings.ToLower(strings.TrimSpace(resp))
	return r == "y" || r == "yes"
}

// readLine reads a line from r with respect to ctx. On context cancellation
// it closes closer to unblock a goroutine parked on the underlying read so
// the goroutine exits cleanly instead of leaking. The result channel is
// buffered so the goroutine's send never blocks on the slow path. Closing
// the same handle twice is safe (the outer deferred Close returns
// ErrClosed and is ignored).
func readLine(ctx context.Context, closer io.Closer, r *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			ch <- result{err: err}
			return
		}
		ch <- result{line: strings.TrimRight(line, "\r\n")}
	}()

	select {
	case <-ctx.Done():
		if closer != nil {
			_ = closer.Close()
		}
		return "", ctx.Err()
	case res := <-ch:
		return res.line, res.err
	}
}

func strOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// OSUsernameOrDefault returns the operator's login name from the conventional
// environment variables in order (USER, LOGNAME, USERNAME). Returns fallback
// if none are set. The USERNAME entry is the Windows convention. Exposed so
// the identity capturer and other call sites share the same env walk.
func OSUsernameOrDefault(fallback string) string {
	for _, key := range []string{"USER", "LOGNAME", "USERNAME"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return fallback
}

// DefaultOperatorIdentity returns the operator's login name from the
// conventional environment variables, falling back to "operator" when none
// are set. Exposed so the CLI deferral commands can reuse the same lookup
// the CLIPrompt service applies.
func DefaultOperatorIdentity() string {
	return OSUsernameOrDefault("operator")
}

func defaultApprover() string {
	return DefaultOperatorIdentity()
}

var _ Service = (*CLIPrompt)(nil)
