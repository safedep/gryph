package localauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	restore := policyPath
	policyPath = filepath.Join(t.TempDir(), "io.safedep.gryph.policy")
	t.Cleanup(func() { policyPath = restore })
	ctx := context.Background()

	reg, err := Register(ctx)
	require.NoError(t, err)
	assert.Equal(t, Registration{Path: policyPath, Changed: true}, reg)
	data, err := os.ReadFile(policyPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `action id="`+ActionID+`"`)
	assert.Contains(t, string(data), "auth_self")

	reg, err = Register(ctx)
	require.NoError(t, err)
	assert.False(t, reg.Changed, "the same content is not written again")

	reg, err = Unregister(ctx)
	require.NoError(t, err)
	assert.True(t, reg.Changed)
	reg, err = Unregister(ctx)
	require.NoError(t, err)
	assert.False(t, reg.Changed, "a missing file is not an error")
}

func TestHint(t *testing.T) {
	assert.Contains(t, Hint(), "pkttyagent")
}
