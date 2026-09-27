package securefile

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode, owner and symlink checks do not apply on Windows")
	}

	cases := []struct {
		name    string
		setup   func(t *testing.T, dir string) string
		wantErr string
	}{
		{
			name: "owner only file loads",
			setup: func(t *testing.T, dir string) string {
				return writeFile(t, dir, "secret", 0o600)
			},
		},
		{
			name: "wide mode fails",
			setup: func(t *testing.T, dir string) string {
				return writeFile(t, dir, "secret", 0o666)
			},
			wantErr: "has mode 0666. Run chmod 600",
		},
		{
			name: "group read fails",
			setup: func(t *testing.T, dir string) string {
				return writeFile(t, dir, "secret", 0o640)
			},
			wantErr: "has mode 0640",
		},
		{
			name: "symlink fails",
			setup: func(t *testing.T, dir string) string {
				target := writeFile(t, dir, "secret", 0o600)
				link := filepath.Join(dir, "link")
				require.NoError(t, os.Symlink(target, link))
				return link
			},
			wantErr: "is a symbolic link",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t, t.TempDir())
			data, err := ReadFile(path)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, data)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []byte("data"), data)
		})
	}
}

func TestReadFile_MissingFile(t *testing.T) {
	_, err := ReadFile(filepath.Join(t.TempDir(), "missing"))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func writeFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o600))
	require.NoError(t, os.Chmod(path, mode))
	return path
}

func TestWriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, WriteFile(path, []byte("one")))
	require.NoError(t, WriteFile(path, []byte("two")))

	data, err := ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("two"), data)
	assertOwnerOnlyMode(t, path)

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file stays")
}

func TestCreateTemp(t *testing.T) {
	dir := t.TempDir()
	a, err := CreateTemp(dir, ".key-")
	require.NoError(t, err)
	b, err := CreateTemp(dir, ".key-")
	require.NoError(t, err)
	for _, f := range []*os.File{a, b} {
		assert.True(t, strings.HasPrefix(filepath.Base(f.Name()), ".key-"))
		_, err := f.Write([]byte("data"))
		require.NoError(t, err)
		require.NoError(t, f.Close())
		assertOwnerOnlyMode(t, f.Name())
		_, err = ReadFile(f.Name())
		require.NoError(t, err, "a file that CreateTemp makes loads")
	}
	assert.NotEqual(t, a.Name(), b.Name())
}

func assertOwnerOnlyMode(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
