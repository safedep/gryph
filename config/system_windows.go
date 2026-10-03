//go:build windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// programDataDir returns the ProgramData folder from the shell, never from
// the environment. A user can set PROGRAMDATA to a directory they own.
// managedBinaryDefault is where a package puts the gryph binary on
// Windows, under the folder that only an administrator can write.
func managedBinaryDefault() string {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil || dir == "" {
		dir = `C:\Program Files`
	}
	return filepath.Join(dir, "SafeDep", "gryph", "gryph.exe")
}

// supervisorSocketDefault names the pipe of the decision service.
func supervisorSocketDefault() string { return `\\.\pipe\safedep-gryph-hook` }

// supervisorStateDefault holds the partitions of the accounts.
func supervisorStateDefault() string { return filepath.Join(programDataDir(), "safedep", "gryph") }

func programDataDir() string {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil || dir == "" {
		return `C:\ProgramData`
	}
	return dir
}

// managedTrustAnchor is the directory that the operating system protects.
// The anchor and the directories above it get an owner check only, because
// ProgramData lets a standard user create a subdirectory by design. Every
// component below the anchor gets the full owner and DACL check. A test
// points the anchor at a temporary directory.
var managedTrustAnchor = programDataDir

// Access rights that let a principal change an entry or its contents.
// 0x2 is FILE_WRITE_DATA on a file and FILE_ADD_FILE on a directory. 0x4 is
// FILE_APPEND_DATA on a file and FILE_ADD_SUBDIRECTORY on a directory. 0x40
// is FILE_DELETE_CHILD.
const managedWriteAccess = 0x2 | 0x4 | 0x40 | windows.DELETE | windows.WRITE_DAC |
	windows.WRITE_OWNER | windows.GENERIC_WRITE | windows.GENERIC_ALL

// trustedInstallerSID is NT SERVICE\TrustedInstaller, which owns the
// Windows directory tree and the volume root.
const trustedInstallerSID = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"

// denyAceTypes are the ACE types that deny access: plain, object, callback
// and callback object. A deny entry adds no exposure.
var denyAceTypes = []uint8{1, 6, 10, 12}

// verifyManagedPathTrust accepts path only when it lies below the trust
// anchor, SYSTEM, Administrators or TrustedInstaller own every component,
// and no DACL entry below the anchor lets another principal write, delete
// or take control. A reparse point below the anchor is refused.
func verifyManagedPathTrust(path string) error {
	trusted, err := trustedOwners()
	if err != nil {
		return err
	}

	anchor := filepath.Clean(managedTrustAnchor())
	path = filepath.Clean(path)
	rel, err := filepath.Rel(anchor, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s is not below %s", path, anchor)
	}

	if err := checkManagedEntry(anchor, trusted, false); err != nil {
		return err
	}
	current := anchor
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := checkManagedEntry(current, trusted, true); err != nil {
			return err
		}
	}
	return nil
}

// checkManagedEntry checks the owner of path, and with full also the DACL
// and the file type.
func checkManagedEntry(path string, trusted []*windows.SID, full bool) error {
	if full {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("%s is a reparse point", path)
		}
	}

	var flags windows.SECURITY_INFORMATION = windows.OWNER_SECURITY_INFORMATION
	if full {
		flags |= windows.DACL_SECURITY_INFORMATION
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, flags)
	if err != nil {
		return fmt.Errorf("read the security descriptor of %s: %w", path, err)
	}
	isTrusted := func(sid *windows.SID) bool { return slices.ContainsFunc(trusted, sid.Equals) }

	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read the owner of %s: %w", path, err)
	}
	if !isTrusted(owner) {
		return fmt.Errorf("%s is owned by %s, not by SYSTEM or Administrators", path, accountName(owner))
	}
	if !full {
		return nil
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read the DACL of %s: %w", path, err)
	}
	if dacl == nil {
		return fmt.Errorf("%s has no DACL, so every user can change it", path)
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Errorf("read the DACL of %s: %w", path, err)
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || slices.Contains(denyAceTypes, ace.Header.AceType) {
			continue
		}
		// Other ACE types keep the SID at another offset, so the type check
		// comes before the SID read.
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("%s has a DACL entry of type %d that Gryph cannot check", path, ace.Header.AceType)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if uint32(ace.Mask)&managedWriteAccess != 0 && !isTrusted(sid) {
			return fmt.Errorf("%s lets %s write to it", path, accountName(sid))
		}
	}
	return nil
}

func trustedOwners() ([]*windows.SID, error) {
	var out []*windows.SID
	for _, t := range []windows.WELL_KNOWN_SID_TYPE{windows.WinLocalSystemSid, windows.WinBuiltinAdministratorsSid} {
		sid, err := windows.CreateWellKnownSid(t)
		if err != nil {
			return nil, fmt.Errorf("create a well-known SID: %w", err)
		}
		out = append(out, sid)
	}
	sid, err := windows.StringToSid(trustedInstallerSID)
	if err != nil {
		return nil, fmt.Errorf("parse the TrustedInstaller SID: %w", err)
	}
	return append(out, sid), nil
}

func accountName(sid *windows.SID) string {
	account, domain, _, err := sid.LookupAccount("")
	if err != nil {
		return sid.String()
	}
	if domain == "" {
		return account
	}
	return domain + `\` + account
}
