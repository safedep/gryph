//go:build !windows

package receipt

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// readPrivateKeyFileSecure opens the file at path once with O_NOFOLLOW so a
// symlink swap between the perm check and the read cannot trick us into
// loading attacker-controlled bytes. It then verifies the file mode and
// owner on the open file descriptor before returning its contents.
func readPrivateKeyFileSecure(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("receipt: open private key: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := checkSecureFilePerms(f); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("receipt: read private key: %w", err)
	}
	return data, nil
}

func checkSecureFilePerms(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("receipt: stat private key: %w", err)
	}
	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		return fmt.Errorf("receipt: private key %s has too-loose permissions %#o (want 0600)", f.Name(), mode)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if st.Uid != uint32(os.Getuid()) {
			return fmt.Errorf("receipt: private key %s is owned by uid %d, expected %d", f.Name(), st.Uid, os.Getuid())
		}
	}
	return nil
}
