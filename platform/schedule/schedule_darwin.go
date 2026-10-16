package schedule

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/safedep/gryph/platform/internal/fileutil"
	"github.com/safedep/gryph/platform/launchd"
)

var launchctl = launchd.Launchctl{Run: func(ctx context.Context, args ...string) error {
	return runCommand(ctx, "launchctl", args...)
}}

// agentsDir is the per-user launchd directory. Tests replace it.
var agentsDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// systemAgentsDir is the launchd directory whose agents load into every
// account's session at login. Tests replace it.
var systemAgentsDir = func() (string, error) { return "/Library/LaunchAgents", nil }

// installSystemWide writes the agent where launchd loads it for every
// account at login. A session that is already open loads it at its next
// login.
func installSystemWide(_ context.Context, job Job) (*Result, error) {
	dir, err := systemAgentsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	data, err := renderPlist(job)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, launchd.Label(job.Name)+".plist")
	changed, err := fileutil.WriteIfChanged(path, data)
	if err != nil {
		return nil, err
	}
	return &Result{Paths: []string{path}, Changed: changed, Enabled: true}, nil
}

func removeSystemWide(_ context.Context, name string) (*Result, error) {
	dir, err := systemAgentsDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, launchd.Label(name)+".plist")
	res := &Result{Enabled: true}
	switch err := os.Remove(path); {
	case err == nil:
		res.Paths = append(res.Paths, path)
		res.Changed = true
	case !errors.Is(err, fs.ErrNotExist):
		return res, err
	}
	return res, nil
}

func install(ctx context.Context, job Job) (*Result, error) {
	dir, err := agentsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	data, err := renderPlist(job)
	if err != nil {
		return nil, err
	}
	label := launchd.Label(job.Name)
	path := filepath.Join(dir, label+".plist")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	domain := launchd.GUIDomain(os.Getuid())
	res := &Result{Paths: []string{path}, Changed: true, Next: "launchctl bootstrap " + domain + " " + path}
	// A second bootstrap of a loaded agent fails, so take it out first.
	_ = launchctl.Bootout(ctx, domain, label)
	if err := launchctl.Bootstrap(ctx, domain, path); err != nil {
		return res, nil
	}
	res.Enabled = true
	return res, nil
}

func remove(ctx context.Context, name string) (*Result, error) {
	dir, err := agentsDir()
	if err != nil {
		return nil, err
	}
	label := launchd.Label(name)
	path := filepath.Join(dir, label+".plist")
	domain := launchd.GUIDomain(os.Getuid())
	res := &Result{Enabled: true}
	if err := launchctl.Bootout(ctx, domain, label); err != nil {
		res.Enabled = false
		res.Next = "launchctl bootout " + domain + "/" + label
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

func renderPlist(job Job) ([]byte, error) {
	return launchd.Dict{
		{Key: "Label", Value: launchd.Label(job.Name)},
		{Key: "ProgramArguments", Value: job.Command},
		{Key: "StartInterval", Value: job.minutes() * 60},
		{Key: "RunAtLoad", Value: true},
	}.Encode()
}
