//go:build darwin

package macos

import (
	"os"
	"os/exec"
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

	err = again.Obtain("system.privilege.admin", false, nil)
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

// writeTestRight puts a right whose rule is allow in the policy database
// and removes it at the end. Only root writes the database, so the test
// runs in the privileged job.
func writeTestRight(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("writing the authorization database needs root")
	}
	const right = "io.safedep.gryph.test-allow"
	out, err := exec.Command("security", "authorizationdb", "write", right, "allow").CombinedOutput()
	require.NoError(t, err, "%s", out)
	t.Cleanup(func() { _ = exec.Command("security", "authorizationdb", "remove", right).Run() })
	return right
}

// The path of a proof: one process obtains the right, and another takes
// the authorization from its external form and obtains the right again
// without a question.
func TestAuthorization_ExternalFormCarriesTheRight(t *testing.T) {
	right := writeTestRight(t)
	require.True(t, RightExists(right))

	auth, err := NewAuthorization()
	require.NoError(t, err)
	defer auth.Free()
	require.NoError(t, auth.Obtain(right, false, nil))
	form, err := auth.External()
	require.NoError(t, err)

	again, err := AuthorizationFromExternal(form)
	require.NoError(t, err)
	defer again.Free()
	assert.NoError(t, again.Obtain(right, false, nil))
}
