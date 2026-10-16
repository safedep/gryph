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
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/localauth"
	"github.com/safedep/gryph/platform/nofollow"
	"github.com/safedep/gryph/platform/peercred"
	"github.com/safedep/gryph/platform/procs"
	"github.com/safedep/gryph/spool"
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

	spoolDir       string
	spoolLimits    spool.Limits
	ingestInterval time.Duration
	started        time.Time

	mu         sync.Mutex
	rootDir    *nofollow.Dir
	partitions map[uint32]*partition
	// conns counts the open connections of each account. It lives here,
	// not in a partition, so the limit holds across the replacement of a
	// partition whose open failed.
	conns     map[uint32]int
	wg        sync.WaitGroup
	approvers approverCache
	auth      localauth.Authorizer
	authCache authCache
	// agentNames are the programs of the known agents. ancestors walks
	// the parents of a peer.
	agentNames   []string
	ancestors    func(pid int) ([]procs.Process, error)
	lookup       func(pid int) []procs.Process
	reloadConfig func() (*config.Config, error)
	// base is the context of Serve. A partition opens under it, so the
	// open outlives the request that starts it and ends with the server.
	base context.Context
	// openHook runs before a partition opens, and its error fails the
	// open. It exists for a test, which sets it under mu.
	openHook func(ctx context.Context, uid uint32) error
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
	// SpoolDir is the spool the service reads. Empty takes the configured
	// or the platform default.
	SpoolDir string
	// SpoolLimits bound one pass over one account's spool. A zero value
	// takes the defaults.
	SpoolLimits spool.Limits
	// IngestInterval is the time between two passes over the spool. Zero
	// takes DefaultIngestInterval. A negative value turns the passes off.
	IngestInterval time.Duration
	// Authorizer is the authority that authenticates an approver. Nil
	// takes the one of the platform.
	Authorizer localauth.Authorizer
	// Ancestors walks the parents of a process, and Processes returns
	// the process with a pid when it runs. Both exist for a test. Nil
	// takes the platform.
	Ancestors func(pid int) ([]procs.Process, error)
	Processes func(pid int) []procs.Process
	// ReloadConfig reads the configuration again for Reload. Nil keeps
	// the configuration the server started with.
	ReloadConfig func() (*config.Config, error)
}

// New builds a server for the managed configuration cfg.
func New(cfg *config.Config, opts Options) *Server {
	root := opts.StateDir
	if root == "" {
		root = cfg.Supervisor.StatePath()
	}
	// Every partition reads the machine keys below the state directory in
	// force, so the configuration the partitions get names it.
	own := *cfg
	own.Supervisor.StateDir = root
	cfg = &own
	limits := opts.Limits
	if limits.MaxConns <= 0 {
		limits = DefaultLimits()
	}
	spoolDir := opts.SpoolDir
	if spoolDir == "" {
		spoolDir = cfg.Supervisor.SpoolPath()
	}
	spoolLimits := opts.SpoolLimits
	if spoolLimits.MaxFiles <= 0 {
		spoolLimits = spool.DefaultLimits()
	}
	interval := opts.IngestInterval
	if interval == 0 {
		interval = DefaultIngestInterval
	}
	auth := opts.Authorizer
	if auth == nil {
		auth = localauth.Default()
	}
	ancestors := opts.Ancestors
	if ancestors == nil {
		ancestors = procs.Ancestors
	}
	return &Server{
		cfg: cfg, root: root, limits: limits, version: opts.Version, reloadConfig: opts.ReloadConfig,
		spoolDir: spoolDir, spoolLimits: spoolLimits, ingestInterval: interval,
		partitions: map[uint32]*partition{}, conns: map[uint32]int{}, auth: auth,
		agentNames: agentProcessNames(cfg), ancestors: ancestors, lookup: opts.Processes,
	}
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
	keys, err := engine.EnsureMachineKeys(s.cfg)
	if err != nil {
		_ = root.Close()
		return fmt.Errorf("machine keys: %w", err)
	}
	if keys.Created {
		log.Warnf("supervisor: made the machine key %s at %s. Run `gryph install --managed` or `gryph supervisor keys rotate` as root to put its public half in the managed trust store", keys.KeyID, keys.ReceiptKey)
	}
	s.mu.Lock()
	s.rootDir = root
	s.base = ctx
	s.started = time.Now()
	s.mu.Unlock()
	defer func() { _ = root.Close() }()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	if s.ingestInterval > 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.ingestLoop(ctx)
		}()
	}
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

	// The connection counts in before the handshake, so the limit holds
	// for a connection that never sends a frame. The partition opens in
	// the background: a hook has a short handshake budget, and the first
	// open of an account creates and migrates its store. The first frame
	// waits for the open under the decision deadline.
	part, admitted := s.admit(peer.UID)
	if !admitted {
		_ = ipc.WriteFrame(conn, ipc.ErrorFrame(ipc.CodeRateLimited, "too many connections from this account"))
		// A refused connection closes at once, so a flood holds no
		// goroutine while the partition opens. The refusal is recorded
		// only in an open partition.
		if part.opened() {
			part.recordRateLimit(ctx, "connection")
		}
		return
	}
	defer s.leave(peer.UID, part)

	welcome := ipc.Welcome{Proto: ipc.Proto, ServerVersion: s.version, Mode: s.cfg.Supervisor.EffectiveProfile()}
	rw := &idleConn{Conn: conn, timeout: s.limits.IdleTimeout}
	err = ipc.ServeConn(ctx, rw, welcome, ipc.HandlerFunc(func(ctx context.Context, f *ipc.Frame, body ipc.Body) (*ipc.Frame, error) {
		if err := s.waitOpen(ctx, part, f.Type); err != nil {
			// The connection ends after this answer, so it does not hold
			// a slot of the account while the open is stuck, or past the
			// limit of the partition that replaces a failed one.
			rw.ended = true
			if errors.Is(err, context.DeadlineExceeded) {
				return ipc.ErrorFrame(ipc.CodeDeadline, "the partition did not open in time"), nil
			}
			return ipc.ErrorFrame(ipc.CodeInternal, "partition not available"), nil
		}
		return s.dispatch(ctx, part, peer, rw, f, body)
	}))
	if err != nil && !errors.Is(err, io.EOF) && !isTimeout(err) {
		log.Debugf("supervisor: connection of uid %d ended: %v", peer.UID, err)
	}
}

