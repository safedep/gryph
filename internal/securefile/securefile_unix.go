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

func create(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
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

// syncDir makes a rename in dir durable. Without it, a power loss after the
// rename can leave the old file or no file.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
