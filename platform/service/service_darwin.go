package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/safedep/gryph/platform/account"
	"github.com/safedep/gryph/platform/internal/fileutil"
	"github.com/safedep/gryph/platform/launchd"
)

// daemonsDir is where launchd reads the system daemons, and logDir where
// they write their output. logDir is under /var/log, which only root
// writes: /Library/Logs is writable by the admin group, and an admin user
// could plant a link where root makes the log. Tests replace both.
var (
	daemonsDir = func() string { return "/Library/LaunchDaemons" }
	logDir     = func() string { return "/var/log/safedep" }
)

// launchctl runs by its absolute path, because the install runs as root
// with the PATH of whoever started it.
var launchctl = launchd.Launchctl{Run: func(ctx context.Context, args ...string) error {
	return runCommand(ctx, "/bin/launchctl", args...)
}}

// bootstrapAttempts bounds the bootstrap after a bootout. launchd ends a
// job in the background, and a bootstrap of the same label before it has
// ended fails.
const bootstrapAttempts = 5

func install(ctx context.Context, spec Spec) (*Result, error) {
	if len(spec.Capabilities) > 0 || len(spec.SyscallAllow) > 0 || spec.RuntimeDir != "" {
		return nil, fmt.Errorf("%w: %s needs Linux kernel settings", ErrUnsupported, spec.Name)
	}
	label := launchd.Label(spec.Name)
	path := filepath.Join(daemonsDir(), label+".plist")
	data, err := renderDaemon(spec)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(daemonsDir(), 0o755); err != nil {
		return nil, err
	}
	if err := ensureLog(logPath(spec.Name), spec.User); err != nil {
		return nil, err
	}
	if spec.Socket != "" {
		// launchd binds the socket and makes no directory for it.
		if err := os.MkdirAll(filepath.Dir(spec.Socket), 0o755); err != nil {
			return nil, err
		}
	}
	changed, err := fileutil.WriteIfChanged(path, data)
	if err != nil {
		return nil, err
	}
	target := launchd.SystemDomain + " " + path
	res := &Result{Paths: []string{path}, Changed: changed, Next: "launchctl bootstrap " + target}
	loaded := launchctl.Loaded(ctx, launchd.SystemDomain, label)
	if loaded && !changed {
		res.Enabled = true
		return res, nil
	}
	if loaded {
		// launchd reads a job file at bootstrap only, so a changed file
		// loads again. The socket closes until the job is back, and a
		// hook in that window takes the fallback of the configuration.
		if err := launchctl.Bootout(ctx, launchd.SystemDomain, label); err != nil {
			res.Next = "launchctl bootout " + launchd.SystemDomain + "/" + label + " && launchctl bootstrap " + target + " (" + err.Error() + ")"
			return res, nil
		}
	}
	if err := bootstrap(ctx, path); err != nil {
		res.Next += " (" + err.Error() + ")"
		return res, nil
	}
	res.Enabled = true
	return res, nil
}

func bootstrap(ctx context.Context, path string) error {
	var err error
	for attempt := 0; attempt < bootstrapAttempts; attempt++ {
		if err = launchctl.Bootstrap(ctx, launchd.SystemDomain, path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
		}
	}
	return err
}

func remove(ctx context.Context, name string) (*Result, error) {
	label := launchd.Label(name)
	path := filepath.Join(daemonsDir(), label+".plist")
	res := &Result{Enabled: true}
	if launchctl.Loaded(ctx, launchd.SystemDomain, label) {
		if err := launchctl.Bootout(ctx, launchd.SystemDomain, label); err != nil {
			res.Enabled = false
			res.Next = "launchctl bootout " + launchd.SystemDomain + "/" + label
		}
	}
	switch err := os.Remove(path); {
	case err == nil:
		res.Paths = append(res.Paths, path)
		res.Changed = true
	case !errors.Is(err, fs.ErrNotExist):
		return res, err
	}
	return res, nil
}

func active(ctx context.Context, name string) bool {
	return launchctl.Loaded(ctx, launchd.SystemDomain, launchd.Label(name))
}

func reloadHint(name string) string {
	return "launchctl kill SIGHUP " + launchd.SystemDomain + "/" + launchd.Label(name)
}

func logHint(name string) string { return logPath(name) }

func logPath(name string) string { return filepath.Join(logDir(), name+".log") }

// ensureLog makes the log file of the job and gives it to the account of
// the job. launchd can open the file as that account, which cannot make
// a file in the root-owned log directory. Root opens the file without
// following a link and changes the owner through the open file, so a
// link in the directory cannot point root at another file.
func ensureLog(path, user string) error {
	acct, err := account.Lookup(user)
	if err != nil {
		return fmt.Errorf("the account of the job: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := checkLogDir(dir); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o640)
	if err != nil {
		return fmt.Errorf("log %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("log %s is not a regular file", path)
	}
	return f.Chown(int(acct.UID), int(acct.GID))
}

// checkLogDir checks the log directory. Tests, which run without root,
// replace it.
var checkLogDir = checkRootDir

// checkRootDir accepts a directory, not a link, that root owns and that no
// group or other can write.
func checkRootDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case !info.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	case !ok || st.Uid != 0:
		return fmt.Errorf("%s is not owned by root", dir)
	case info.Mode().Perm()&0o022 != 0:
		return fmt.Errorf("%s is writable by group or other", dir)
	}
	return nil
}

// renderDaemon is the job file. launchd opens the socket as root before
// the job starts and holds it while the job restarts, so a hook waits for
// the welcome. The job runs as the service account and restarts one
// second after it ends. ProcessType Interactive keeps launchd from
// slowing the job down, because a hook waits on it. macOS has no
// equivalent of the systemd hardening set: the service account is what
// keeps the job from the files of a user.
func renderDaemon(spec Spec) ([]byte, error) {
	dict := launchd.Dict{
		{Key: "Label", Value: launchd.Label(spec.Name)},
		{Key: "ProgramArguments", Value: spec.Command},
		{Key: "UserName", Value: spec.User},
		{Key: "GroupName", Value: spec.User},
		{Key: "RunAtLoad", Value: true},
		{Key: "KeepAlive", Value: true},
		{Key: "ThrottleInterval", Value: 1},
		{Key: "ProcessType", Value: "Interactive"},
		{Key: "Umask", Value: 0o077},
		{Key: "StandardOutPath", Value: logPath(spec.Name)},
		{Key: "StandardErrorPath", Value: logPath(spec.Name)},
	}
	if spec.Socket != "" {
		dict = append(dict, launchd.Entry{Key: "Sockets", Value: launchd.Dict{
			{Key: launchd.SocketName, Value: launchd.Dict{
				{Key: "SockPathName", Value: spec.Socket},
				{Key: "SockPathMode", Value: 0o666},
				{Key: "SockType", Value: "stream"},
				{Key: "SockFamily", Value: "Unix"},
			}},
		}})
	}
	return dict.Encode()
}
