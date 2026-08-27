package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// clearPathEnv neutralizes the environment that steers path resolution, so a
// test controls only the variables it sets.
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
	t.Setenv("HOME", t.TempDir())
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
