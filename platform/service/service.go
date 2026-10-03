// Package service installs the decision service as a system service with
// a socket that the service manager holds: a systemd socket and service
// unit on Linux. The socket stays open across a restart or an upgrade of
// the service, so a hook that connects in that window waits for the
// welcome instead of taking the absent-service fallback. The service runs
// as the service account under the hardening set of the platform.
package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrUnsupported says that this platform has no service manager that the
// package drives.
var ErrUnsupported = errors.New("service: not supported on this platform")

// Spec describes the service.
type Spec struct {
	// Name is the unit name, for example gryph-supervisor.
	Name        string
	Description string
	// Command is the program by its absolute path, with its arguments.
	Command []string
	// Socket is the path of the socket the service manager opens for the
	// service. Every account connects to it. Empty gives a service with no
	// socket unit, which the service manager starts on its own.
	Socket string
	// User is the account the service runs as.
	User string
	// StateDir and SpoolDir are the directories the service writes. The
	// rest of the file system is read-only to it. SpoolDir may be empty.
	StateDir string
	SpoolDir string
	// Capabilities are the capabilities the service keeps, by name, for a
	// service that needs one. Empty keeps none.
	Capabilities []string
	// RuntimeDir is the directory under /run the service writes, relative
	// to /run. It stays in place when the service stops. Empty gives none.
	RuntimeDir string
	// SyscallAllow names the system calls the service needs beyond the
	// system-service set.
	SyscallAllow []string
}

// Result is the outcome of an Install or a Remove.
type Result struct {
	// Paths lists the unit files the call wrote, kept, or removed.
	Paths []string
	// Changed is true when the call wrote or removed a file.
	Changed bool
	// Enabled is true when the service manager took the change. When it is
	// false the files are in place and Next names the command to run by
	// hand.
	Enabled bool
	Next    string
}

// Install writes the units and asks the service manager to start the
// socket. A service manager that is not running does not fail the call:
// the files stay in place and Result.Next names the command to finish.
func Install(ctx context.Context, spec Spec) (*Result, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	return install(ctx, spec)
}

// Remove stops the service and the socket and removes the units. A
// service that is not installed is not an error.
func Remove(ctx context.Context, name string) (*Result, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	return remove(ctx, name)
}

// Active reports whether the service manager holds the socket of the
// service.
func Active(ctx context.Context, name string) bool {
	if err := checkName(name); err != nil {
		return false
	}
	return active(ctx, name)
}

func (s Spec) validate() error {
	if err := checkName(s.Name); err != nil {
		return err
	}
	if len(s.Command) == 0 || !strings.HasPrefix(s.Command[0], "/") {
		return errors.New("service: the command needs the absolute path of the program")
	}
	if s.Socket != "" && !strings.HasPrefix(s.Socket, "/") {
		return errors.New("service: the socket needs an absolute path")
	}
	if s.User == "" {
		return errors.New("service: the service needs an account")
	}
	if !strings.HasPrefix(s.StateDir, "/") || (s.SpoolDir != "" && !strings.HasPrefix(s.SpoolDir, "/")) {
		return errors.New("service: the state and spool directories need absolute paths")
	}
	for _, c := range s.Capabilities {
		if !strings.HasPrefix(c, "CAP_") || strings.ContainsAny(c, " \t\n") {
			return fmt.Errorf("service: %q is not a capability name", c)
		}
	}
	return nil
}

func checkName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\ \t\n") {
		return fmt.Errorf("service: %q is not a unit name", name)
	}
	return nil
}

// runCommand runs a program of the service manager. Its output is part of
// the error, so a caller can show why the manager refused.
func runCommand(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
