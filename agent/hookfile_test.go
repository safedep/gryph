package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	restore := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = restore })
	return home
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need a privilege on Windows")
		}
		require.NoError(t, err)
	}
}

func TestHookFile_InstallWritesThroughLink(t *testing.T) {
	home := fakeHome(t)
	target := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(target, []byte(`{}`), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(home, ".claude"), 0o700))
	link := filepath.Join(home, ".claude", "settings.json")
	symlinkOrSkip(t, target, link)

	opts := InstallOptions{}
	data, err := ReadHookFile(link, opts)
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(data))

	require.NoError(t, WriteHookFile(link, []byte(`{"a":1}`), 0o600, opts))
	data, err = os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, string(data), "the install writes the file the link points to")
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link survives")
}

func TestHookFile_RepairRefusesLink(t *testing.T) {
	home := fakeHome(t)
	target := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(target, []byte(`{}`), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(home, ".claude"), 0o700))
	link := filepath.Join(home, ".claude", "settings.json")
	symlinkOrSkip(t, target, link)

	opts := InstallOptions{Repair: true}
	_, err := ReadHookFile(link, opts)
	assert.True(t, RepairRefused(err), "%v", err)
	err = WriteHookFile(link, []byte(`{"a":1}`), 0o600, opts)
	assert.True(t, RepairRefused(err), "%v", err)
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(data), "the target is untouched")

	symlinkOrSkip(t, t.TempDir(), filepath.Join(home, ".cursor"))
	err = WriteHookFile(filepath.Join(home, ".cursor", "hooks.json"), []byte(`{}`), 0o600, opts)
	assert.True(t, RepairRefused(err), "a linked directory is refused too: %v", err)
	err = EnsureHookDir(filepath.Join(home, ".cursor", "plugins"), 0o700, opts)
	assert.True(t, RepairRefused(err), "%v", err)
}

func TestHookFile_RepairWritesRegularFile(t *testing.T) {
	home := fakeHome(t)
	require.NoError(t, os.Mkdir(filepath.Join(home, ".claude"), 0o700))
	path := filepath.Join(home, ".claude", "settings.json")
	opts := InstallOptions{Repair: true}

	_, err := ReadHookFile(path, opts)
	assert.True(t, IsNotExist(err))
	require.NoError(t, WriteHookFile(path, []byte(`{"a":1}`), 0o600, opts))
	data, err := ReadHookFile(path, opts)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, string(data))

	require.NoError(t, EnsureHookDir(filepath.Join(home, ".claude", "plugins"), 0o700, opts))
	require.NoError(t, EnsureHookDir(filepath.Join(home, ".claude", "plugins"), 0o700, opts))
	info, err := os.Stat(filepath.Join(home, ".claude", "plugins"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestHookFile_OutsideHomeUsesParentAsBase(t *testing.T) {
	fakeHome(t)
	outside := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(outside, "agent"), 0o700))
	path := filepath.Join(outside, "agent", "hooks.json")
	opts := InstallOptions{Repair: true}
	require.NoError(t, WriteHookFile(path, []byte(`{}`), 0o600, opts))
	data, err := ReadHookFile(path, opts)
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(data))
}
