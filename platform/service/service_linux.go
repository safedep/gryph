package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// unitsDir is the systemd system unit directory. Tests replace it.
var unitsDir = func() string { return "/etc/systemd/system" }

func install(ctx context.Context, spec Spec) (*Result, error) {
	dir := unitsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if spec.Socket == "" {
		return installService(ctx, dir, spec)
	}
	socket := filepath.Join(dir, spec.Name+".socket")
	service := filepath.Join(dir, spec.Name+".service")
	wroteSocket, err := writeIfChanged(socket, []byte(renderSocket(spec)))
	if err != nil {
		return nil, err
	}
	wroteService, err := writeIfChanged(service, []byte(renderService(spec)))
	if err != nil {
		return nil, err
	}
	res := &Result{Paths: []string{socket, service}, Changed: wroteSocket || wroteService, Next: "systemctl daemon-reload && systemctl enable --now " + spec.Name + ".socket"}
	if err := runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		res.Next += " (" + err.Error() + ")"
		return res, nil
	}
	if err := runCommand(ctx, "systemctl", "enable", "--now", spec.Name+".socket"); err != nil {
		res.Next += " (" + err.Error() + ")"
		return res, nil
	}
	if wroteService {
		// A changed service unit takes effect on the next start. The
		// socket stays open through the restart, so a hook waits for the
		// welcome instead of taking the fallback.
		if err := runCommand(ctx, "systemctl", "try-restart", spec.Name+".service"); err != nil {
			res.Next = "systemctl restart " + spec.Name + ".service (" + err.Error() + ")"
			return res, nil
		}
	}
	res.Enabled = true
	return res, nil
}

// installService writes a service with no socket and starts it. A changed
// unit restarts the service, which has no socket to hold its clients, so
// the caller picks a service that can restart at any time.
func installService(ctx context.Context, dir string, spec Spec) (*Result, error) {
	service := filepath.Join(dir, spec.Name+".service")
	wrote, err := writeIfChanged(service, []byte(renderService(spec)))
	if err != nil {
		return nil, err
	}
	res := &Result{Paths: []string{service}, Changed: wrote, Next: "systemctl daemon-reload && systemctl enable --now " + spec.Name + ".service"}
	if err := runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		res.Next += " (" + err.Error() + ")"
		return res, nil
	}
	if err := runCommand(ctx, "systemctl", "enable", "--now", spec.Name+".service"); err != nil {
		res.Next += " (" + err.Error() + ")"
		return res, nil
	}
	if wrote {
		if err := runCommand(ctx, "systemctl", "try-restart", spec.Name+".service"); err != nil {
			res.Next = "systemctl restart " + spec.Name + ".service (" + err.Error() + ")"
			return res, nil
		}
	}
	res.Enabled = true
	return res, nil
}

func remove(ctx context.Context, name string) (*Result, error) {
	dir := unitsDir()
	res := &Result{Enabled: true}
	if err := runCommand(ctx, "systemctl", "disable", "--now", name+".socket", name+".service"); err != nil {
		res.Enabled = false
		res.Next = "systemctl disable --now " + name + ".socket " + name + ".service"
	}
	for _, path := range []string{
		filepath.Join(dir, "sockets.target.wants", name+".socket"),
		filepath.Join(dir, "multi-user.target.wants", name+".service"),
		filepath.Join(dir, name+".socket"),
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
	if res.Enabled {
		if err := runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
			res.Enabled = false
			res.Next = "systemctl daemon-reload"
		}
	}
	return res, nil
}

func active(ctx context.Context, name string) bool {
	return runCommand(ctx, "systemctl", "is-active", "--quiet", name+".socket") == nil
}

// renderSocket is the socket unit. Root owns the socket and every account
// connects to it: the root-owned path chain is what a hook client
// verifies, and the service account never has to own anything under /run.
func renderSocket(spec Spec) string {
	return fmt.Sprintf(`[Unit]
Description=%s socket

[Socket]
ListenStream=%s
SocketUser=root
SocketGroup=root
SocketMode=0666
DirectoryMode=0755
Backlog=1024
RemoveOnStop=no

[Install]
WantedBy=sockets.target
`, spec.Description, spec.Socket)
}

// renderService is the service unit with the hardening set: no new
// privileges and no capability, a read-only file system but the state and
// the spool, no home (so the service cannot open a user's file even by
// mistake), no network but Unix sockets, and the system-service call
// filter. ProtectProc stays off: the service reads /proc/<pid> of its
// peers. The socket comes from the service manager through LISTEN_FDS.
func renderService(spec Spec) string {
	command := make([]string, 0, len(spec.Command))
	for _, part := range spec.Command {
		command = append(command, systemdQuote(part))
	}
	var unit, paths strings.Builder
	if spec.Socket != "" {
		fmt.Fprintf(&unit, "Requires=%s.socket\nAfter=%s.socket\n", spec.Name, spec.Name)
	}
	if spec.SpoolDir != "" {
		fmt.Fprintf(&paths, "ReadWritePaths=%s\n", spec.SpoolDir)
	}
	if spec.RuntimeDir != "" {
		fmt.Fprintf(&paths, "RuntimeDirectory=%s\nRuntimeDirectoryMode=0755\nRuntimeDirectoryPreserve=yes\n", spec.RuntimeDir)
	}
	caps := strings.Join(spec.Capabilities, " ")
	calls := strings.Join(append([]string{"@system-service"}, spec.SyscallAllow...), " ")
	return fmt.Sprintf(`[Unit]
Description=%s
%s
[Service]
Type=simple
ExecStart=%s
ExecReload=/bin/kill -HUP $MAINPID
User=%s
Group=%s
Restart=always
RestartSec=1
StateDirectory=%s
StateDirectoryMode=0750
%sNoNewPrivileges=yes
CapabilityBoundingSet=%s
AmbientCapabilities=%s
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictAddressFamilies=AF_UNIX
SystemCallFilter=%s
SystemCallErrorNumber=EPERM
UMask=0077

[Install]
WantedBy=multi-user.target
`, spec.Description, unit.String(), strings.Join(command, " "), spec.User, spec.User,
		strings.TrimPrefix(spec.StateDir, "/var/lib/"), paths.String(), caps, caps, calls)
}

// systemdQuote quotes one argument for a unit file.
func systemdQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

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
