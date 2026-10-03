//go:build windows

package nofollow

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// dirHandle is a checked directory path. Windows has no fd-relative open,
// so each operation checks the reparse attribute of its target and then
// opens the full path. The check and the open are two steps.
type dirHandle struct {
	path string
}

func openDir(base string, comps []string) (dirHandle, error) {
	cur := base
	for _, comp := range comps {
		cur = filepath.Join(cur, comp)
		info, err := attributes(cur)
		if err != nil {
			return dirHandle{}, err
		}
		if info&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return dirHandle{}, fmt.Errorf("%s is not a directory", cur)
		}
	}
	return dirHandle{path: cur}, nil
}

func (h dirHandle) close() error { return nil }

func (h dirHandle) stat(name string) (fs.FileInfo, error) {
	return os.Lstat(filepath.Join(h.path, name))
}

func (h dirHandle) readFile(name string) ([]byte, error) {
	path := filepath.Join(h.path, name)
	if _, err := attributes(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.ReadFile(path)
}

func (h dirHandle) writeFile(name string, data []byte, perm os.FileMode) error {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	tmp := filepath.Join(h.path, tempName(name, suffix))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(h.path, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (h dirHandle) mkdir(name string, perm os.FileMode) error {
	return os.Mkdir(filepath.Join(h.path, name), perm)
}

// attributes returns the file attributes of path and refuses a reparse
// point, which covers symbolic links, junctions and mount points.
func attributes(path string) (uint32, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var data windows.Win32FileAttributeData
	if err := windows.GetFileAttributesEx(name, windows.GetFileExInfoStandard, (*byte)(unsafePointer(&data))); err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return 0, &os.PathError{Op: "stat", Path: path, Err: fs.ErrNotExist}
		}
		return 0, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return 0, fmt.Errorf("%s: %w", path, ErrSymlink)
	}
	return data.FileAttributes, nil
}
