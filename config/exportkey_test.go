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

func TestLoadOrCreateExportKey_RefusesUnsafeFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode, owner and symlink checks do not apply on Windows")
	}
	valid := make([]byte, exportKeySize)

	cases := []struct {
		name    string
		setup   func(t *testing.T, dir string) string
		wantErr string
	}{
		{
			name: "owner only key loads",
			setup: func(t *testing.T, dir string) string {
				return writeExportKey(t, dir, valid, 0o600)
			},
		},
		{
			name: "wide mode fails",
			setup: func(t *testing.T, dir string) string {
				return writeExportKey(t, dir, valid, 0o666)
			},
			wantErr: "has mode 0666. Run chmod 600",
		},
		{
			name: "symlink to a valid key fails",
			setup: func(t *testing.T, dir string) string {
				target := writeExportKey(t, dir, valid, 0o600)
				link := filepath.Join(dir, "link.key")
				require.NoError(t, os.Symlink(target, link))
				return link
			},
			wantErr: "is a symbolic link",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t, t.TempDir())
			key, err := LoadOrCreateExportKey(path)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, "export key "+path)
				assert.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, key)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, valid, key)
		})
	}
}

func writeExportKey(t *testing.T, dir string, key []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, "export.key")
	require.NoError(t, os.WriteFile(path, key, 0o600))
	require.NoError(t, os.Chmod(path, mode))
	return path
}

func TestConfig_ExportKeyFile(t *testing.T) {
	cfg := Default()
	cfg.Storage.Path = filepath.Join("/var", "gryph", "audit.db")
	assert.Equal(t, filepath.Join("/var", "gryph", "export.key"), cfg.ExportKeyFile())
}
