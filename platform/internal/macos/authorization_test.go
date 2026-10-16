//go:build darwin

package macos

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRightExists(t *testing.T) {
	assert.True(t, RightExists("system.privilege.admin"), "a right of every macOS host")
	assert.False(t, RightExists("io.safedep.gryph.test-no-such-right"))
}

// The calls run without a person: an authorization goes to its external
// form and back, and a right that needs a password fails without a
// question.
func TestAuthorization_RoundTrip(t *testing.T) {
	auth, err := NewAuthorization()
	require.NoError(t, err)
	defer auth.Free()

	form, err := auth.External()
	require.NoError(t, err)
	assert.NotEqual(t, [ExternalFormSize]byte{}, form)

	again, err := AuthorizationFromExternal(form)
	require.NoError(t, err)
	defer again.Free()

	err = again.Obtain("system.privilege.admin", false)
	require.Error(t, err)
	assert.True(t, IsStatus(err, StatusDenied) || IsStatus(err, StatusInteractionNotAllowed), "no question without interaction: %v", err)

	_, err = again.Username()
	assert.Error(t, err, "nobody authenticated")
}

func TestAuthorizationFromExternal_Garbage(t *testing.T) {
	var form [ExternalFormSize]byte
	for i := range form {
		form[i] = byte(i)
	}
	_, err := AuthorizationFromExternal(form)
	assert.Error(t, err, "a form that names no authorization")
}
