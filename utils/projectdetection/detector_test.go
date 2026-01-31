package projectdetection

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNPMDetector(t *testing.T) {
	d := npmDetector{}
	info, err := d.Detect(filepath.Join("testdata"))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "my-node-app", info.Name)

	info, err = d.Detect(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, info)
}

func TestCargoDetector(t *testing.T) {
	d := cargoDetector{}
	info, err := d.Detect(filepath.Join("testdata"))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "my-rust-crate", info.Name)

	info, err = d.Detect(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, info)
}

func TestGomodDetector(t *testing.T) {
	d := gomodDetector{}
	info, err := d.Detect(filepath.Join("testdata"))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "bar-baz", info.Name)

	info, err = d.Detect(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, info)
}

func TestPyprojectDetector(t *testing.T) {
	d := pyprojectDetector{}
	info, err := d.Detect(filepath.Join("testdata"))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "my-python-pkg", info.Name)

	info, err = d.Detect(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, info)
}
