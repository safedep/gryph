package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withManagedConfigDir points the managed config location at dir and makes
// the trust check pass, because tests cannot create root owned files.
func withManagedConfigDir(t *testing.T, dir string) {
	t.Helper()

	restoreDir := globalConfigDirOverride
	restoreTrust := managedFileTrusted
	globalConfigDirOverride = dir
	managedFileTrusted = func(os.FileInfo) bool { return true }
	t.Cleanup(func() {
		globalConfigDirOverride = restoreDir
		managedFileTrusted = restoreTrust
	})
}

func TestManagedConfigFile(t *testing.T) {
	dir := t.TempDir()
	withManagedConfigDir(t, dir)

	assert.Empty(t, ManagedConfigFile())

	path := filepath.Join(dir, configFileName)
	require.NoError(t, os.WriteFile(path, []byte("logging:\n  level: full\n"), 0o644))
	assert.Equal(t, path, ManagedConfigFile())
}

func TestManagedConfigFile_UntrustedFileIgnored(t *testing.T) {
	dir := t.TempDir()
	withManagedConfigDir(t, dir)
	managedFileTrusted = func(os.FileInfo) bool { return false }

	path := filepath.Join(dir, configFileName)
	require.NoError(t, os.WriteFile(path, []byte("logging:\n  level: full\n"), 0o644))

	assert.Empty(t, ManagedConfigFile())
}

func TestLoad_ManagedConfigIsAuthoritative(t *testing.T) {
	clearPathEnv(t)

	userBase := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", userBase)
	userDir := filepath.Join(userBase, "safedep", "gryph")
	require.NoError(t, os.MkdirAll(userDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, configFileName),
		[]byte("logging:\n  level: minimal\n"), 0o600))

	managedDir := t.TempDir()
	withManagedConfigDir(t, managedDir)
	require.NoError(t, os.WriteFile(filepath.Join(managedDir, configFileName),
		[]byte("logging:\n  level: full\n"), 0o644))

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, LoggingFull, cfg.Logging.Level, "the managed file wins over the per-user file")

	explicit := filepath.Join(t.TempDir(), "explicit.yml")
	require.NoError(t, os.WriteFile(explicit, []byte("logging:\n  level: standard\n"), 0o600))

	cfg, err = Load(explicit)
	require.NoError(t, err)
	assert.Equal(t, LoggingFull, cfg.Logging.Level, "the managed file wins over an explicit --config path")
}

func TestLoad_ExplicitPathWinsWithoutManagedFile(t *testing.T) {
	clearPathEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	withManagedConfigDir(t, t.TempDir())

	explicit := filepath.Join(t.TempDir(), "explicit.yml")
	require.NoError(t, os.WriteFile(explicit, []byte("logging:\n  level: standard\n"), 0o600))

	cfg, err := Load(explicit)
	require.NoError(t, err)
	assert.Equal(t, LoggingStandard, cfg.Logging.Level)
}

func TestLoad_UserConfigWithoutManagedFile(t *testing.T) {
	clearPathEnv(t)

	userBase := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", userBase)
	userDir := filepath.Join(userBase, "safedep", "gryph")
	require.NoError(t, os.MkdirAll(userDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, configFileName),
		[]byte("logging:\n  level: minimal\n"), 0o600))

	withManagedConfigDir(t, t.TempDir())

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, LoggingMinimal, cfg.Logging.Level)
}

func TestLoad_GryphDirEnvDoesNotBindConfigValues(t *testing.T) {
	clearPathEnv(t)
	t.Setenv(configDirEnvKey, t.TempDir())
	t.Setenv(dataDirEnvKey, t.TempDir())
	t.Setenv(cacheDirEnvKey, t.TempDir())

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, Default(), cfg)
}

func TestVerifyManagedFileTrust(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows build accepts every file until an ACL check lands")
	}

	path := filepath.Join(t.TempDir(), configFileName)
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	info, err := os.Stat(path)
	require.NoError(t, err)

	if os.Geteuid() == 0 {
		assert.True(t, verifyManagedFileTrust(info))
	} else {
		assert.False(t, verifyManagedFileTrust(info))
	}

	require.NoError(t, os.Chmod(path, 0o664))
	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.False(t, verifyManagedFileTrust(info), "a group writable file is never trusted")
}