// dispatch answers one frame for one partition. conn is the connection
// of the frame, for a prompt that a decision needs, and peer the account
// and process behind it.
func (s *Server) dispatch(ctx context.Context, part *partition, peer *peercred.Peer, conn *idleConn, f *ipc.Frame, body ipc.Body) (*ipc.Frame, error) {
	switch b := body.(type) {
	case *ipc.Handle:
		audit, _ := peer.LoginIdentity()
		ancestor, found := s.agentAncestor(peer)
		return part.handle(withPrompter(ctx, &prompter{conn: conn, wait: b.InlineWait, audit: audit, ancestor: ancestor, found: found}), b)
	case *ipc.Approve:
		return s.approve(ctx, part, peer, b)
	case *ipc.PromptReply:
		// A reply reaches the loop only when no prompt is open: the open
		// prompt reads its reply itself. A nonce never counts twice.
		return ipc.ErrorFrame(ipc.CodeInvalid, "no prompt is open on this connection"), nil
	case *ipc.ReportHookError:
		if err := part.reportHookError(ctx, b); err != nil {
			if errors.Is(err, errRateLimited) {
				return ipc.ErrorFrame(ipc.CodeRateLimited, "too many requests from this account"), nil
			}
			return nil, err
		}
		return nil, nil
	case *ipc.Query:
		return part.query(ctx, b, func(ctx context.Context, q *ipc.Query) ([]any, bool, error) {
			return s.approverRead(ctx, part, peer, q)
		})
	case *ipc.SessionCost:
		if !part.bucket.take(time.Now()) {
			part.recordRateLimit(ctx, "session_cost")
			return ipc.ErrorFrame(ipc.CodeRateLimited, "too many requests from this account"), nil
		}
		if err := part.setSessionCost(ctx, b); err != nil {
			return ipc.ErrorFrame(ipc.CodeInvalid, err.Error()), nil
		}
		return nil, nil
	case *ipc.ImportEvents, *ipc.ImportReceipts, *ipc.ImportSession:
		if !part.bucket.take(time.Now()) {
			part.recordRateLimit(ctx, "import")
			return ipc.ErrorFrame(ipc.CodeRateLimited, "too many requests from this account"), nil
		}
		taken, err := part.importRows(ctx, body)
		if err != nil {
			return ipc.ErrorFrame(ipc.CodeInvalid, err.Error()), nil
		}
		return ipc.NewFrame(ipc.TypeImportResult, ipc.ImportResult{Taken: taken})
	case *ipc.Hello:
		return ipc.ErrorFrame(ipc.CodeInvalid, "hello after the handshake"), nil
	default:
		return ipc.ErrorFrame(ipc.CodeUnsupported, fmt.Sprintf("%s is not served yet", f.Type)), nil
	}
}

