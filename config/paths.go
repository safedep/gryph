package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/safedep/dry/log"
)

// defaultHomeRelativePath is the SafeDep shared namespace leaf appended to
// every base directory.
const defaultHomeRelativePath = "safedep/gryph"

// Directory overrides. Each value names the final directory, with no leaf
// appended.
const (
	configDirEnvKey = "GRYPH_CONFIG_DIR"
	dataDirEnvKey   = "GRYPH_DATA_DIR"
	cacheDirEnvKey  = "GRYPH_CACHE_DIR"
)

// resolveDir applies the shared resolution order for one directory kind:
// the GRYPH_* override, then the sudo guard, then the XDG base, then the
// platform base. XDG variables are honored on every platform, not only
// where os.UserConfigDir does, so users with an XDG layout on macOS or
// Windows keep one location across systems. A relative XDG value is
// ignored, as the XDG spec requires.
func resolveDir(overrideEnvKey, xdgEnvKey string, rootBase func() (string, error), platformBase func() string) string {
	if dir := os.Getenv(overrideEnvKey); dir != "" {
		return dir
	}
	if isSudoElevation() {
		if base, err := rootBase(); err == nil {
			return filepath.Join(base, defaultHomeRelativePath)
		} else {
			// No resolvable root passwd entry, e.g. scratch containers.
			// Without a passwd database there is no user switching, so the
			// cross-user poisoning the guard prevents cannot occur.
			log.Warnf("failed to resolve root home, using environment: %v", err)
		}
	}
	if base := os.Getenv(xdgEnvKey); filepath.IsAbs(base) {
		return filepath.Join(base, defaultHomeRelativePath)
	}
	return filepath.Join(platformBase(), defaultHomeRelativePath)
}

// getConfigDir returns the configuration directory for gryph.
func getConfigDir() string {
	return resolveDir(configDirEnvKey, "XDG_CONFIG_HOME", rootConfigDirResolver, configBaseDir)
}

// getDataDir returns the data directory for gryph.
func getDataDir() string {
	return resolveDir(dataDirEnvKey, "XDG_DATA_HOME", rootDataDirResolver, dataBaseDir)
}

// getCacheDir returns the cache directory for gryph.
func getCacheDir() string {
	return resolveDir(cacheDirEnvKey, "XDG_CACHE_HOME", rootCacheDirResolver, cacheBaseDir)
}

func configBaseDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config")
	}
	return dir
}

func dataBaseDir() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return localAppData
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Local")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share")
	}
}

func cacheBaseDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		switch runtime.GOOS {
		case "darwin":
			return filepath.Join(home, "Library", "Caches")
		case "windows":
			return filepath.Join(home, "AppData", "Local")
		default:
			return filepath.Join(home, ".cache")
		}
	}
	return dir
}

// configGeteuid is overridable in tests. os.Geteuid returns -1 on Windows,
// which disables the guard there.
var configGeteuid = os.Geteuid

// isSudoElevation reports a root run started through sudo. Only then do the
// resolvers divert to root's passwd home: sudo can preserve the invoking
// user's HOME and XDG_*, and a root run must not create root owned state in
// that user's home. A genuine root login keeps honoring its environment.
func isSudoElevation() bool {
	return configGeteuid() == 0 && os.Getenv("SUDO_USER") != ""
}

func rootHomeDir() (string, error) {
	u, err := user.LookupId("0")
	if err != nil {
		return "", fmt.Errorf("failed to resolve root home directory: %w", err)
	}
	if u.HomeDir == "" {
		return "", fmt.Errorf("root user has no home directory")
	}
	return u.HomeDir, nil
}

func rootConfigDir() (string, error) {
	home, err := rootHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return filepath.Join(home, ".config"), nil
}

func rootDataDir() (string, error) {
	home, err := rootHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return filepath.Join(home, ".local", "share"), nil
}

func rootCacheDir() (string, error) {
	home, err := rootHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Caches"), nil
	}
	return filepath.Join(home, ".cache"), nil
}

// Overridable in tests to exercise root path resolution and the
// passwd-unavailable fallback.
var (
	rootConfigDirResolver = rootConfigDir
	rootDataDirResolver   = rootDataDir
	rootCacheDirResolver  = rootCacheDir
)

// EnsureDirectories creates all required directories if they don't exist.
func EnsureDirectories() error {
	MigrateLegacyLayout()

	paths := ResolvePaths()

	dirs := []string{
		paths.ConfigDir,
		paths.DataDir,
		paths.CacheDir,
		paths.BackupsDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}

	return nil
}

// ClaudeCodeHooksDir returns the hooks directory for Claude Code.
func ClaudeCodeHooksDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "hooks")
}

// CursorConfigDir returns the config directory for Cursor.
func CursorConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cursor")
}

// CursorHooksFile returns the hooks file path for Cursor.
func CursorHooksFile() string {
	return filepath.Join(CursorConfigDir(), "hooks.json")
}
