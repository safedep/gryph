package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// getConfigDir returns the configuration directory for gryph.
func getConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		// Fallback to home directory
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "gryph")
}

// getDataDir returns the data directory for gryph.
// This follows XDG on Linux, Application Support on macOS, and LocalAppData on Windows.
func getDataDir() string {
	switch runtime.GOOS {
	case "linux":
		// Follow XDG Base Directory Specification
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			return filepath.Join(xdgData, "gryph")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "gryph")

	case "darwin":
		// macOS: Use Application Support (same as config)
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "gryph")

	case "windows":
		// Windows: Use LocalAppData
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "gryph")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Local", "gryph")

	default:
		// Fallback: use config directory
		return getConfigDir()
	}
}

// getCacheDir returns the cache directory for gryph.
func getCacheDir() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		// Fallback to home directory
		home, _ := os.UserHomeDir()
		switch runtime.GOOS {
		case "darwin":
			cacheDir = filepath.Join(home, "Library", "Caches")
		case "windows":
			cacheDir = filepath.Join(home, "AppData", "Local")
		default:
			cacheDir = filepath.Join(home, ".cache")
		}
	}
	return filepath.Join(cacheDir, "gryph")
}

// EnsureDirectories creates all required directories if they don't exist.
func EnsureDirectories() error {
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
