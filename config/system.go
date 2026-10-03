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
// state, or "" when the platform has no such location. It holds config.yml,
// policy.yaml and policies/. The directory mirrors the per-user config
// directory layout, so keys/receipt-pub.json is reserved for a future
// managed trust store.
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
		return filepath.Join(programDataDir(), defaultHomeRelativePath)
	}

	return ""
}

// managedPathTrusted is overridable in tests, which cannot create root owned
// files.
var managedPathTrusted = verifyManagedPathTrust

// ManagedConfigState describes the system managed config file of this host.
type ManagedConfigState struct {
	// Path is where the managed file lives on this platform. It is empty
	// when the platform has no managed location.
	Path string
	// Exists is true when a regular file is at Path.
	Exists bool
	// Err says why Gryph ignores a file that exists. It is nil when the file
	// is trusted or when no file exists.
	Err error
}

// ManagedConfigStatus reports the managed config file and whether Gryph
// trusts it. The trust check covers the file and every directory above it.
// Without the chain, a root owned file in a user writable directory is
// replaceable by rename, and the host silently falls back to the per-user
// config.
func ManagedConfigStatus() ManagedConfigState {
	dir := systemConfigDir()
	if dir == "" {
		return ManagedConfigState{}
	}

	state := ManagedConfigState{Path: filepath.Join(dir, configFileName)}
	info, err := os.Stat(state.Path)
	if err != nil || !info.Mode().IsRegular() {
		return state
	}
	state.Exists = true
	state.Err = managedPathTrusted(state.Path)
	return state
}

// managedConfigActive reports a trusted managed config file. It logs
// nothing, because the path resolvers call it on every lookup.
func managedConfigActive() bool {
	state := ManagedConfigStatus()
	return state.Exists && state.Err == nil
}

// ManagedConfigDir returns the system managed directory, or "" when the
// platform has no such location. The built-in rules protect it.
func ManagedConfigDir() string {
	return systemConfigDir()
}

// ManagedConfigActive reports a trusted managed config file.
func ManagedConfigActive() bool {
	return managedConfigActive()
}

// ManagedPolicy describes the managed policy sources of this host.
type ManagedPolicy struct {
	// Active is true when a trusted managed config file is in force. Its
	// allow_user_policy key then decides whether the user's sources load.
	Active bool
	// File and Dir are the managed policy file and the managed policies
	// directory. Both are empty when the platform has no managed location.
	File string
	Dir  string
	// Trust verifies the path chain of one managed file. A file that fails
	// it does not load.
	Trust func(path string) error
}

// ManagedPolicyState returns the managed policy sources of this host. The
// files load whenever they exist and pass the trust check, with or without
// a managed config file, so an administrator can ship policy alone.
func ManagedPolicyState() ManagedPolicy {
	dir := systemConfigDir()
	if dir == "" {
		return ManagedPolicy{}
	}
	return ManagedPolicy{
		Active: managedConfigActive(),
		File:   filepath.Join(dir, "policy.yaml"),
		Dir:    filepath.Join(dir, "policies"),
		Trust:  managedPathTrusted,
	}
}

// ManagedConfigFile returns the system managed config file when it exists,
// is a regular file, and passes the trust check. It returns "" otherwise.
// While a managed file is active, it is authoritative and the per-user
// config file is ignored.
func ManagedConfigFile() string {
	state := ManagedConfigStatus()
	if !state.Exists {
		return ""
	}
	if state.Err != nil {
		log.Warnf("ignoring managed config %s: %v", state.Path, state.Err)
		return ""
	}
	return state.Path
}
