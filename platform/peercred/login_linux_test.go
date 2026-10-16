package peercred

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpen_LoginIdentity(t *testing.T) {
	peer := openSelf(t)
	data, err := os.ReadFile("/proc/self/loginuid")
	if err != nil {
		t.Skipf("no loginuid on this kernel: %v", err)
	}
	want := strings.TrimSpace(string(data))
	id, ok := peer.LoginIdentity()
	if want == loginUIDUnset {
		assert.False(t, ok, "a process outside a login session has no identity")
		return
	}
	assert.True(t, ok)
	assert.Equal(t, want, id, "the identity is the one read at accept")
}
