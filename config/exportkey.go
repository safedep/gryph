package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/safedep/dry/log"
)

const exportKeySize = 32

// ExportKeyFile returns the path of the export key. It sits next to the
// database, because it keys the digests of the events in that database.
func (c *Config) ExportKeyFile() string {
	return filepath.Join(filepath.Dir(c.GetDatabasePath()), "export.key")
}

// LoadOrCreateExportKey reads the export key at path. When the file does
// not exist, it writes a new random key with mode 0600. The key never
// leaves the machine. Two processes that create the key at the same time
// get the same key, because only the first hard link wins.
func LoadOrCreateExportKey(path string) ([]byte, error) {
	key, err := readExportKey(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return key, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create export key directory: %w", err)
	}
	key = make([]byte, exportKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate export key: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".export.key-*")
	if err != nil {
		return nil, fmt.Errorf("create export key: %w", err)
	}
	defer func() {
		if err := os.Remove(tmp.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Warnf("export key: remove %s: %v", tmp.Name(), err)
		}
	}()
	if _, err := tmp.Write(key); err != nil {
		return nil, fmt.Errorf("write export key: %w", errors.Join(err, tmp.Close()))
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("write export key: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return nil, fmt.Errorf("write export key: %w", err)
	}

	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return readExportKey(path)
		}
		return nil, fmt.Errorf("write export key: %w", err)
	}
	return key, nil
}

func readExportKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read export key: %w", err)
	}
	if len(key) != exportKeySize {
		return nil, fmt.Errorf("export key %s: want %d bytes, got %d", path, exportKeySize, len(key))
	}
	return key, nil
}
