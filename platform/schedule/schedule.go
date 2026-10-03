// Package schedule installs the per-user job that runs a Gryph command on an
// interval through the scheduler of the operating system: a systemd user
// timer on Linux, a launchd agent on macOS, a scheduled task on Windows. The
// job runs as the user, so it can only touch what the user can touch.
package schedule

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Job describes the scheduled command.
type Job struct {
	// Name is the file and unit name, for example gryph-reconcile.
	Name string
	// Description is one line for the scheduler's own listing.
	Description string
	// Command is the program by its absolute path, with its arguments.
	Command []string
	// Interval is the time between two runs. The scheduler rounds it to
	// whole minutes, and to at least one minute.
	Interval time.Duration
}

// Result is the outcome of an Install or a Remove.
type Result struct {
	// Paths lists the files of the job that the call wrote, kept as they
	// were, or removed.
	Paths []string
	// Changed is true when the call wrote or removed a file. A repeated
	// install of the same job changes nothing.
	Changed bool
	// Enabled is true when the scheduler took the change. When it is false
	// the files are in place and Next names the command to run by hand.
	Enabled bool
	Next    string
}

// Install writes the job and asks the scheduler to run it. A scheduler
// that refuses, for example a systemd user instance that is not running,
// does not fail the call: the files stay in place and Result.Next names
// the command to finish by hand.
func Install(ctx context.Context, job Job) (*Result, error) {
	if err := job.validate(); err != nil {
		return nil, err
	}
	return install(ctx, job)
}

// Remove stops the job and removes its files. A job that is not installed
// is not an error.
func Remove(ctx context.Context, name string) (*Result, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	return remove(ctx, name)
}

// InstallSystemWide writes the job once for every account of the host, as
// root: a systemd user unit under /etc/systemd/user enabled for every user
// manager, a launchd agent under /Library/LaunchAgents, or a scheduled task
// that runs for every member of the Users group. The scheduler of each
// account runs the job as that account, at login and on the interval, so
// no process of root ever touches a home. A logged-in account picks the
// job up at its next login, or when it reloads its scheduler by hand.
func InstallSystemWide(ctx context.Context, job Job) (*Result, error) {
	if err := job.validate(); err != nil {
		return nil, err
	}
	return installSystemWide(ctx, job)
}

// RemoveSystemWide removes the system-wide job. A job that is not installed
// is not an error.
func RemoveSystemWide(ctx context.Context, name string) (*Result, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	return removeSystemWide(ctx, name)
}

func (j Job) validate() error {
	if err := checkName(j.Name); err != nil {
		return err
	}
	if len(j.Command) == 0 || !filepath.IsAbs(j.Command[0]) {
		return errors.New("schedule: the command needs the absolute path of the program")
	}
	if j.Interval <= 0 {
		return errors.New("schedule: the interval must be positive")
	}
	return nil
}

func checkName(name string) error {
	if name == "" || strings.ContainsAny(name, `/\. `) {
		return fmt.Errorf("schedule: %q is not a job name", name)
	}
	return nil
}

// writeIfChanged writes data to path unless the file already holds it,
// so a repeated install changes nothing.
func writeIfChanged(path string, data []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// minutes returns the interval in whole minutes, at least one.
func (j Job) minutes() int {
	m := int(j.Interval / time.Minute)
	if m < 1 {
		return 1
	}
	return m
}

// runCommand runs a scheduler command and returns its combined output in
// the error. Tests replace it.
var runCommand = func(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
