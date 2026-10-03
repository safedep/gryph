// Package nofollow opens files under a trusted base directory without
// following a symbolic link or a reparse point in any component below the
// base. An unattended repair of a file in the user's home runs through it,
// so a link that an adversary planted cannot send the write elsewhere.
//
// On Unix every component is opened relative to the previous one with
// O_NOFOLLOW, so a swap of a component between two steps cannot redirect
// the path. Linux uses openat2 with RESOLVE_NO_SYMLINKS and RESOLVE_BENEATH
// when the kernel has it. On Windows each component is checked for the
// reparse attribute before the open, in two steps.
package nofollow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrSymlink is the error of a path component that is a symbolic link or a
// reparse point.
var ErrSymlink = errors.New("nofollow: the path holds a symbolic link")

// Dir is an open directory below the base, reached without following a
// link. Its methods act on names directly inside it.
type Dir struct {
	path   string
	handle dirHandle
}

// OpenDir opens the directory base/rel. base is trusted and may hold
// links. rel must be a clean relative path with no ".." component, or "."
// for the base itself.
func OpenDir(base, rel string) (*Dir, error) {
	comps, err := components(rel)
	if err != nil {
		return nil, err
	}
	h, err := openDir(base, comps)
	if err != nil {
		return nil, err
	}
	return &Dir{path: filepath.Join(base, rel), handle: h}, nil
}

// Path returns the path of the directory.
func (d *Dir) Path() string { return d.path }

// Close releases the directory.
func (d *Dir) Close() error { return d.handle.close() }

// ReadFile returns the content of the regular file name in the directory.
// A link or another kind of file is refused.
func (d *Dir) ReadFile(name string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	return d.handle.readFile(name)
}

// Stat returns the information of name in the directory without following
// a link.
func (d *Dir) Stat(name string) (fs.FileInfo, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	return d.handle.stat(name)
}

// WriteFile replaces the file name in the directory with data. It writes a
// temporary file next to it and renames it into place, so a reader sees the
// old file or the new one. A link at name is refused and stays in place.
func (d *Dir) WriteFile(name string, data []byte, perm os.FileMode) error {
	if err := checkName(name); err != nil {
		return err
	}
	if info, err := d.handle.stat(name); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s: %w", filepath.Join(d.path, name), ErrSymlink)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return d.handle.writeFile(name, data, perm)
}

// Mkdir creates the directory name in the directory. An existing directory
// is not an error. A link at name is refused.
func (d *Dir) Mkdir(name string, perm os.FileMode) error {
	if err := checkName(name); err != nil {
		return err
	}
	info, err := d.handle.stat(name)
	switch {
	case err == nil && info.IsDir():
		return nil
	case err == nil:
		return fmt.Errorf("%s exists and is not a directory: %w", filepath.Join(d.path, name), ErrSymlink)
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	return d.handle.mkdir(name, perm)
}

// components splits rel into its path components and rejects a path that
// leaves the base.
func components(rel string) ([]string, error) {
	rel = filepath.Clean(rel)
	if rel == "." {
		return nil, nil
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("nofollow: %q is not a relative path below the base", rel)
	}
	return strings.Split(rel, string(filepath.Separator)), nil
}

// checkName rejects a name that is not one path component.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("nofollow: %q is not a file name", name)
	}
	return nil
}

// tempName returns the name of the temporary file for name.
func tempName(name string, suffix [8]byte) string {
	return fmt.Sprintf(".%s.tmp-%x", name, suffix)
}

// ReadFile returns the content of the file at path without following a
// link in its last component. The directory of path is trusted.
func ReadFile(path string) ([]byte, error) {
	dir, err := OpenDir(filepath.Dir(path), ".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	return dir.ReadFile(filepath.Base(path))
}

// WriteFile replaces the file at path through a temporary file and a
// rename, and refuses a link at path. The directory of path is trusted.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir, err := OpenDir(filepath.Dir(path), ".")
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.WriteFile(filepath.Base(path), data, perm)
}
