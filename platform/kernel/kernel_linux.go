package kernel

import (
	"os"
	"path/filepath"
	"strings"
)

func setting(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join("/proc/sys", filepath.FromSlash(strings.ReplaceAll(name, ".", "/"))))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
