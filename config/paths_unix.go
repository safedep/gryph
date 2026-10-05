//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// baseDirsFor returns the platform base directories under home.
func baseDirsFor(home string) baseDirs {
	if runtime.GOOS == "darwin" {
		support := filepath.Join(home, "Library", "Application Support")
		return baseDirs{config: support, data: support, cache: filepath.Join(home, "Library", "Caches")}
	}
	return baseDirs{
		config: filepath.Join(home, ".config"),
		data:   filepath.Join(home, ".local", "share"),
		cache:  filepath.Join(home, ".cache"),
	}
}

// accountBaseDirs returns the base directories of the real user from the
// account database.
func accountBaseDirs() (baseDirs, error) {
	home, err := accountHomeDir(os.Getuid())
	if err != nil {
		return baseDirs{}, err
	}
	return baseDirsFor(home), nil
}
