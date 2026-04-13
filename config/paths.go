package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// getConfigDir returns the configuration directory for gryph.
//
// Resolution order:
//  1. $XDG_CONFIG_HOME/gryph if the environment variable is set (all platforms).
//     Go's os.UserConfigDir honors this on Linux but not on macOS or Windows;
//     we extend it to all platforms so users who organize their configs under
//     an XDG-style layout get a consistent location across systems.
//  2. $HOME/.config/gryph if that directory already exists (all platforms).
//     This lets users who have opted into the XDG layout on macOS/Windows
//     keep their configs in one place without setting an environment variable.
//  3. Platform default via os.UserConfigDir (Linux $XDG_CONFIG_HOME or
//     $HOME/.config, macOS $HOME/Library/Application Support, Windows %AppData%).
func getConfigDir() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "gryph")
	}
	if home, err := os.UserHomeDir(); err == nil {
		xdgDefault := filepath.Join(home, ".config", "gryph")
		if info, err := os.Stat(xdgDefault); err == nil && info.IsDir() {
			return xdgDefault
		}
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		// Fallback to home directory
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "gryph")
}

// getDataDir returns the data directory for gryph.
//
// Resolution order:
//  1. $XDG_DATA_HOME/gryph if the environment variable is set (all platforms).
//     Mirrors the config-dir behaviour above — respect the XDG base dir spec
//     everywhere, not just on Linux, for users who want a single layout.
//  2. $HOME/.local/share/gryph if that directory already exists (all
//     platforms), for users who have opted into the XDG layout on
//     macOS/Windows without setting environment variables.
//  3. Platform default: Linux $HOME/.local/share, macOS $HOME/Library/
//     Application Support, Windows %LocalAppData%.
func getDataDir() string {
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "gryph")
	}
	if home, err := os.UserHomeDir(); err == nil {
		xdgDefault := filepath.Join(home, ".local", "share", "gryph")
		if info, err := os.Stat(xdgDefault); err == nil && info.IsDir() {
			return xdgDefault
		}
	}

	switch runtime.GOOS {
	case "linux":
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
