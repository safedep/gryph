//go:build windows

package cli

import (
	"errors"
	"io/fs"
)

// ownerOf reports false on Windows: the owner is a security identifier,
// and the decision service does not run there.
func ownerOf(fs.FileInfo) (uid, gid uint32, ok bool) {
	return 0, 0, false
}

// reloadSignal is not available on Windows: the decision service does not
// run there.
func reloadSignal(int) error {
	return errors.New("not supported on this platform")
}
