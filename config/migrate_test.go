package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMigrationEnv isolates HOME and every XDG base in temp directories and
// returns the config and data bases.
func setupMigrationEnv(t *testing.T) (string, string) {
	t.Helper()
	clearPathEnv(t)

	configBase := t.TempDir()
	dataBase := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", configBase)
	t.Setenv("XDG_DATA_HOME", dataBase)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	return configBase, dataBase
}

func seedLegacyConfigTree(t *testing.T, configBase string) string {
	t.Helper()

	legacy := filepath.Join(configBase, legacyLeaf)
	require.NoError(t, os.MkdirAll(filepath.Join(legacy, "keys"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, legacyConfigFileName),
		[]byte("logging:\n  level: full\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "policy.yaml"),
		[]byte("version: 1\n"), 0o600))

	return legacy
}

func TestMigrateLegacyLayout_MovesLegacyTree(t *testing.T) {
	configBase, dataBase := setupMigrationEnv(t)

	legacyConfig := seedLegacyConfigTree(t, configBase)
	legacyData := filepath.Join(dataBase, legacyLeaf)
	require.NoError(t, os.MkdirAll(legacyData, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(legacyData, "audit.db"), []byte("db"), 0o600))
	legacyCache := legacyCacheDir()
	require.NoError(t, os.MkdirAll(legacyCache, 0o700))

	migrateLegacyLayout()

	newConfig := filepath.Join(configBase, "safedep", "gryph")
	newData := filepath.Join(dataBase, "safedep", "gryph")
	assert.NoDirExists(t, legacyConfig)
	assert.NoDirExists(t, legacyData)
	assert.NoDirExists(t, legacyCache)
	assert.FileExists(t, filepath.Join(newConfig, configFileName))
	assert.NoFileExists(t, filepath.Join(newConfig, legacyConfigFileName))
	assert.FileExists(t, filepath.Join(newConfig, "policy.yaml"))
	assert.DirExists(t, filepath.Join(newConfig, "keys"))
	assert.FileExists(t, filepath.Join(newData, "audit.db"))

	record := ConsumeMigrationMarker()
	require.NotNil(t, record)
	assert.Len(t, record.Moves, 2)
	assert.Empty(t, record.Warnings)
	assert.Nil(t, ConsumeMigrationMarker(), "the marker is consumed once")

	migrateLegacyLayout()
	assert.Nil(t, ConsumeMigrationMarker(), "a second run is a no-op")
}

func TestMigrateLegacyLayout_SamePairMovesOnce(t *testing.T) {
	clearPathEnv(t)
	base := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("XDG_DATA_HOME", base)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	seedLegacyConfigTree(t, base)

	migrateLegacyLayout()

	assert.FileExists(t, filepath.Join(base, "safedep", "gryph", configFileName))

	record := ConsumeMigrationMarker()
	require.NotNil(t, record)
	assert.Len(t, record.Moves, 1, "config and data resolve to one pair and move once")
}

func TestMigrateLegacyLayout_BothExistKeepsNew(t *testing.T) {
	configBase, _ := setupMigrationEnv(t)

	legacy := seedLegacyConfigTree(t, configBase)
	newConfig := filepath.Join(configBase, "safedep", "gryph")
	require.NoError(t, os.MkdirAll(newConfig, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(newConfig, configFileName),
		[]byte("logging:\n  level: minimal\n"), 0o600))

	migrateLegacyLayout()

	assert.DirExists(t, legacy, "the legacy directory is left for manual reconciliation")
	data, err := os.ReadFile(filepath.Join(newConfig, configFileName))
	require.NoError(t, err)
	assert.Contains(t, string(data), "minimal", "the new directory wins")
	assert.Nil(t, ConsumeMigrationMarker(), "no move means no marker")
}

func TestMigrateLegacyLayout_EnvOverrideSkipsMoveButNormalizesName(t *testing.T) {
	configBase, _ := setupMigrationEnv(t)
	legacy := seedLegacyConfigTree(t, configBase)

	override := t.TempDir()
	t.Setenv(configDirEnvKey, override)
	require.NoError(t, os.WriteFile(filepath.Join(override, legacyConfigFileName),
		[]byte("logging:\n  level: full\n"), 0o600))

	migrateLegacyLayout()

	assert.DirExists(t, legacy, "an override skips the move for that kind")
	assert.FileExists(t, filepath.Join(override, configFileName))
	assert.NoFileExists(t, filepath.Join(override, legacyConfigFileName))
}

func TestMigrateLegacyLayout_SudoElevationSkips(t *testing.T) {
	configBase, _ := setupMigrationEnv(t)
	legacy := seedLegacyConfigTree(t, configBase)

	restoreGeteuid := configGeteuid
	configGeteuid = func() int { return 0 }
	t.Cleanup(func() { configGeteuid = restoreGeteuid })
	t.Setenv("SUDO_USER", "someone")

	migrateLegacyLayout()

	assert.DirExists(t, legacy, "an elevated run never moves the invoking user's files")
	assert.NoDirExists(t, filepath.Join(configBase, "safedep", "gryph"))
}

func TestNormalizeConfigFileName_KeepsBothFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacyConfigFileName), []byte("a: 1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), []byte("b: 2\n"), 0o600))

	normalizeConfigFileName(dir)

	assert.FileExists(t, filepath.Join(dir, legacyConfigFileName))
	data, err := os.ReadFile(filepath.Join(dir, configFileName))
	require.NoError(t, err)
	assert.Equal(t, "b: 2\n", string(data))
}

func TestLegacyResolvers_PreferPreexistingXDGDirs(t *testing.T) {
	clearPathEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	legacyConfig := filepath.Join(home, ".config", legacyLeaf)
	require.NoError(t, os.MkdirAll(legacyConfig, 0o700))
	legacyData := filepath.Join(home, ".local", "share", legacyLeaf)
	require.NoError(t, os.MkdirAll(legacyData, 0o700))

	assert.Equal(t, legacyConfig, legacyConfigDir())
	assert.Equal(t, legacyData, legacyDataDir())
}
