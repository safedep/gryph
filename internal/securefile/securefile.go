// Package securefile reads secret files that only the current user may
// read and write.
package securefile

import (
	"fmt"
	"io"

	"github.com/safedep/dry/log"
)

// ReadFile returns the content of the file at path. On Unix it does not
// follow a symbolic link, and it refuses a file that another user owns or
// that grants any access to the group or to others. It checks the open
// file, so a swap of the path after the check cannot change what it reads.
func ReadFile(path string) ([]byte, error) {
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Warnf("close %s: %v", path, err)
		}
	}()
	if err := checkOwnerOnly(path, f); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}
