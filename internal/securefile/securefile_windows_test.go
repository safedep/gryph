//go:build windows

package securefile

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestReadFile_WindowsDACL(t *testing.T) {
	user, err := currentUser()
	require.NoError(t, err)
	owner := "(A;;FA;;;" + user.String() + ")"

	cases := []struct {
		name    string
		sddl    string
		wantErr string
	}{
		{"owner only", "D:P" + owner, ""},
		{"owner, SYSTEM and Administrators", "D:P" + owner + "(A;;FA;;;SY)(A;;FA;;;BA)", ""},
		{"Everyone can read", "D:P" + owner + "(A;;FR;;;WD)", "grants access to"},
		{"Users can read", "D:P" + owner + "(A;;FR;;;BU)", "grants access to"},
		{"Authenticated Users can change", "D:P" + owner + "(A;;FW;;;AU)", "grants access to"},
		{"Anonymous is denied", "D:P(D;;FA;;;AN)" + owner, ""},
		{"inherit-only entry for Everyone", "D:P" + owner + "(A;OICIIO;FA;;;WD)", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key")
			require.NoError(t, os.WriteFile(path, []byte("data"), 0o600))
			setDACL(t, path, tc.sddl)

			data, err := ReadFile(path)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.ErrorContains(t, err, "icacls")
				assert.Nil(t, data)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []byte("data"), data)
		})
	}
}

func TestCreateTemp_WindowsDACLIsOwnerOnly(t *testing.T) {
	f, err := CreateTemp(t.TempDir(), ".key-")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	sd, err := windows.GetNamedSecurityInfo(f.Name(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	require.NoError(t, err)
	control, _, err := sd.Control()
	require.NoError(t, err)
	assert.NotZero(t, control&windows.SE_DACL_PROTECTED, "the DACL inherits no entry")

	user, err := currentUser()
	require.NoError(t, err)
	trusted, err := trustedSIDs(user)
	require.NoError(t, err)
	dacl, _, err := sd.DACL()
	require.NoError(t, err)
	require.Equal(t, uint16(len(trusted)), dacl.AceCount)
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		require.NoError(t, windows.GetAce(dacl, i, &ace))
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		assert.True(t, trusted[i].Equals(sid), "entry %d is for %s", i, sid)
	}
}

func setDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	require.NoError(t, err)
	dacl, _, err := sd.DACL()
	require.NoError(t, err)
	require.NoError(t, windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil))
}
