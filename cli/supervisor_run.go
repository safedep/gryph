package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/platform/listen"
	"github.com/safedep/gryph/supervisor"
	"github.com/spf13/cobra"
)

func newSupervisorRunCmd() *cobra.Command {
	var (
		socket    string
		stateDir  string
		allowRoot bool
		maxConns  int
		rate      float64
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the decision service for every account of this host",
		Long: `Run the decision service for every account of this host.

The service takes its socket from the service manager when it was started
with socket activation, else it opens --socket in the foreground. It keys
the partition of every connection on the account that the kernel reports
for the peer, under the state directory, and bounds what one account can
ask of it. It runs as the service account, never as root: --allow-root
exists for a test.`,
		Example: `  gryph supervisor run
  gryph supervisor run --socket /tmp/hook.sock --state-dir /tmp/gryph-state`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if utils.IsPrivileged() && !allowRoot {
				return NewCLIError(ExitGeneral, "supervisor run must not run as root: run it as the service account, or pass --allow-root for a test")
			}
			cfg, err := config.Load(globalFlags.ConfigPath)
			if err != nil {
				return ErrConfig("failed to load configuration", err)
			}
			ln, err := serviceListener(socket, cfg)
			if err != nil {
				return WrapError(ExitGeneral, "open the service socket", err)
			}
			limits := supervisor.DefaultLimits()
			if maxConns > 0 {
				limits.MaxConns = maxConns
			}
			if rate > 0 {
				limits.Rate = rate
				limits.Burst = int(rate * 2)
			}
			srv := supervisor.New(cfg, supervisor.Options{StateDir: stateDir, Limits: limits, Version: version.Version})

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			log.Infof("supervisor: serving on %s", ln.Addr())
			if err := srv.Serve(ctx, ln); err != nil {
				return WrapError(ExitGeneral, "supervisor", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "listen at this socket path instead of the one the service manager passes or the configured one")
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "hold the partitions here instead of the configured state directory")
	cmd.Flags().BoolVar(&allowRoot, "allow-root", false, "allow a run as root, for a test")
	cmd.Flags().IntVar(&maxConns, "max-conns", 0, "open connections per account")
	cmd.Flags().Float64Var(&rate, "rate", 0, "requests per second per account")
	return cmd
}

// serviceListener takes the socket from the service manager, or opens one.
func serviceListener(socket string, cfg *config.Config) (net.Listener, error) {
	if socket == "" {
		lns, err := listen.Activated()
		if err != nil {
			return nil, err
		}
		if len(lns) > 0 {
			for _, extra := range lns[1:] {
				_ = extra.Close()
			}
			return lns[0], nil
		}
		socket = cfg.Supervisor.SocketPath()
	}
	return listen.Open(socket)
}

// newSupervisorSendCmd is the hidden test seam of the socket: it sends the
// frames on stdin to the service and prints every reply. With --idle it
// first opens that many connections that send hello and then hold, so a
// test reaches the connection limit of the account.
func newSupervisorSendCmd() *cobra.Command {
	var (
		socket  string
		text    bool
		idle    int
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:    "send",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if socket == "" {
				return ErrConfig("invalid flags", errNoSocket)
			}
			var held []net.Conn
			defer func() {
				for _, c := range held {
					_ = c.Close()
				}
			}()
			for i := 0; i < idle; i++ {
				c, err := dialWithRetry(socket, timeout)
				if err != nil {
					return WrapError(ExitGeneral, "connect", err)
				}
				held = append(held, c)
				_ = c.SetDeadline(time.Now().Add(timeout))
				if err := ipc.WriteFrame(c, ipc.MustFrame(ipc.TypeHello, ipc.Hello{Proto: ipc.Proto, ClientVersion: version.Version})); err != nil {
					return WrapError(ExitGeneral, "hello", err)
				}
				if _, err := ipc.ReadFrame(c); err != nil {
					return WrapError(ExitGeneral, "welcome", err)
				}
			}

			conn, err := dialWithRetry(socket, timeout)
			if err != nil {
				return WrapError(ExitGeneral, "connect", err)
			}
			defer func() { _ = conn.Close() }()
			var out = cmd.OutOrStdout()
			if text {
				out = &frameLines{w: out}
			}
			for {
				f, err := ipc.ReadFrame(cmd.InOrStdin())
				if errors.Is(err, io.EOF) {
					return nil
				}
				if err != nil {
					return WrapError(ExitGeneral, "read a frame from stdin", err)
				}
				// The timeout bounds every exchange, so a service that holds
				// the connection without a word fails the test instead of
				// hanging it.
				_ = conn.SetDeadline(time.Now().Add(timeout))
				writeErr := ipc.WriteFrame(conn, f)
				reply, err := ipc.ReadFrame(conn)
				if err != nil {
					if writeErr != nil {
						return WrapError(ExitGeneral, "send", writeErr)
					}
					return WrapError(ExitGeneral, "reply", err)
				}
				if err := ipc.WriteFrame(out, reply); err != nil {
					return err
				}
				if writeErr != nil {
					return nil
				}
			}
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "the service socket")
	cmd.Flags().BoolVar(&text, "text", false, "print each reply frame as one JSON line")
	cmd.Flags().IntVar(&idle, "idle", 0, "open this many connections first and hold them")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "how long to wait for the socket")
	return cmd
}

var errNoSocket = errors.New("--socket is required")

// dialWithRetry connects to the socket, and retries until the timeout
// while the socket is not there yet, so a test can start the service in
// the background and connect at once.
func dialWithRetry(socket string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("unix", socket, time.Second)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}
