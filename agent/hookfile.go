package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/safedep/gryph/platform/nofollow"
)

// ReadHookFile returns the content of the hook configuration file at path.
// It returns fs.ErrNotExist when the file is missing. Under a repair it
// refuses a symbolic link in the path below the user's home directory.
func ReadHookFile(path string, opts InstallOptions) ([]byte, error) {
	if !opts.Repair {
		return os.ReadFile(path)
	}
	dir, name, err := openHookDir(path)
	if err != nil {
		return nil, err
	}
	defer closeDir(dir)
	return dir.ReadFile(name)
}

// WriteHookFile replaces the hook configuration file at path with data. It
// writes a temporary file in the same directory and renames it into place,
// so a crash leaves the old file. A human-run install writes through a link
// at path, because a user may keep the file in a dotfiles repository. A
// repair refuses every link below the home directory.
func WriteHookFile(path string, data []byte, perm os.FileMode, opts InstallOptions) error {
	if !opts.Repair {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		dir, err := nofollow.OpenDir(filepath.Dir(path), ".")
		if err != nil {
			return err
		}
		defer closeDir(dir)
		return dir.WriteFile(filepath.Base(path), data, perm)
	}
	dir, name, err := openHookDir(path)
	if err != nil {
		return err
	}
	defer closeDir(dir)
	return dir.WriteFile(name, data, perm)
}

// EnsureHookDir creates the directory at path when it is missing. Under a
// repair it creates only the last component, below a parent that holds no
// link.
func EnsureHookDir(path string, perm os.FileMode, opts InstallOptions) error {
	if !opts.Repair {
		return os.MkdirAll(path, perm)
	}
	dir, name, err := openHookDir(path)
	if err != nil {
		return err
	}
	defer closeDir(dir)
	return dir.Mkdir(name, perm)
}

// RepairRefused reports whether err comes from a link that a repair refused.
func RepairRefused(err error) bool {
	return errors.Is(err, nofollow.ErrSymlink)
}

// openHookDir opens the directory of path without following a link below
// the trusted base. The base is the user's home directory when path is
// under it, else the parent of the directory of path.
func openHookDir(path string) (*nofollow.Dir, string, error) {
	base, rel, err := hookFileBase(filepath.Dir(path))
	if err != nil {
		return nil, "", err
	}
	dir, err := nofollow.OpenDir(base, rel)
	if err != nil {
		return nil, "", fmt.Errorf("open %s for a repair: %w", filepath.Dir(path), err)
	}
	return dir, filepath.Base(path), nil
}

// homeDir returns the user's home directory. Tests override it.
var homeDir = os.UserHomeDir

func hookFileBase(dir string) (base, rel string, err error) {
	if home, homeErr := homeDir(); homeErr == nil && home != "" {
		if r, relErr := filepath.Rel(home, dir); relErr == nil && r != ".." && !isParentPath(r) {
			return trustedBase(home), r, nil
		}
	}
	return trustedBase(filepath.Dir(dir)), filepath.Base(dir), nil
}

// trustedBase resolves the links of the base once. The base comes from the
// operating system or from a path above the agent directory, not from the
// hook configuration, so following its links is safe.
func trustedBase(base string) string {
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		return resolved
	}
	return base
}

func isParentPath(rel string) bool {
	return len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}

func closeDir(dir *nofollow.Dir) {
	_ = dir.Close()
}

// IsNotExist reports whether err says that the file is missing.
func IsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
