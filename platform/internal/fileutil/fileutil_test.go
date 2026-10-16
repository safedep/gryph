package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.plist")
	changed, err := WriteIfChanged(path, []byte("a"))
	require.NoError(t, err)
	assert.True(t, changed)

	changed, err = WriteIfChanged(path, []byte("a"))
	require.NoError(t, err)
	assert.False(t, changed, "the same content is not written again")

	require.NoError(t, os.Chmod(path, 0o666))
	changed, err = WriteIfChanged(path, []byte("b"))
	require.NoError(t, err)
	assert.True(t, changed)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "a file that others could write gets the mode again")
	}
}
