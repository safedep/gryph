//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
)

// The file system calls are variables so a test can describe a path chain
// without root.
var (
	lstatManaged    = os.Lstat
	readlinkManaged = os.Readlink
)

// maxManagedLinkDepth bounds the symbolic links that one chain may follow.
const maxManagedLinkDepth = 8

// programDataDir has no meaning outside Windows.
func programDataDir() string {
	return ""
}

// verifyManagedPathTrust accepts path only when root owns it and every
// directory above it, and no component lets group or other write. A sticky
// directory may be group or other writable, because then only the owner of
// an entry can rename or remove it. A symbolic link component must be root
// owned, and its target chain must pass the same check. Anything else would
// let a user plant or replace a managed file that governs every user on the
// machine.
// managedBinaryDefault is where a package puts the root-owned gryph
// binary, on Linux and on macOS. /opt is root-only on a stock install of
// both, which /usr/local is not on an Intel Mac with Homebrew, and a
// package may write it on macOS without a change to the protected system
// volume.
func managedBinaryDefault() string {
	return "/opt/safedep/gryph/bin/gryph"
}

// supervisorSocketDefault is where the service manager opens the socket of
// the decision service. The directory is root-owned, so a user cannot put
// a socket of their own there.
func supervisorSocketDefault() string {
	if runtime.GOOS == "darwin" {
		return "/var/run/safedep/gryph/hook.sock"
	}
	return "/run/safedep/gryph/hook.sock"
}

// supervisorStateDefault holds the partitions of the accounts. The service
// account owns it, and no user can read another user's partition.
func supervisorStateDefault() string {
	if runtime.GOOS == "darwin" {
		return "/Library/Application Support/safedep/gryph"
	}
	return "/var/lib/safedep/gryph"
}

func verifyManagedPathTrust(path string) error {
	return verifyManagedChain(path, 0)
}

func verifyManagedChain(path string, depth int) error {
	if depth > maxManagedLinkDepth {
		return fmt.Errorf("%s: too many symbolic links", path)
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s is not an absolute path", path)
	}

	for _, prefix := range pathPrefixes(path) {
		info, err := lstatManaged(prefix)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("%s: no ownership information", prefix)
		}
		if st.Uid != 0 {
			return fmt.Errorf("%s is not owned by root", prefix)
		}

		mode := info.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			target, err := readlinkManaged(prefix)
			if err != nil {
				return err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(prefix), target)
			}
			if err := verifyManagedChain(target, depth+1); err != nil {
				return err
			}
		case mode.IsDir():
			if mode.Perm()&0o022 != 0 && mode&os.ModeSticky == 0 {
				return fmt.Errorf("%s is writable by group or other", prefix)
			}
		case mode.IsRegular():
			if mode.Perm()&0o022 != 0 {
				return fmt.Errorf("%s is writable by group or other", prefix)
			}
		default:
			return fmt.Errorf("%s is not a directory or a regular file", prefix)
		}
	}
	return nil
}

// pathPrefixes returns every prefix of an absolute clean path, from the
// root to the path itself.
func pathPrefixes(path string) []string {
	var out []string
	for p := path; ; p = filepath.Dir(p) {
		out = append(out, p)
		if p == filepath.Dir(p) {
			break
		}
	}
	slices.Reverse(out)
	return out
}
