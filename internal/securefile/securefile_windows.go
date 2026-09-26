//go:build windows

package securefile

import (
	"fmt"
	"os"
)

// Windows has no POSIX mode bits or uid, so it skips the owner and mode checks.
func open(name, path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s %s: %w", name, path, err)
	}
	return f, nil
}

func checkOwnerOnly(string, string, *os.File) error {
	return nil
}
