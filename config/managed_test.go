package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedFileChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	changed, err := ManagedFileChanged(path, []byte("a: 1\n"))
	require.NoError(t, err)
	assert.True(t, changed, "a file that does not exist changes")
	require.NoError(t, os.WriteFile(path, []byte("a: 1\n"), 0o644))
	changed, err = ManagedFileChanged(path, []byte("a: 1\n"))
	require.NoError(t, err)
	assert.False(t, changed, "the same content changes nothing")
	changed, err = ManagedFileChanged(path, []byte("a: 2\n"))
	require.NoError(t, err)
	assert.True(t, changed, "other content changes")
}
