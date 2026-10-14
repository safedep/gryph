package listen

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Socket activation is a systemd contract, so only Linux reads and unsets
// the variables. Other systems ignore them.
func TestActivated_NotForThisProcess(t *testing.T) {
	t.Setenv("LISTEN_PID", "1")
	t.Setenv("LISTEN_FDS", "1")
	lns, err := Activated()
	require.NoError(t, err)
	assert.Nil(t, lns)
	_, set := os.LookupEnv("LISTEN_PID")
	assert.False(t, set, "the variables are unset after the read")
}
