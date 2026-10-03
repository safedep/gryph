//go:build !windows

package account

import (
	"os/user"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemberOf(t *testing.T) {
	u, err := user.Current()
	require.NoError(t, err)
	g, err := user.LookupGroupId(u.Gid)
	require.NoError(t, err)

	ok, err := MemberOf(u.Uid, g.Name)
	require.NoError(t, err)
	assert.True(t, ok, "an account is in its primary group")

	_, err = MemberOf(u.Uid, "gryph-no-such-group-xyz")
	assert.Error(t, err, "an unknown group is an error, not a no")

	_, err = MemberOf("4000000000", g.Name)
	assert.Error(t, err, "an unknown account is an error")
}
