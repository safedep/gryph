package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/safedep/gryph/platform/launchd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSpec = Spec{Name: "gryph-supervisor", Description: "Gryph decision service", Command: []string{"/opt/safedep/gryph/bin/gryph", "supervisor", "run"}, Socket: "/var/db/safedep/gryph/hook.sock", User: "_gryph", StateDir: "/Library/Application Support/safedep/gryph-supervisor", SpoolDir: "/var/spool/safedep/gryph"}

// fakeLaunchd stands in for launchctl. It keeps whether the job is
// loaded, so print answers as launchd would.
type fakeLaunchd struct {
	loaded bool
	calls  []string
}

func withFakeLaunchd(t *testing.T) *fakeLaunchd {
	t.Helper()
	dir := t.TempDir()
	restoreDaemons, restoreLogs, restoreRun := daemonsDir, logDir, launchctl
	daemonsDir = func() string { return filepath.Join(dir, "LaunchDaemons") }
	logDir = func() string { return filepath.Join(dir, "Logs") }
	f := &fakeLaunchd{}
	launchctl = launchd.Launchctl{Run: func(_ context.Context, args ...string) error {
		f.calls = append(f.calls, args[0])
		switch args[0] {
		case "print":
			if !f.loaded {
				return errors.New("not loaded")
			}
		case "bootstrap":
			f.loaded = true
		case "bootout":
			f.loaded = false
		}
		return nil
	}}
	t.Cleanup(func() { daemonsDir, logDir, launchctl = restoreDaemons, restoreLogs, restoreRun })
	return f
}

func TestRenderDaemon(t *testing.T) {
	data, err := renderDaemon(testSpec)
	require.NoError(t, err)
	plist := string(data)
	for _, want := range []string{
		"<string>io.safedep.gryph-supervisor</string>",
		"<key>UserName</key>\n\t<string>_gryph</string>",
		"<key>GroupName</key>\n\t<string>_gryph</string>",
		"<key>KeepAlive</key>\n\t<true/>",
		"<key>ThrottleInterval</key>\n\t<integer>1</integer>",
		"<key>ProcessType</key>\n\t<string>Interactive</string>",
		"<key>Umask</key>\n\t<integer>63</integer>",
		"<key>Listeners</key>",
		"<key>SockPathName</key>\n\t\t\t<string>/var/db/safedep/gryph/hook.sock</string>",
		"<key>SockPathMode</key>\n\t\t\t<integer>438</integer>",
	} {
		assert.Contains(t, plist, want)
	}
}

func TestInstallRemove_Darwin(t *testing.T) {
	f := withFakeLaunchd(t)
	spec := testSpec
	spec.Socket = filepath.Join(t.TempDir(), "run", "hook.sock")
	ctx := context.Background()

	res, err := Install(ctx, spec)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.True(t, res.Enabled)
	path := filepath.Join(daemonsDir(), "io.safedep.gryph-supervisor.plist")
	assert.Equal(t, []string{path}, res.Paths)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	assert.DirExists(t, filepath.Dir(spec.Socket), "launchd makes no directory for the socket")
	assert.Equal(t, []string{"print", "bootstrap"}, f.calls)

	f.calls = nil
	res, err = Install(ctx, spec)
	require.NoError(t, err)
	assert.False(t, res.Changed)
	assert.True(t, res.Enabled)
	assert.Equal(t, []string{"print"}, f.calls, "an unchanged loaded job stays")

	f.calls = nil
	spec.Command = append(spec.Command, "--verbose")
	_, err = Install(ctx, spec)
	require.NoError(t, err)
	assert.Equal(t, []string{"print", "bootout", "bootstrap"}, f.calls, "a changed job file loads again")
	assert.True(t, Active(ctx, spec.Name))

	removed, err := Remove(ctx, spec.Name)
	require.NoError(t, err)
	assert.True(t, removed.Changed)
	assert.True(t, removed.Enabled)
	assert.NoFileExists(t, path)
	assert.False(t, Active(ctx, spec.Name))
}

func TestInstall_DarwinRefusesLinuxSettings(t *testing.T) {
	withFakeLaunchd(t)
	spec := testSpec
	spec.Capabilities = []string{"CAP_SYS_ADMIN"}
	_, err := Install(context.Background(), spec)
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestHints_Darwin(t *testing.T) {
	assert.Equal(t, "launchctl kill SIGHUP system/io.safedep.gryph-supervisor", ReloadHint("gryph-supervisor"))
	assert.True(t, strings.HasSuffix(LogHint("gryph-supervisor"), "/gryph-supervisor.log"))
}
