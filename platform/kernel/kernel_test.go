package kernel

import (
	"errors"
	"io/fs"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetting(t *testing.T) {
	v, err := Setting("kernel.ostype")
	if runtime.GOOS != "linux" {
		assert.ErrorIs(t, err, ErrUnsupported)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, "Linux", v)

	_, err = Setting("kernel.no_such_setting_for_gryph")
	assert.True(t, errors.Is(err, fs.ErrNotExist), "%v", err)
}
