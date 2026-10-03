//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func withTrustAnchor(t *testing.T, dir string) {
	t.Helper()
	restore := managedTrustAnchor
	managedTrustAnchor = func() string { return dir }
	t.Cleanup(func() { managedTrustAnchor = restore })
}

func TestProgramDataDir_IgnoresEnvironment(t *testing.T) {
	fake := t.TempDir()
	t.Setenv("PROGRAMDATA", fake)
	assert.NotEqual(t, fake, programDataDir())
	assert.True(t, filepath.IsAbs(programDataDir()))
}

func TestVerifyManagedPathTrust_OutsideAnchor(t *testing.T) {
	withTrustAnchor(t, `C:\ProgramData`)
	assert.ErrorContains(t, verifyManagedPathTrust(`C:\Users\dev\config.yml`), "is not below")
	assert.ErrorContains(t, verifyManagedPathTrust(`C:\ProgramData`), "is not below")
}

// TestVerifyManagedPathTrust_UserFileRefused checks a file that the test
// user created. Its DACL inherits an entry that lets the user write, so the
// chain is untrusted whoever owns it.
func TestVerifyManagedPathTrust_UserFileRefused(t *testing.T) {
	base := t.TempDir()
	withTrustAnchor(t, base)
	dir := filepath.Join(base, "safedep", "gryph")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, configFileName)
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	assert.Error(t, verifyManagedPathTrust(path))
}

// TestVerifyManagedPathTrust_ProtectedChain gives every component below the
// anchor an explicit DACL for SYSTEM and Administrators only, and the
// Administrators owner. It skips when the process cannot set that owner,
// because only an elevated process can.
func TestVerifyManagedPathTrust_ProtectedChain(t *testing.T) {
	base := t.TempDir()
	withTrustAnchor(t, base)
	dir := filepath.Join(base, "safedep", "gryph")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, configFileName)
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	require.NoError(t, err)
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)")
	require.NoError(t, err)
	dacl, _, err := sd.DACL()
	require.NoError(t, err)

	for _, p := range []string{base, filepath.Join(base, "safedep"), dir, path} {
		err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
			windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			admins, nil, dacl, nil)
		if err != nil {
			t.Skipf("cannot set the owner of %s: %v", p, err)
		}
	}

	require.NoError(t, verifyManagedPathTrust(path))

	// A write entry for Users on one directory breaks the chain.
	loose, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;BU)")
	require.NoError(t, err)
	looseDACL, _, err := loose.DACL()
	require.NoError(t, err)
	require.NoError(t, windows.SetNamedSecurityInfo(filepath.Join(base, "safedep"), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, looseDACL, nil))
	assert.ErrorContains(t, verifyManagedPathTrust(path), "lets")
}
