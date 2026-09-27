// Package securefile reads and writes secret files that only the current
// user may read and write.
package securefile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/safedep/dry/log"
)

// ReadFile returns the content of the file at path. It refuses a file that
// another user owns or that grants access to another user. On Unix it also
// refuses a symbolic link. On Windows it reads the owner and the DACL. It
// checks the open file, so a swap of the path after the check cannot change
// what it reads.
func ReadFile(path string) ([]byte, error) {
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	defer closeFile(f)
	if err := checkOwnerOnly(path, f); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// CreateTemp creates a new file in dir with a random name that starts with
// prefix. Only the current user can read and write it from the moment it
// exists: mode 0600 on Unix, an owner-only DACL with no inherited entries on
// Windows.
func CreateTemp(dir, prefix string) (*os.File, error) {
	for range 16 {
		var suffix [8]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, fmt.Errorf("create a file in %s: %w", dir, err)
		}
		f, err := create(filepath.Join(dir, prefix+hex.EncodeToString(suffix[:])))
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, fmt.Errorf("create a file in %s: %w", dir, fs.ErrExist)
}

// WriteFile writes data to path through a temporary file from CreateTemp,
// and renames it into place. A crash leaves the old file.
func WriteFile(path string, data []byte) error {
	f, err := CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	return Replace(f, path, data)
}

// Replace writes data to tmp, syncs and closes it, and renames it to path.
// It removes tmp when a step fails. tmp must be in the directory of path.
func Replace(tmp *os.File, path string, data []byte) error {
	if err := writeAndClose(tmp, data); err != nil {
		removeFile(tmp.Name())
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		removeFile(tmp.Name())
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

func closeFile(f *os.File) {
	if err := f.Close(); err != nil {
		log.Warnf("close %s: %v", f.Name(), err)
	}
}

func removeFile(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warnf("remove %s: %v", path, err)
	}
}
