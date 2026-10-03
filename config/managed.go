package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/safedep/gryph/platform/nofollow"
	"github.com/spf13/viper"
)

// ManagedConfig is the section of a system managed configuration that
// drives gryph install --managed.
type ManagedConfig struct {
	// Agents is the allowlist: every agent in it gets a managed hook entry,
	// also when the agent is not installed yet. An agent that leaves the
	// list loses its entry on the next run.
	Agents []string `mapstructure:"agents"`
	// LockHooks names the agents whose managed entry also turns on the
	// agent's own lock, so only managed hooks run. Off by default, because
	// the lock also stops the developer's own hooks.
	LockHooks []string `mapstructure:"lock_hooks"`
	// Binary is the absolute path of the gryph binary that the hook entries
	// name. Empty takes the platform default.
	Binary string `mapstructure:"binary"`
}

// Locked reports whether the lock switch is on for the agent.
func (m ManagedConfig) Locked(agent string) bool {
	return slices.Contains(m.LockHooks, agent)
}

func validateManagedConfig(m ManagedConfig) error {
	seen := map[string]bool{}
	for _, a := range m.Agents {
		if a == "" {
			return errors.New("managed.agents holds an empty name")
		}
		if seen[a] {
			return fmt.Errorf("managed.agents names %s twice", a)
		}
		seen[a] = true
	}
	for _, a := range m.LockHooks {
		if !seen[a] {
			return fmt.Errorf("managed.lock_hooks names %s, which is not in managed.agents", a)
		}
	}
	if m.Binary != "" && !filepath.IsAbs(m.Binary) {
		return fmt.Errorf("managed.binary %q is not an absolute path", m.Binary)
	}
	return nil
}

// ManagedBinaryPath returns the binary that the managed hook entries name:
// the configured path, or the platform default. The default sits in a
// directory that only root can write, so a hook entry never points at a
// binary a user can replace.
func ManagedBinaryPath(cfg *Config) string {
	if cfg != nil && cfg.Managed.Binary != "" {
		return cfg.Managed.Binary
	}
	return managedBinaryDefault()
}

// VerifyManagedPath checks that root owns path and every directory above
// it, and that no component is writable by another user. It is the trust
// check of the managed configuration, for any file an administrator owns.
func VerifyManagedPath(path string) error {
	return managedPathTrusted(path)
}

// ReadTrustedFile reads a file that an administrator hands to Gryph, such
// as the input of gryph install --managed. The file must pass
// VerifyManagedPath, and the open does not follow a link in its last
// component. A file that another user can write is refused, because root
// would otherwise act on that user's input.
func ReadTrustedFile(path string) ([]byte, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s is not an absolute path", path)
	}
	if err := VerifyManagedPath(path); err != nil {
		return nil, err
	}
	data, err := nofollow.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Parse loads a configuration from YAML with the defaults and the
// validation of Load, and from no other source: no managed file, no
// per-user file, no environment variable.
func Parse(data []byte) (*Config, error) {
	v := viper.New()
	setDefaults(v)
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("error reading config: %w", err)
	}
	return unmarshalConfig(v)
}

// WriteManagedFile puts data at path in the managed directory tree. It
// creates the directories with mode 0755, writes through a temporary file
// and a rename, and refuses a link at path. It reports false and writes
// nothing when the file already holds data, so a repeated run changes
// nothing.
func WriteManagedFile(path string, data []byte) (changed bool, err error) {
	existing, err := nofollow.ReadFile(path)
	switch {
	case err == nil && bytes.Equal(existing, data):
		return false, nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := nofollow.WriteFile(path, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// ManagedConfigPath returns the path of the managed configuration file,
// or "" when the platform has no managed location.
func ManagedConfigPath() string {
	dir := systemConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, configFileName)
}
