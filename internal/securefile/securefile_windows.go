//go:build windows

package securefile

import (
	"fmt"
	"os"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Access rights that let a principal read, change, or take control of the
// file.
const (
	fileReadData    = 0x1
	fileWriteData   = 0x2
	fileAppendData  = 0x4
	sensitiveAccess = fileReadData | fileWriteData | fileAppendData |
		windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE |
		windows.GENERIC_READ | windows.GENERIC_WRITE | windows.GENERIC_ALL
)

// denyAceTypes are the ACE types that deny access: plain, object, callback
// and callback object. A deny entry adds no exposure.
var denyAceTypes = []uint8{1, 6, 10, 12}

func open(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}

// create makes the file with a protected DACL that grants access only to
// the current user, SYSTEM and Administrators. The file has this DACL from
// the moment it exists, so no other user can open it in between.
func create(path string) (*os.File, error) {
	user, err := currentUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.String() + ")(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, sa,
		windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "create", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// checkOwnerOnly refuses a file whose owner or DACL lets a principal other
// than the current user, SYSTEM or Administrators read or change it. SYSTEM
// and Administrators can read every file, so they add no exposure. An
// elevated process creates files that Administrators own.
func checkOwnerOnly(path string, f *os.File) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read the security descriptor of %s: %w", path, err)
	}
	user, err := currentUser()
	if err != nil {
		return err
	}
	trusted, err := trustedSIDs(user)
	if err != nil {
		return err
	}
	isTrusted := func(sid *windows.SID) bool { return slices.ContainsFunc(trusted, sid.Equals) }

	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read the owner of %s: %w", path, err)
	}
	if !isTrusted(owner) {
		return fmt.Errorf("%s has owner %s, not the current user. Delete the file so that Gryph creates a new one",
			path, accountName(owner))
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read the DACL of %s: %w", path, err)
	}
	if dacl == nil {
		return fmt.Errorf("%s has no DACL, so every user can open it. Run: %s", path, restrictCommand(path, user, nil))
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
			return fmt.Errorf("%s has a DACL entry of type %d that Gryph cannot check. Run: %s",
				path, ace.Header.AceType, restrictCommand(path, user, nil))
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if uint32(ace.Mask)&sensitiveAccess != 0 && !isTrusted(sid) {
			return fmt.Errorf("%s grants access to %s. Run: %s", path, accountName(sid), restrictCommand(path, user, sid))
		}
	}
	return nil
}

func currentUser() (*windows.SID, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read the current user: %w", err)
	}
	return tu.User.Sid, nil
}

func trustedSIDs(user *windows.SID) ([]*windows.SID, error) {
	out := []*windows.SID{user}
	for _, t := range []windows.WELL_KNOWN_SID_TYPE{windows.WinLocalSystemSid, windows.WinBuiltinAdministratorsSid} {
		sid, err := windows.CreateWellKnownSid(t)
		if err != nil {
			return nil, fmt.Errorf("create a well-known SID: %w", err)
		}
		out = append(out, sid)
	}
	return out, nil
}

// restrictCommand is an icacls command that removes the inherited entries
// and the entry of other, and gives the current user sole access.
func restrictCommand(path string, user, other *windows.SID) string {
	remove := ""
	if other != nil {
		remove = " /remove:g " + quote(accountName(other))
	}
	return "icacls " + quote(path) + " /inheritance:r" + remove + " /grant:r " + quote(accountName(user)+":F")
}

// quote wraps s in double quotes for cmd.exe and PowerShell. A Windows path
// or account name cannot hold a double quote, so s needs no escape. Go %q
// would double each backslash of the path.
func quote(s string) string {
	return `"` + s + `"`
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

// syncDir does nothing. Windows cannot sync a directory handle, and NTFS
// journals the rename.
func syncDir(string) error {
	return nil
}
