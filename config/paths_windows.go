//go:build windows

package config

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// baseDirsFor returns the profile base directories under home.
func baseDirsFor(home string) baseDirs {
	local := filepath.Join(home, "AppData", "Local")
	return baseDirs{config: filepath.Join(home, "AppData", "Roaming"), data: local, cache: local}
}

// accountBaseDirs returns the known folders of the current account from the
// shell, never from the environment.
func accountBaseDirs() (baseDirs, error) {
	roaming, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return baseDirs{}, fmt.Errorf("failed to resolve the roaming application data folder: %w", err)
	}
	local, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return baseDirs{}, fmt.Errorf("failed to resolve the local application data folder: %w", err)
	}
	return baseDirs{config: roaming, data: local, cache: local}, nil
}
