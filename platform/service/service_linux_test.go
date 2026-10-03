package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	spec := Spec{Name: "gryph-supervisor", Description: "Gryph decision service", Command: []string{"/opt/safedep/gryph/bin/gryph", "supervisor", "run"}, Socket: "/run/safedep/gryph/hook.sock", User: "_gryph", StateDir: "/var/lib/safedep/gryph", SpoolDir: "/var/spool/safedep/gryph"}
	socket := renderSocket(spec)
	assert.Contains(t, socket, "ListenStream=/run/safedep/gryph/hook.sock\n")
	assert.Contains(t, socket, "SocketUser=root\nSocketGroup=root\nSocketMode=0666\n")
	assert.Contains(t, socket, "Backlog=1024\n")
	assert.Contains(t, socket, "WantedBy=sockets.target\n")

	service := renderService(spec)
	for _, line := range []string{
		"ExecStart=/opt/safedep/gryph/bin/gryph supervisor run\n",
		"User=_gryph\n", "Restart=always\n", "StateDirectory=safedep/gryph\n",
		"ReadWritePaths=/var/spool/safedep/gryph\n",
		"NoNewPrivileges=yes\n", "CapabilityBoundingSet=\n", "ProtectSystem=strict\n", "ProtectHome=yes\n",
		"PrivateTmp=yes\n", "PrivateDevices=yes\n", "ProtectKernelTunables=yes\n", "ProtectKernelModules=yes\n",
		"ProtectKernelLogs=yes\n", "ProtectControlGroups=yes\n", "RestrictNamespaces=yes\n", "RestrictRealtime=yes\n",
		"RestrictSUIDSGID=yes\n", "LockPersonality=yes\n", "MemoryDenyWriteExecute=yes\n",
		"RestrictAddressFamilies=AF_UNIX\n", "SystemCallFilter=@system-service\n", "UMask=0077\n",
		"Requires=gryph-supervisor.socket\n",
	} {
		assert.Contains(t, service, line)
	}
	assert.NotContains(t, service, "ProtectProc", "the ancestor walk reads /proc of the peers")
	assert.Equal(t, `"a b"`, systemdQuote("a b"))
}

func TestInstallRemove_WritesUnits(t *testing.T) {
	dir := t.TempDir()
	restore := unitsDir
	unitsDir = func() string { return dir }
	t.Cleanup(func() { unitsDir = restore })
	spec := Spec{Name: "gryph-test-service", Description: "d", Command: []string{"/bin/true"}, Socket: "/tmp/x.sock", User: "nobody", StateDir: "/var/lib/x", SpoolDir: "/var/spool/x"}

	res, err := Install(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.Equal(t, []string{filepath.Join(dir, "gryph-test-service.socket"), filepath.Join(dir, "gryph-test-service.service")}, res.Paths)
	if !res.Enabled {
		assert.Contains(t, res.Next, "systemctl")
	}
	again, err := Install(context.Background(), spec)
	require.NoError(t, err)
	assert.False(t, again.Changed, "a second install changes nothing")

	removed, err := Remove(context.Background(), spec.Name)
	require.NoError(t, err)
	assert.True(t, removed.Changed)
	_, err = os.Stat(filepath.Join(dir, "gryph-test-service.service"))
	assert.ErrorIs(t, err, os.ErrNotExist)

	_, err = Install(context.Background(), Spec{Name: "bad name"})
	assert.Error(t, err)
}
