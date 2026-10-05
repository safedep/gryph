package localauth

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStartTime(t *testing.T) {
	line := "1234 (a b) c) S 1 1234 1234 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 98765 1000 100 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	start, err := parseStartTime(line)
	require.NoError(t, err)
	assert.Equal(t, uint64(98765), start)
	_, err = parseStartTime("1234 (x) S 1")
	assert.Error(t, err)
	_, err = parseStartTime("no parenthesis")
	assert.Error(t, err)
}

func TestSubjectOf_Self(t *testing.T) {
	s, err := SubjectOf(int32(os.Getpid()))
	require.NoError(t, err)
	assert.NotZero(t, s.StartTime)
	assert.Equal(t, int32(os.Getpid()), s.PID)
	_, err = SubjectOf(2147483000)
	assert.Error(t, err)
}

func TestPolicyFile(t *testing.T) {
	path, content := PolicyFile()
	assert.Equal(t, "/usr/share/polkit-1/actions/io.safedep.gryph.policy", path)
	assert.Contains(t, string(content), `action id="`+ActionID+`"`)
	assert.Contains(t, string(content), "auth_self")
}
