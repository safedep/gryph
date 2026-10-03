package cli

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/internal/version"
	"github.com/safedep/gryph/platform/procs"
	"github.com/spf13/cobra"
)

// waitStep is how often the hidden wait commands look again.
const waitStep = 50 * time.Millisecond

// newSupervisorReadyCmd is the hidden health check of the acceptance
// scripts. It dials the socket until the service answers hello with
// welcome, then waits until every --until-exists path exists and every
// --until-empty directory holds no entry. It looks again every 50 ms until
// --timeout, so a script states the condition it waits for instead of a
// fixed sleep.
func newSupervisorReadyCmd() *cobra.Command {
	var (
		socket     string
		timeout    time.Duration
		untilExist []string
		untilEmpty []string
	)
	cmd := &cobra.Command{
		Use:    "ready",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if socket == "" {
				return ErrConfig("invalid flags", errNoSocket)
			}
			deadline := time.Now().Add(timeout)
			if err := awaitWelcome(socket, deadline); err != nil {
				return WrapError(ExitGeneral, "the service does not answer", err)
			}
			if err := awaitPaths(untilExist, untilEmpty, deadline); err != nil {
				return WrapError(ExitGeneral, "the condition did not come", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "the service socket")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "how long to wait in total")
	cmd.Flags().StringArrayVar(&untilExist, "until-exists", nil, "also wait until this path exists")
	cmd.Flags().StringArrayVar(&untilEmpty, "until-empty", nil, "also wait until this directory holds no entry")
	return cmd
}

// awaitWelcome completes one handshake with the service, and retries the
// dial and the handshake until the deadline while the socket is not there
// or the service does not answer yet.
func awaitWelcome(socket string, deadline time.Time) error {
	for {
		err := handshake(socket, deadline)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(waitStep)
	}
}

func handshake(socket string, deadline time.Time) error {
	conn, err := dialWithRetry(socket, time.Until(deadline))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(deadline)
	if err := ipc.WriteFrame(conn, ipc.MustFrame(ipc.TypeHello, ipc.Hello{Proto: ipc.Proto, ClientVersion: version.Version})); err != nil {
		return err
	}
	reply, err := ipc.ReadFrame(conn)
	if err != nil {
		return err
	}
	if reply.Type != ipc.TypeWelcome {
		return fmt.Errorf("first frame %q, not welcome", reply.Type)
	}
	return nil
}

// awaitPaths waits until every path in exist exists and every directory in
// empty holds no entry, or the deadline passes.
func awaitPaths(exist, empty []string, deadline time.Time) error {
	for {
		missing := pendingPaths(exist, empty)
		if len(missing) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New(strings.Join(missing, ", "))
		}
		time.Sleep(waitStep)
	}
}

func pendingPaths(exist, empty []string) []string {
	var pending []string
	for _, p := range exist {
		if _, err := os.Lstat(p); err != nil {
			pending = append(pending, p+" does not exist")
		}
	}
	for _, dir := range empty {
		entries, err := os.ReadDir(dir)
		if err != nil {
			pending = append(pending, dir+": "+err.Error())
			continue
		}
		if len(entries) > 0 {
			pending = append(pending, fmt.Sprintf("%s holds %d entry(ies)", dir, len(entries)))
		}
	}
	return pending
}

// newSupervisorStopCmd is the hidden stop of the acceptance scripts. It
// sends SIGTERM, or SIGKILL with --kill, to the pid in --pid-file and
// waits until that process has exited. A pid file that does not exist is not an error: the service
// already left. With --user it then kills every process of that account
// and waits until none runs, so the script can remove the account. The
// harness that started a process reaps it later, so an exited process
// that is not reaped yet counts as gone.
func newSupervisorStopCmd() *cobra.Command {
	var (
		pidFile string
		users   []string
		kill    bool
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:    "stop",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pidFile == "" && len(users) == 0 {
				return ErrConfig("invalid flags", errors.New("--pid-file or --user is required"))
			}
			deadline := time.Now().Add(timeout)
			if pidFile != "" {
				sig := syscall.SIGTERM
				if kill {
					sig = syscall.SIGKILL
				}
				if err := stopPIDFile(pidFile, sig, deadline); err != nil {
					return err
				}
			}
			for _, name := range users {
				if err := stopUser(name, deadline); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&pidFile, "pid-file", "", "the pid file of the service")
	cmd.Flags().StringArrayVar(&users, "user", nil, "also kill every process of this account")
	cmd.Flags().BoolVar(&kill, "kill", false, "send SIGKILL instead of SIGTERM, so the service cannot drain")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "how long to wait for the exit")
	return cmd
}

func stopPIDFile(pidFile string, sig syscall.Signal, deadline time.Time) error {
	data, err := os.ReadFile(pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return WrapError(ExitGeneral, "read the pid file", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return WrapError(ExitGeneral, "read the pid file", err)
	}
	if err := signalPID(pid, sig); err != nil {
		return WrapError(ExitGeneral, "stop the service", err)
	}
	for procs.Running(pid) {
		if time.Now().After(deadline) {
			return NewCLIError(ExitGeneral, fmt.Sprintf("pid %d still runs", pid))
		}
		time.Sleep(waitStep)
	}
	return nil
}

// stopUser kills the processes of the account until none runs. A process
// that the first pass missed, because a parent made it meanwhile, falls to
// the next pass.
func stopUser(name string, deadline time.Time) error {
	u, err := user.Lookup(name)
	var unknown user.UnknownUserError
	if errors.As(err, &unknown) {
		return nil
	}
	if err != nil {
		return WrapError(ExitGeneral, "find the account", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return WrapError(ExitGeneral, "find the account", err)
	}
	for {
		list, err := procs.ListUID(uid)
		if err != nil {
			return WrapError(ExitGeneral, "list the processes of "+name, err)
		}
		if len(list) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return NewCLIError(ExitGeneral, fmt.Sprintf("%d process(es) of %s still run", len(list), name))
		}
		for _, p := range list {
			_ = signalPID(p.PID, syscall.SIGKILL)
		}
		time.Sleep(waitStep)
	}
}

func signalPID(pid int, sig syscall.Signal) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := p.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
