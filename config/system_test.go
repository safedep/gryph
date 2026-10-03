package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withManagedConfigDir points the managed config location at dir and makes
// the trust check pass, because tests cannot create root owned files.
func withManagedConfigDir(t *testing.T, dir string) {
	t.Helper()

	restoreDir := globalConfigDirOverride
	restoreTrust := managedPathTrusted
	globalConfigDirOverride = dir
	managedPathTrusted = func(string) error { return nil }
	t.Cleanup(func() {
		globalConfigDirOverride = restoreDir
		managedPathTrusted = restoreTrust
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
	managedPathTrusted = func(string) error { return errors.New("not owned by root") }

	path := filepath.Join(dir, configFileName)
	require.NoError(t, os.WriteFile(path, []byte("logging:\n  level: full\n"), 0o644))

	assert.Empty(t, ManagedConfigFile())

	state := ManagedConfigStatus()
	assert.Equal(t, path, state.Path)
	assert.True(t, state.Exists)
	assert.EqualError(t, state.Err, "not owned by root")
}

func TestManagedConfigStatus_NoFile(t *testing.T) {
	dir := t.TempDir()
	withManagedConfigDir(t, dir)

	state := ManagedConfigStatus()
	assert.Equal(t, filepath.Join(dir, configFileName), state.Path)
	assert.False(t, state.Exists)
	assert.NoError(t, state.Err)
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

func TestLoad_ManagedConfigIgnoresEnv(t *testing.T) {
	clearPathEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	managedDir := t.TempDir()
	withManagedConfigDir(t, managedDir)
	require.NoError(t, os.WriteFile(filepath.Join(managedDir, configFileName),
		[]byte("logging:\n  level: full\npolicy:\n  enabled: true\n"), 0o644))

	t.Setenv("GRYPH_LOGGING_LEVEL", "minimal")
	t.Setenv("GRYPH_POLICY_ENABLED", "false")
	t.Setenv("GRYPH_STORAGE_RETENTION_DAYS", "7")
	t.Setenv("GRYPH_AGENTS_CURSOR_LOGGING_LEVEL", "minimal")

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, LoggingFull, cfg.Logging.Level, "env must not override a managed key")
	assert.True(t, cfg.Policy.Enabled, "env must not turn off managed policy")
	assert.Equal(t, Default().Storage.RetentionDays, cfg.Storage.RetentionDays,
		"env must not set a key that the managed file leaves unset")
	assert.Empty(t, cfg.Agents["cursor"].LoggingLevel, "env must not set an agent override")
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

func TestManagedPolicyState(t *testing.T) {
	dir := t.TempDir()
	withManagedConfigDir(t, dir)

	state := ManagedPolicyState()
	assert.False(t, state.Active, "no managed config file yet")
	assert.Equal(t, filepath.Join(dir, "policy.yaml"), state.File)
	assert.Equal(t, filepath.Join(dir, "policies"), state.Dir)
	require.NotNil(t, state.Trust)
	assert.NoError(t, state.Trust(filepath.Join(dir, "policy.yaml")), "the test trust check passes")

	require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), []byte("policy:\n  enabled: true\n"), 0o644))
	assert.True(t, ManagedPolicyState().Active)
	assert.True(t, ManagedConfigActive())
	assert.Equal(t, dir, ManagedConfigDir())
}

func TestLoad_AllowUserPolicyDefault(t *testing.T) {
	clearPathEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	withManagedConfigDir(t, t.TempDir())
	cfg, err := Load("")
	require.NoError(t, err)
	assert.True(t, cfg.Policy.AllowUserPolicy)
}
