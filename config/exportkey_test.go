package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOrCreateExportKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "export.key")

	key, err := LoadOrCreateExportKey(path)
	require.NoError(t, err)
	assert.Len(t, key, exportKeySize)

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file stays")

	again, err := LoadOrCreateExportKey(path)
	require.NoError(t, err)
	assert.Equal(t, key, again, "the key is stable for one install")

	other, err := LoadOrCreateExportKey(filepath.Join(t.TempDir(), "export.key"))
	require.NoError(t, err)
	assert.NotEqual(t, key, other)
}

func TestLoadOrCreateExportKey_RejectsShortKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.key")
	require.NoError(t, os.WriteFile(path, []byte("short"), 0o600))
	_, err := LoadOrCreateExportKey(path)
	assert.ErrorContains(t, err, "want 32 bytes")
}

func TestConfig_ExportKeyFile(t *testing.T) {
	cfg := Default()
	cfg.Storage.Path = filepath.Join("/var", "gryph", "audit.db")
	assert.Equal(t, filepath.Join("/var", "gryph", "export.key"), cfg.ExportKeyFile())
}
