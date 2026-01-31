package projectdetection

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectProject_EmptyPath(t *testing.T) {
	info, err := DetectProject("")
	assert.Nil(t, info)
	assert.True(t, errors.Is(err, ErrNoProjectDetected))
}

func TestDetectProject_NoManifests(t *testing.T) {
	dir := t.TempDir()
	info, err := DetectProject(dir)
	assert.Nil(t, info)
	assert.True(t, errors.Is(err, ErrNoProjectDetected))
}

func TestDetectProject_FirstSuccessWins(t *testing.T) {
	testdataDir := filepath.Join("testdata")
	info, err := DetectProject(testdataDir)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "my-node-app", info.Name)
}

func TestDetectProject_OnlyGoMod(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join("testdata", "go.mod"), filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	info, err := DetectProject(dir)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "bar-baz", info.Name)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
