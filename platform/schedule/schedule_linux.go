package schedule

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// unitsDir is the systemd user unit directory. Tests replace it.
var unitsDir = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user"), nil
}

// systemUnitsDir is the systemd unit directory that every user manager
// reads. Tests replace it.
var systemUnitsDir = func() (string, error) { return "/etc/systemd/user", nil }

// installSystemWide writes the units under the system user unit directory
// and enables the timer for every user manager with systemctl --global,
// which links it under timers.target.wants there. A running user manager
// picks the timer up on its next daemon-reload or login.
func installSystemWide(ctx context.Context, job Job) (*Result, error) {
	dir, err := systemUnitsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	service := filepath.Join(dir, job.Name+".service")
	timer := filepath.Join(dir, job.Name+".timer")
	wroteService, err := writeIfChanged(service, []byte(renderService(job)))
	if err != nil {
		return nil, err
	}
	wroteTimer, err := writeIfChanged(timer, []byte(renderTimer(job)))
	if err != nil {
		return nil, err
	}
	res := &Result{Paths: []string{service, timer}, Changed: wroteService || wroteTimer, Next: "systemctl --global enable " + job.Name + ".timer"}
	if err := runCommand(ctx, "systemctl", "--global", "enable", job.Name+".timer"); err != nil {
		return res, nil
	}
	res.Enabled = true
	return res, nil
}

func removeSystemWide(ctx context.Context, name string) (*Result, error) {
	dir, err := systemUnitsDir()
	if err != nil {
		return nil, err
	}
	res := &Result{Enabled: true}
	if err := runCommand(ctx, "systemctl", "--global", "disable", name+".timer"); err != nil {
		res.Enabled = false
		res.Next = "systemctl --global disable " + name + ".timer"
	}
	for _, path := range []string{
		filepath.Join(dir, "timers.target.wants", name+".timer"),
		filepath.Join(dir, name+".timer"),
		filepath.Join(dir, name+".service"),
	} {
		err := os.Remove(path)
		switch {
		case err == nil:
			res.Paths = append(res.Paths, path)
			res.Changed = true
		case !errors.Is(err, fs.ErrNotExist):
			return res, err
		}
	}
	return res, nil
}

func install(ctx context.Context, job Job) (*Result, error) {
	dir, err := unitsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	service := filepath.Join(dir, job.Name+".service")
	timer := filepath.Join(dir, job.Name+".timer")
	if err := os.WriteFile(service, []byte(renderService(job)), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(timer, []byte(renderTimer(job)), 0o644); err != nil {
		return nil, err
	}
	res := &Result{Paths: []string{service, timer}, Changed: true, Next: "systemctl --user enable --now " + job.Name + ".timer"}
	if err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return res, nil
	}
	if err := runCommand(ctx, "systemctl", "--user", "enable", "--now", job.Name+".timer"); err != nil {
		return res, nil
	}
	res.Enabled = true
	return res, nil
}

func remove(ctx context.Context, name string) (*Result, error) {
	dir, err := unitsDir()
	if err != nil {
		return nil, err
	}
	res := &Result{Enabled: true}
	if err := runCommand(ctx, "systemctl", "--user", "disable", "--now", name+".timer"); err != nil {
		res.Enabled = false
		res.Next = "systemctl --user daemon-reload"
	}
	for _, path := range []string{filepath.Join(dir, name+".timer"), filepath.Join(dir, name+".service")} {
		err := os.Remove(path)
		switch {
		case err == nil:
			res.Paths = append(res.Paths, path)
			res.Changed = true
		case !errors.Is(err, fs.ErrNotExist):
			return res, err
		}
	}
	if res.Enabled {
		if err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			res.Enabled = false
			res.Next = "systemctl --user daemon-reload"
		}
	}
	return res, nil
}

func renderService(job Job) string {
	return fmt.Sprintf(`[Unit]
Description=%s

[Service]
Type=oneshot
ExecStart=%s
`, job.Description, execStart(job.Command))
}

func renderTimer(job Job) string {
	return fmt.Sprintf(`[Unit]
Description=%s, every %d minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=%dmin
Persistent=true

[Install]
WantedBy=timers.target
`, job.Description, job.minutes(), job.minutes())
}

// execStart quotes each argument that holds a space or a quote, in the
// systemd form.
func execStart(command []string) string {
	parts := make([]string, 0, len(command))
	for _, arg := range command {
		if strings.ContainsAny(arg, " \t\"'\\") {
			arg = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(arg) + `"`
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}
