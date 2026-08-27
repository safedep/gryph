package config

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/safedep/dry/log"
)

// configFileName is the canonical config file name across SafeDep tools.
const configFileName = "config.yml"

// globalConfigDirOverride replaces the system managed config directory in
// tests. There is no env var or flag on purpose: a user must not be able to
// point the managed path at a file they own and bypass governance.
var globalConfigDirOverride string

// systemConfigDir returns the root owned directory for system managed gryph
// state, or "" when the platform has no such location. Today only
// config.yml is read from it. The directory mirrors the per-user config
// directory layout, so policy.yaml, policies/ and keys/receipt-pub.json are
// reserved for a future managed policy and trust store.
func systemConfigDir() string {
	if globalConfigDirOverride != "" {
		return globalConfigDirOverride
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join("/Library/Application Support", defaultHomeRelativePath)
	case "linux":
		return filepath.Join("/etc", defaultHomeRelativePath)
	case "windows":
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, defaultHomeRelativePath)
	}

	return ""
}

// managedFileTrusted is overridable in tests, which cannot create root owned
// files.
var managedFileTrusted = verifyManagedFileTrust

// ManagedConfigFile returns the system managed config file when it exists,
// is a regular file, and passes the trust check. It returns "" otherwise.
// While a managed file is active, it is authoritative and the per-user
// config file is ignored.
func ManagedConfigFile() string {
	dir := systemConfigDir()
	if dir == "" {
		return ""
	}

	path := filepath.Join(dir, configFileName)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}

	if !managedFileTrusted(info) {
		log.Warnf("ignoring managed config %s: the file is not root owned or is writable by group or other", path)
		return ""
	}

	return path
}
