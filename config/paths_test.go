package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearPathEnv neutralizes the environment that steers path resolution, so a
// test controls only the variables it sets.
// setHome points the home directory at dir. os.UserHomeDir reads HOME on
// Unix and USERPROFILE on Windows.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func clearPathEnv(t *testing.T) {
	t.Helper()
	t.Setenv(configDirEnvKey, "")
	t.Setenv(dataDirEnvKey, "")
	t.Setenv(cacheDirEnvKey, "")
	t.Setenv("SUDO_USER", "")
}

func TestResolveDir_EnvOverrideWins(t *testing.T) {
	tests := []struct {
		name    string
		envKey  string
		resolve func() string
	}{
		{"config", configDirEnvKey, getConfigDir},
		{"data", dataDirEnvKey, getDataDir},
		{"cache", cacheDirEnvKey, getCacheDir},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearPathEnv(t)
			dir := t.TempDir()
			t.Setenv(tc.envKey, dir)

			assert.Equal(t, dir, tc.resolve())
		})
	}
}

func TestResolveDir_XDGHonoredOnAllPlatforms(t *testing.T) {
	tests := []struct {
		name    string
		xdgKey  string
		resolve func() string
	}{
		{"config", "XDG_CONFIG_HOME", getConfigDir},
		{"data", "XDG_DATA_HOME", getDataDir},
		{"cache", "XDG_CACHE_HOME", getCacheDir},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearPathEnv(t)
			base := t.TempDir()
			t.Setenv(tc.xdgKey, base)

			assert.Equal(t, filepath.Join(base, "safedep", "gryph"), tc.resolve())
		})
	}
}

func TestResolveDir_RelativeXDGIgnored(t *testing.T) {
	clearPathEnv(t)
	setHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "relative/path")

	got := getConfigDir()
	assert.True(t, filepath.IsAbs(got))
	assert.NotContains(t, got, "relative/path")
	assert.True(t, strings.HasSuffix(got, filepath.Join("safedep", "gryph")))
}

func withSudoElevation(t *testing.T, rootConfigBase string, rootErr error) {
	t.Helper()

	restoreGeteuid := configGeteuid
	restoreResolver := rootConfigDirResolver
	configGeteuid = func() int { return 0 }
	rootConfigDirResolver = func() (string, error) {
		if rootErr != nil {
			return "", rootErr
		}
		return rootConfigBase, nil
	}
	t.Cleanup(func() {
		configGeteuid = restoreGeteuid
		rootConfigDirResolver = restoreResolver
	})

	t.Setenv("SUDO_USER", "someone")
}

func TestResolveDir_SudoGuardUsesRootHome(t *testing.T) {
	clearPathEnv(t)
	rootBase := t.TempDir()
	withSudoElevation(t, rootBase, nil)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	assert.Equal(t, filepath.Join(rootBase, "safedep", "gryph"), getConfigDir())
}

func TestResolveDir_SudoGuardFallsBackWithoutPasswd(t *testing.T) {
	clearPathEnv(t)
	withSudoElevation(t, "", errors.New("no passwd database"))
	xdgBase := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgBase)

	assert.Equal(t, filepath.Join(xdgBase, "safedep", "gryph"), getConfigDir())
}

func TestResolveDir_NoSudoGuardWithoutSudoUser(t *testing.T) {
	clearPathEnv(t)
	rootBase := t.TempDir()
	withSudoElevation(t, rootBase, nil)
	t.Setenv("SUDO_USER", "")
	xdgBase := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgBase)

	assert.Equal(t, filepath.Join(xdgBase, "safedep", "gryph"), getConfigDir())
}

// withManagedPaths activates a managed config and fixes the account
// directories, so a test can show that the environment has no effect.
func withManagedPaths(t *testing.T, dirs baseDirs, resolveErr error) {
	t.Helper()
	managedDir := t.TempDir()
	withManagedConfigDir(t, managedDir)
	require.NoError(t, os.WriteFile(filepath.Join(managedDir, configFileName), []byte("logging:\n  level: full\n"), 0o644))

	restore := accountBaseDirsResolver
	accountBaseDirsResolver = func() (baseDirs, error) {
		if resolveErr != nil {
			return baseDirs{}, resolveErr
		}
		return dirs, nil
	}
	t.Cleanup(func() { accountBaseDirsResolver = restore })
}

func TestResolveDir_ManagedIgnoresEnvironment(t *testing.T) {
	clearPathEnv(t)
	account := baseDirs{config: t.TempDir(), data: t.TempDir(), cache: t.TempDir()}
	withManagedPaths(t, account, nil)

	stray := t.TempDir()
	setHome(t, stray)
	t.Setenv(configDirEnvKey, stray)
	t.Setenv(dataDirEnvKey, stray)
	t.Setenv(cacheDirEnvKey, stray)
	t.Setenv("XDG_CONFIG_HOME", stray)
	t.Setenv("XDG_DATA_HOME", stray)
	t.Setenv("XDG_CACHE_HOME", stray)

	assert.Equal(t, filepath.Join(account.config, "safedep", "gryph"), getConfigDir())
	assert.Equal(t, filepath.Join(account.data, "safedep", "gryph"), getDataDir())
	assert.Equal(t, filepath.Join(account.cache, "safedep", "gryph"), getCacheDir())
}

func TestResolveDir_ManagedFallsBackWithoutAccountDatabase(t *testing.T) {
	clearPathEnv(t)
	withManagedPaths(t, baseDirs{}, errors.New("no account database"))
	stray := t.TempDir()
	t.Setenv(configDirEnvKey, stray)
	t.Setenv("XDG_CONFIG_HOME", stray)

	got := getConfigDir()
	assert.NotEqual(t, stray, got, "the override has no effect")
	assert.True(t, strings.HasSuffix(got, filepath.Join("safedep", "gryph")))
}

func TestAccountBaseDirs_IgnoresHomeEnvironment(t *testing.T) {
	setHome(t, t.TempDir())

	dirs, err := accountBaseDirsResolver()
	if err != nil {
		t.Skipf("no account database: %v", err)
	}
	stray := os.Getenv("HOME")
	for _, dir := range []string{dirs.config, dirs.data, dirs.cache} {
		assert.True(t, filepath.IsAbs(dir))
		assert.False(t, strings.HasPrefix(dir, stray), "%s must not come from HOME", dir)
	}
}
