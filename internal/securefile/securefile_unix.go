//go:build !windows

package securefile

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func open(name, path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s %s is a symbolic link. Replace it with a regular file: %w", name, path, err)
	}
	if err != nil {
		return nil, fmt.Errorf("open %s %s: %w", name, path, err)
	}
	return f, nil
}

func checkOwnerOnly(name, path string, f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s %s: %w", name, path, err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return fmt.Errorf("%s %s has mode %#o. Run chmod 600 %s", name, path, mode, path)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("%s %s has owner uid %d, not the current uid %d. Run chown %d %s",
			name, path, st.Uid, os.Getuid(), os.Getuid(), path)
	}
	return nil
}
