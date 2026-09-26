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
// The name describes the file in errors, for example "export key".
func ReadFile(name, path string) ([]byte, error) {
	f, err := open(name, path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Warnf("%s: close %s: %v", name, path, err)
		}
	}()
	if err := checkOwnerOnly(name, path, f); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %s %s: %w", name, path, err)
	}
	return data, nil
}