// waitOpen waits for the partition to open, at most OpenWait. A hook
// stops waiting for a decision after ipc.DecisionTimeout, so a Handle
// waits no longer, and its connection does not hold a slot of the account
// after the hook has gone.
func (s *Server) waitOpen(ctx context.Context, p *partition, frame string) error {
	wait := s.limits.OpenWait
	if wait <= 0 {
		wait = ipc.ReadTimeout
	}
	if frame == ipc.TypeHandle {
		wait = min(wait, ipc.DecisionTimeout)
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	return p.wait(ctx)
}

// admit counts a connection of uid in, and returns the partition that it
// uses. It reports false when the account has MaxConns connections.
func (s *Server) admit(uid uint32) (*partition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.partitionLocked(uid)
	if s.conns[uid] >= s.limits.MaxConns {
		return p, false
	}
	s.conns[uid]++
	p.holdConn()
	return p, true
}

// leave counts a connection of uid out.
func (s *Server) leave(uid uint32, p *partition) {
	s.mu.Lock()
	if s.conns[uid]--; s.conns[uid] <= 0 {
		delete(s.conns, uid)
	}
	s.mu.Unlock()
	p.releaseConn()
}

// partition returns the partition of uid, and opens it on first contact.
func (s *Server) partition(ctx context.Context, uid uint32) (*partition, error) {
	p := s.partitionOf(uid)
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// partitionOf returns the partition of uid, open or still opening. The
// first contact of an account starts the open in the background, outside
// the server lock, so a slow open holds no other account. A partition whose
// open failed gives way to a new one, so the next contact tries again.
func (s *Server) partitionOf(uid uint32) *partition {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.partitionLocked(uid)
}

// partitionLocked is partitionOf for a caller that holds mu.
func (s *Server) partitionLocked(uid uint32) *partition {
	if p, ok := s.partitions[uid]; ok && !p.failed() {
		return p
	}
	p := newPartition(uid, s.lookup)
	s.partitions[uid] = p
	ctx, cfg, root, limits, hook := s.base, s.cfg, s.rootDir, s.limits, s.openHook
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		p.open(func() error {
			if hook != nil {
				if err := hook(ctx, uid); err != nil {
					return err
				}
			}
			return p.load(ctx, cfg, root, limits)
		})
	}()
	return p
}

// Reload reads the configuration again and retires every open partition,
// so the next contact of an account opens a fresh one that reads the
// keys and the policy again. A partition with open connections closes
// when the last one ends. Rotation of the machine key and a managed
// install call it through a signal.
func (s *Server) Reload() {
	s.mu.Lock()
	if s.reloadConfig != nil {
		cfg, err := s.reloadConfig()
		if err != nil {
			log.Warnf("supervisor: the configuration did not load, the one in force stays: %v", err)
		} else {
			own := *cfg
			own.Supervisor.StateDir = s.cfg.Supervisor.StateDir
			s.cfg = &own
		}
	}
	var idle []*partition
	for uid, p := range s.partitions {
		if p.retire() {
			idle = append(idle, p)
		}
		delete(s.partitions, uid)
	}
	s.mu.Unlock()
	// A partition can still be opening, and close waits for the open, so
	// the closes run outside the server lock.
	for _, p := range idle {
		if err := p.close(); err != nil {
			log.Warnf("supervisor: close the partition of uid %d: %v", p.uid, err)
		}
	}
	log.Infof("supervisor: reloaded, partitions reopen on the next contact")
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
// connection and sends nothing lets it go after the idle timeout. An open
// prompt sets until, and the read then waits for the reply until then. A
// handler sets ended to end the connection after its answer.
type idleConn struct {
	net.Conn
	timeout time.Duration
	until   time.Time
	ended   bool
}

func (c *idleConn) Read(p []byte) (int, error) {
	switch {
	case c.ended:
		return 0, io.EOF
	case !c.until.IsZero():
		if err := c.SetReadDeadline(c.until); err != nil {
			return 0, err
		}
	case c.timeout > 0:
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
