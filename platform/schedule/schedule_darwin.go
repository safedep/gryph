package schedule

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const labelPrefix = "io.safedep."

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
	path := filepath.Join(dir, labelPrefix+job.Name+".plist")
	changed, err := writeIfChanged(path, []byte(renderPlist(job)))
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
	path := filepath.Join(dir, labelPrefix+name+".plist")
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
	path := filepath.Join(dir, labelPrefix+job.Name+".plist")
	if err := os.WriteFile(path, []byte(renderPlist(job)), 0o644); err != nil {
		return nil, err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	res := &Result{Paths: []string{path}, Changed: true, Next: "launchctl bootstrap " + domain + " " + path}
	// A second bootstrap of a loaded agent fails, so take it out first.
	_ = runCommand(ctx, "launchctl", "bootout", domain+"/"+labelPrefix+job.Name)
	if err := runCommand(ctx, "launchctl", "bootstrap", domain, path); err != nil {
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
	path := filepath.Join(dir, labelPrefix+name+".plist")
	domain := "gui/" + strconv.Itoa(os.Getuid())
	res := &Result{Enabled: true}
	if err := runCommand(ctx, "launchctl", "bootout", domain+"/"+labelPrefix+name); err != nil {
		res.Enabled = false
		res.Next = "launchctl bootout " + domain + "/" + labelPrefix + name
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

func renderPlist(job Job) string {
	var args strings.Builder
	for _, arg := range job.Command {
		var escaped strings.Builder
		_ = xml.EscapeText(&escaped, []byte(arg))
		fmt.Fprintf(&args, "\t\t<string>%s</string>\n", escaped.String())
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
	<key>StartInterval</key>
	<integer>%d</integer>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, labelPrefix+job.Name, args.String(), job.minutes()*60)
}
