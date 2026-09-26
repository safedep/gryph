package securefile

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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
			data, err := ReadFile("test key", path)
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
	_, err := ReadFile("test key", filepath.Join(t.TempDir(), "missing"))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func writeFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o600))
	require.NoError(t, os.Chmod(path, mode))
	return path
}
