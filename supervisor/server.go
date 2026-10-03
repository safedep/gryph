// Package supervisor is the decision service that runs outside the agent
// user. It accepts the hook clients of every account on one socket, keys
// every partition on the peer uid that the kernel reports, and bounds what
// one account can ask of it. It imports the engine, never the CLI.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/platform/nofollow"
	"github.com/safedep/gryph/platform/peercred"
)

// ProviderName names the service in a tamper event.
const ProviderName = "supervisor"

var errRateLimited = errors.New("rate limited")

// Server serves the decision service on a listener.
type Server struct {
	cfg     *config.Config
	root    string
	limits  Limits
	version string

	mu         sync.Mutex
	rootDir    *nofollow.Dir
	partitions map[uint32]*partition
	wg         sync.WaitGroup
}

// Options configure a Server.
type Options struct {
	// StateDir holds the partitions. Empty takes the configured or the
	// platform default. The directory must exist: the service manager or
	// the command that starts the service creates it, never the server.
	StateDir string
	Limits   Limits
	// Version is what Welcome reports as the server version.
	Version string
}

// New builds a server for the managed configuration cfg.
func New(cfg *config.Config, opts Options) *Server {
	root := opts.StateDir
	if root == "" {
		root = cfg.Supervisor.StatePath()
	}
	limits := opts.Limits
	if limits.MaxConns <= 0 {
		limits = DefaultLimits()
	}
	return &Server{cfg: cfg, root: root, limits: limits, version: opts.Version, partitions: map[uint32]*partition{}}
}

// Serve accepts connections until ctx ends or the listener fails. The
// accept loop only accepts, reads the peer credentials and counts the
// connection: every request runs on its own goroutine, so a slow request
// never holds the next connection.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	root, err := nofollow.OpenDir(s.root, ".")
	if err != nil {
		return fmt.Errorf("state directory: %w", err)
	}
	s.mu.Lock()
	s.rootDir = root
	s.mu.Unlock()
	defer func() { _ = root.Close() }()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.wg.Wait()
				return s.closePartitions()
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			s.wg.Wait()
			_ = s.closePartitions()
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serveConn(ctx, conn)
		}()
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	peer, err := peercred.Open(conn)
	if err != nil {
		log.Warnf("supervisor: connection without peer credentials refused: %v", err)
		_ = ipc.WriteFrame(conn, ipc.ErrorFrame(ipc.CodeUnauthorized, "no peer credentials"))
		return
	}
	defer func() { _ = peer.Close() }()

	part, err := s.partition(ctx, peer.UID)
	if err != nil {
		log.Warnf("supervisor: partition of uid %d: %v", peer.UID, err)
		_ = ipc.WriteFrame(conn, ipc.ErrorFrame(ipc.CodeInternal, "partition not available"))
		return
	}
	if !part.acquireConn(s.limits.MaxConns) {
		part.recordRateLimit(ctx, "connection")
		_ = ipc.WriteFrame(conn, ipc.ErrorFrame(ipc.CodeRateLimited, "too many connections from this account"))
		return
	}
	defer part.releaseConn()

	welcome := ipc.Welcome{Proto: ipc.Proto, ServerVersion: s.version, Mode: s.cfg.Supervisor.EffectiveProfile()}
	rw := &idleConn{Conn: conn, timeout: s.limits.IdleTimeout}
	err = ipc.ServeConn(ctx, rw, welcome, ipc.HandlerFunc(func(ctx context.Context, f *ipc.Frame, body ipc.Body) (*ipc.Frame, error) {
		return s.dispatch(ctx, part, f, body)
	}))
	if err != nil && !errors.Is(err, io.EOF) && !isTimeout(err) {
		log.Debugf("supervisor: connection of uid %d ended: %v", peer.UID, err)
	}
}

// dispatch answers one frame for one partition.
func (s *Server) dispatch(ctx context.Context, part *partition, f *ipc.Frame, body ipc.Body) (*ipc.Frame, error) {
	switch b := body.(type) {
	case *ipc.Handle:
		return part.handle(ctx, b)
	case *ipc.ReportHookError:
		if err := part.reportHookError(ctx, b); err != nil {
			if errors.Is(err, errRateLimited) {
				return ipc.ErrorFrame(ipc.CodeRateLimited, "too many requests from this account"), nil
			}
			return nil, err
		}
		return nil, nil
	case *ipc.Hello:
		return ipc.ErrorFrame(ipc.CodeInvalid, "hello after the handshake"), nil
	default:
		return ipc.ErrorFrame(ipc.CodeUnsupported, fmt.Sprintf("%s is not served yet", f.Type)), nil
	}
}

// partition returns the partition of uid, and opens it on first contact.
func (s *Server) partition(ctx context.Context, uid uint32) (*partition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.partitions[uid]; ok {
		return p, nil
	}
	p, err := openPartition(ctx, s.cfg, s.rootDir, uid, s.limits)
	if err != nil {
		return nil, err
	}
	s.partitions[uid] = p
	return p, nil
}

func (s *Server) closePartitions() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for uid, p := range s.partitions {
		if err := p.close(); err != nil {
			errs = append(errs, fmt.Errorf("partition %d: %w", uid, err))
		}
		delete(s.partitions, uid)
	}
	return errors.Join(errs...)
}

// idleConn sets a read deadline before every read, so a peer that holds a
// connection and sends nothing lets it go after the idle timeout.
type idleConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleConn) Read(p []byte) (int, error) {
	if c.timeout > 0 {
		if err := c.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
			return 0, err
		}
	}
	return c.Conn.Read(p)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
