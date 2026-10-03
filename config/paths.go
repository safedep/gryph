package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"

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

// baseDirs holds the base directories of one account. The leaf
// defaultHomeRelativePath is appended to each.
type baseDirs struct {
	config string
	data   string
	cache  string
}

// accountBaseDirsResolver is overridable in tests.
var accountBaseDirsResolver = accountBaseDirs

// resolveDir applies the shared resolution order for one directory kind.
// Under a managed config the environment has no say: the directory comes
// from the account database of the real user, so a stray HOME, XDG_* or
// GRYPH_*_DIR variable cannot move the state that a managed host governs.
// Otherwise: the GRYPH_* override, then the sudo guard, then the XDG base,
// then the platform base. XDG variables are honored on every platform, not
// only where os.UserConfigDir does, so users with an XDG layout on macOS or
// Windows keep one location across systems. A relative XDG value is
// ignored, as the XDG spec requires.
func resolveDir(kind func(baseDirs) string, overrideEnvKey, xdgEnvKey string, rootBase func() (string, error), platformBase func() string) string {
	if managedConfigActive() {
		dirs, err := accountBaseDirsResolver()
		if err == nil {
			return filepath.Join(kind(dirs), defaultHomeRelativePath)
		}
		// No resolvable account entry, e.g. scratch containers. Without an
		// account database there is no user switching, so the environment
		// cannot point at another user's state.
		log.Warnf("failed to resolve the account directories, using the platform defaults: %v", err)
		return filepath.Join(platformBase(), defaultHomeRelativePath)
	}
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
	return resolveDir(func(d baseDirs) string { return d.config }, configDirEnvKey, "XDG_CONFIG_HOME", rootConfigDirResolver, configBaseDir)
}

// getDataDir returns the data directory for gryph.
func getDataDir() string {
	return resolveDir(func(d baseDirs) string { return d.data }, dataDirEnvKey, "XDG_DATA_HOME", rootDataDirResolver, dataBaseDir)
}

// getCacheDir returns the cache directory for gryph.
func getCacheDir() string {
	return resolveDir(func(d baseDirs) string { return d.cache }, cacheDirEnvKey, "XDG_CACHE_HOME", rootCacheDirResolver, cacheBaseDir)
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
	return accountHomeDir(0)
}

// accountHomeDir returns the home directory of uid from the account
// database, never from the environment.
func accountHomeDir(uid int) (string, error) {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", fmt.Errorf("failed to resolve the home directory of uid %d: %w", uid, err)
	}
	if u.HomeDir == "" {
		return "", fmt.Errorf("uid %d has no home directory", uid)
	}
	return u.HomeDir, nil
}

func rootConfigDir() (string, error) {
	home, err := rootHomeDir()
	if err != nil {
		return "", err
	}
	return baseDirsFor(home).config, nil
}

func rootDataDir() (string, error) {
	home, err := rootHomeDir()
	if err != nil {
		return "", err
	}
	return baseDirsFor(home).data, nil
}

func rootCacheDir() (string, error) {
	home, err := rootHomeDir()
	if err != nil {
		return "", err
	}
	return baseDirsFor(home).cache, nil
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
