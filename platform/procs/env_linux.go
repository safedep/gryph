package procs

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

// hasEnv reads /proc/<pid>/environ, which the kernel shows to the same
// account only.
func hasEnv(pid int, key string) (bool, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return false, err
	}
	prefix := []byte(key + "=")
	for _, entry := range bytes.Split(data, []byte{0}) {
		if bytes.HasPrefix(entry, prefix) {
			return true, nil
		}
	}
	return false, nil
}
