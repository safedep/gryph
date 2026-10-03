package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// HandshakeTimeout bounds connect plus welcome. It covers a service
// restart under socket activation, where the kernel accepts the connect
// while the service starts.
const HandshakeTimeout = 300 * time.Millisecond

// DecisionTimeout bounds one request after the handshake.
const DecisionTimeout = 2 * time.Second

var (
	// ErrConnect says the client did not complete the handshake: no socket,
	// a refused connect, or no welcome in time. It is the one error that
	// lets the hook fall back to the configured verdict for an absent
	// service.
	ErrConnect = errors.New("ipc: cannot reach the decision service")
	// ErrDeadline says the service took longer than the request deadline.
	ErrDeadline = errors.New("ipc: the decision service did not answer in time")
	// ErrProtocol says the service answered with a frame the client did not
	// expect.
	ErrProtocol = errors.New("ipc: unexpected answer from the decision service")
)

// ServerError is an Error frame the service sent.
type ServerError struct {
	Code    string
	Message string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("ipc: the decision service refused: %s: %s", e.Code, e.Message)
}

// DialOptions configure Dial.
type DialOptions struct {
	// Handshake bounds connect plus welcome. Zero takes HandshakeTimeout.
	Handshake time.Duration
	// Version is what Hello reports as the client version.
	Version string
	// VerifyServer runs on the open connection before hello. An error
	// from it ends the dial with ErrServerIdentity, never with ErrConnect:
	// a socket that is not the system's must block, not fall back.
	VerifyServer func(conn net.Conn) error
}

// ErrServerIdentity says the peer behind the socket is not the service.
var ErrServerIdentity = errors.New("ipc: the socket is not the decision service")

// Client is one connection to the decision service after the handshake.
type Client struct {
	conn    net.Conn
	welcome Welcome
}

// Dial connects to the service at socket and completes the handshake.
// Every failure before welcome is ErrConnect.
func Dial(ctx context.Context, socket string, opts DialOptions) (*Client, error) {
	budget := opts.Handshake
	if budget <= 0 {
		budget = HandshakeTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}
	if opts.VerifyServer != nil {
		if err := opts.VerifyServer(conn); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%w: %v", ErrServerIdentity, err)
		}
	}
	if err := WriteFrame(conn, MustFrame(TypeHello, Hello{Proto: Proto, ClientVersion: opts.Version})); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}
	reply, err := ReadFrame(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: no welcome: %v", ErrConnect, err)
	}
	body, err := Decode(reply)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: no welcome: %v", ErrConnect, err)
	}
	welcome, ok := body.(*Welcome)
	if !ok {
		_ = conn.Close()
		if e, isErr := body.(*Error); isErr {
			// The service answered, so it runs. A refusal before the
			// handshake is a server error, not an absent service.
			return nil, &ServerError{Code: e.Code, Message: e.Message}
		}
		return nil, fmt.Errorf("%w: first frame %q", ErrProtocol, reply.Type)
	}
	_ = conn.SetDeadline(time.Time{})
	return &Client{conn: conn, welcome: *welcome}, nil
}

// Welcome returns the frame the service answered the handshake with.
func (c *Client) Welcome() Welcome { return c.welcome }

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Handle asks for a decision and waits for it until the deadline of ctx.
// It returns ErrDeadline when the service does not answer in time, a
// ServerError when it refuses, and ErrProtocol when it answers with another
// frame.
func (c *Client) Handle(ctx context.Context, h Handle) (*Decision, error) {
	reply, err := c.call(ctx, MustFrame(TypeHandle, h))
	if err != nil {
		return nil, err
	}
	d, ok := reply.(*Decision)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrProtocol, reply)
	}
	return d, nil
}

// ReadTimeout bounds one read of the CLI over the socket. A read runs a
// query on the partition of the account, which can take longer than a
// decision, and nothing waits on it but the user.
const ReadTimeout = 10 * time.Second

// Query runs one read on the partition of the account and returns its
// rows. The deadline is the one of ctx, else ReadTimeout.
func (c *Client) Query(ctx context.Context, q Query) (*QueryResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ReadTimeout)
		defer cancel()
	}
	reply, err := c.call(ctx, MustFrame(TypeQuery, q))
	if err != nil {
		return nil, err
	}
	res, ok := reply.(*QueryResult)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrProtocol, reply)
	}
	return res, nil
}

// SessionCost sends the cost totals of a session, which the client read
// from the transcript, for the service to store as a claim.
func (c *Client) SessionCost(ctx context.Context, sc SessionCost) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ReadTimeout)
		defer cancel()
	}
	reply, err := c.call(ctx, MustFrame(TypeSessionCost, sc))
	if err != nil {
		return err
	}
	if _, ok := reply.(*Ack); !ok {
		return fmt.Errorf("%w: %T", ErrProtocol, reply)
	}
	return nil
}

// Import sends one import frame (import_events, import_receipts or
// import_session) and returns the count of rows the service took.
func (c *Client) Import(ctx context.Context, frameType string, body Body) (int, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ReadTimeout)
		defer cancel()
	}
	reply, err := c.call(ctx, MustFrame(frameType, body))
	if err != nil {
		return 0, err
	}
	res, ok := reply.(*ImportResult)
	if !ok {
		return 0, fmt.Errorf("%w: %T", ErrProtocol, reply)
	}
	return res.Taken, nil
}

// ReportHookError tells the service that the hook produced no decision.
func (c *Client) ReportHookError(ctx context.Context, r ReportHookError) error {
	reply, err := c.call(ctx, MustFrame(TypeReportHookError, r))
	if err != nil {
		return err
	}
	if _, ok := reply.(*Ack); !ok {
		return fmt.Errorf("%w: %T", ErrProtocol, reply)
	}
	return nil
}

// call sends one frame and decodes the answer under the deadline of ctx.
func (c *Client) call(ctx context.Context, f *Frame) (Body, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(DecisionTimeout)
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if err := WriteFrame(c.conn, f); err != nil {
		return nil, timeoutOr(err)
	}
	reply, err := ReadFrame(c.conn)
	if err != nil {
		return nil, timeoutOr(err)
	}
	body, err := Decode(reply)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	if e, isErr := body.(*Error); isErr {
		return nil, &ServerError{Code: e.Code, Message: e.Message}
	}
	return body, nil
}

func timeoutOr(err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ErrDeadline
	}
	return fmt.Errorf("%w: %v", ErrProtocol, err)
}
