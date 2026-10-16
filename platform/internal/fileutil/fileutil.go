// Package fileutil holds the file writes that the installers of the
// platform share.
package fileutil

import (
	"bytes"
	"os"
)

// WriteIfChanged writes a service manager file, readable by every account,
// and reports whether the content changed. A repeated install then changes
// nothing. The mode is set again on a file that existed, because a service
// manager refuses a job file that others can write.
func WriteIfChanged(path string, data []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, err
	}
	if err := os.Chmod(path, 0o644); err != nil {
		return false, err
	}
	return true, nil
}
