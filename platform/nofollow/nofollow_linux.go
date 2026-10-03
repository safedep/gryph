package nofollow

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// openBeneath opens the directory comps below h in one call with openat2.
// RESOLVE_NO_SYMLINKS refuses a link in any component and RESOLVE_BENEATH
// refuses an escape from h. A kernel without openat2 (before 5.6) gets
// the component walk.
func (h dirHandle) openBeneath(comps []string) (dirHandle, error) {
	rel := strings.Join(comps, "/")
	fd, err := unix.Openat2(h.fd, rel, &unix.OpenHow{
		Flags:   uint64(dirOpenFlags),
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_BENEATH,
	})
	switch {
	case errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.E2BIG):
		return h.walk(comps)
	case errors.Is(err, syscall.ELOOP) || errors.Is(err, unix.EXDEV):
		return dirHandle{}, fmt.Errorf("%s: %w", filepath.Join(h.path, rel), ErrSymlink)
	case err != nil:
		return dirHandle{}, wrapLink(filepath.Join(h.path, rel), err)
	}
	return dirHandle{fd: fd, path: filepath.Join(h.path, rel)}, nil
}
