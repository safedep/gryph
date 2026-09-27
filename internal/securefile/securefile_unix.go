//go:build !windows

package securefile

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func open(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s is a symbolic link. Replace it with a regular file: %w", path, err)
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}

func checkOwnerOnly(path string, f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return fmt.Errorf("%s has mode %#o. Run chmod 600 %s", path, mode, path)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("%s has owner uid %d, not the current uid %d. Run chown %d %s",
			path, st.Uid, os.Getuid(), os.Getuid(), path)
	}
	return nil
}
