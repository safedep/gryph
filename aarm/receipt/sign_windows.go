//go:build windows

package receipt

import (
	"fmt"
	"io"
	"os"
)

// readPrivateKeyFileSecure on Windows opens the file once and reads its
// contents. Windows POSIX-style mode bits and Unix uid checks do not apply,
// so the perm/owner gates are skipped here.
func readPrivateKeyFileSecure(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("receipt: open private key: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("receipt: read private key: %w", err)
	}
	return data, nil
}
