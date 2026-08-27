package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/safedep/dry/log"
)

const (
	legacyLeaf           = "gryph"
	legacyConfigFileName = "config.yaml"
	migrationMarkerName  = "migration.json"
)

// MigrationMove records one directory move performed by the migration.
type MigrationMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// MigrationRecord describes a completed layout migration. It is handed to
// the CLI through a marker file, so the self-audit entry can be written by
// whichever later process opens the store first.
type MigrationRecord struct {
	Moves    []MigrationMove `json:"moves"`
	Warnings []string        `json:"warnings,omitempty"`
}

var migrateOnce sync.Once

// MigrateLegacyLayout moves the pre safedep/gryph directory tree to the new
// layout, once per install. The fast path is a few stat calls. Failures
// never stop the caller: gryph runs in the agent hook path and a migration
// problem must not break the agent.
func MigrateLegacyLayout() {
	migrateOnce.Do(migrateLegacyLayout)
}

func migrateLegacyLayout() {
	// An elevated run must not move the invoking user's files. The next
	// non-elevated run migrates them.
	if isSudoElevation() {
		return
	}

	record := MigrationRecord{}

	// The cache directory is not migrated: gryph stores nothing in it. Only
	// config and data move, and on macOS those resolve to one pair.
	kinds := []struct {
		envKey string
		legacy string
		target string
	}{
		{configDirEnvKey, legacyConfigDir(), getConfigDir()},
		{dataDirEnvKey, legacyDataDir(), getDataDir()},
	}

	seen := map[string]bool{}
	for _, kind := range kinds {
		if os.Getenv(kind.envKey) != "" {
			continue
		}
		pair := kind.legacy + "\x00" + kind.target
		if kind.legacy == kind.target || seen[pair] {
			continue
		}
		seen[pair] = true

		moved, warning := moveLegacyDir(kind.legacy, kind.target)
		if moved {
			record.Moves = append(record.Moves, MigrationMove{From: kind.legacy, To: kind.target})
		}
		if warning != "" {
			record.Warnings = append(record.Warnings, warning)
		}
	}

	if os.Getenv(cacheDirEnvKey) == "" {
		// EnsureDirectories created the legacy cache directory empty, so a
		// plain remove clears it. os.Remove refuses a non-empty directory.
		if err := os.Remove(legacyCacheDir()); err != nil && !os.IsNotExist(err) {
			log.Debugf("config migration: legacy cache directory not removed: %v", err)
		}
	}

	normalizeConfigFileName(getConfigDir())

	if len(record.Moves) > 0 {
		writeMigrationMarker(record)
	}
}

// moveLegacyDir renames legacy to target when only legacy exists. It
// returns whether the move happened and a warning for the self-audit record.
func moveLegacyDir(legacy, target string) (bool, string) {
	if _, err := os.Stat(legacy); err != nil {
		return false, ""
	}
	if _, err := os.Stat(target); err == nil {
		warning := fmt.Sprintf("legacy directory %s and new directory %s both exist: gryph uses the new directory, reconcile and remove the legacy one by hand", legacy, target)
		log.Warnf("config migration: %s", warning)
		return false, warning
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		log.Warnf("config migration: failed to create %s: %v", filepath.Dir(target), err)
		return false, ""
	}
	if err := os.Rename(legacy, target); err != nil {
		// A concurrent gryph process can win the rename. Its result is the
		// same, so a vanished legacy directory is not a failure.
		if os.IsNotExist(err) {
			return false, ""
		}
		log.Warnf("config migration: failed to move %s to %s, move it by hand: %v", legacy, target, err)
		return false, ""
	}
	return true, ""
}

// normalizeConfigFileName renames config.yaml to config.yml in configDir.
// Viper prefers the yaml extension, so the two names must never coexist or
// writes to config.yml would be shadowed by a stale config.yaml.
func normalizeConfigFileName(configDir string) {
	legacy := filepath.Join(configDir, legacyConfigFileName)
	target := filepath.Join(configDir, configFileName)

	if _, err := os.Stat(legacy); err != nil {
		return
	}
	if _, err := os.Stat(target); err == nil {
		return
	}
	if err := os.Rename(legacy, target); err != nil && !os.IsNotExist(err) {
		log.Warnf("config migration: failed to rename %s to %s: %v", legacy, target, err)
	}
}

// Legacy resolvers reproduce the pre safedep/gryph resolution, including the
// XDG preference on every platform and the preexisting-directory checks.
// They are the only source of legacy paths.

func legacyConfigDir() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, legacyLeaf)
	}
	if home, err := os.UserHomeDir(); err == nil {
		xdgDefault := filepath.Join(home, ".config", legacyLeaf)
		if info, err := os.Stat(xdgDefault); err == nil && info.IsDir() {
			return xdgDefault
		}
	}
	return filepath.Join(configBaseDir(), legacyLeaf)
}

func legacyDataDir() string {
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, legacyLeaf)
	}
	if home, err := os.UserHomeDir(); err == nil {
		xdgDefault := filepath.Join(home, ".local", "share", legacyLeaf)
		if info, err := os.Stat(xdgDefault); err == nil && info.IsDir() {
			return xdgDefault
		}
	}
	return filepath.Join(dataBaseDir(), legacyLeaf)
}

func legacyCacheDir() string {
	return filepath.Join(cacheBaseDir(), legacyLeaf)
}

func migrationMarkerPath() string {
	return filepath.Join(getDataDir(), migrationMarkerName)
}

func writeMigrationMarker(record MigrationRecord) {
	data, err := json.Marshal(record)
	if err != nil {
		log.Warnf("config migration: failed to encode marker: %v", err)
		return
	}
	// The data directory may not exist yet when only the config directory
	// moved.
	if err := os.MkdirAll(getDataDir(), 0o700); err != nil {
		log.Warnf("config migration: failed to create data directory for marker: %v", err)
		return
	}
	if err := os.WriteFile(migrationMarkerPath(), data, 0o600); err != nil {
		log.Warnf("config migration: failed to write marker: %v", err)
	}
}

// ConsumeMigrationMarker returns the pending migration record and removes
// the marker file. It returns nil when no migration is pending. The CLI
// calls it after the store opens to write the self-audit entry, because this
// package must not depend on storage.
func ConsumeMigrationMarker() *MigrationRecord {
	path := migrationMarkerPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("config migration: failed to read marker: %v", err)
		}
		return nil
	}

	if err := os.Remove(path); err != nil {
		log.Warnf("config migration: failed to remove marker: %v", err)
	}

	record := &MigrationRecord{}
	if err := json.Unmarshal(data, record); err != nil {
		log.Warnf("config migration: failed to decode marker: %v", err)
		return nil
	}

	return record
}
