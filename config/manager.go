package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Manager provides a high-level API for configuration management.
// It encapsulates viper and handles defaults, file persistence, and validation.
type Manager struct {
	v          *viper.Viper
	configPath string
}

// NewManager creates a new configuration manager.
// It initializes with defaults and reads the config file if it exists.
func NewManager(configPath string) (*Manager, error) {
	v := viper.New()

	setDefaults(v)

	v.SetConfigType("yaml")
	v.SetConfigFile(configPath)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to read config: %w", err)
			}
		}
	}

	return &Manager{
		v:          v,
		configPath: configPath,
	}, nil
}

// Get returns the value for a given key.
// Returns nil if the key does not exist.
func (m *Manager) Get(key string) interface{} {
	return m.v.Get(key)
}

// Set sets a configuration value and persists it to the config file.
// It ensures the config directory exists and writes a complete config file.
func (m *Manager) Set(key string, value interface{}) error {
	configDir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	m.v.Set(key, value)

	configMap := m.v.AllSettings()
	data, err := yaml.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(m.configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// Reset removes the config file, effectively resetting to defaults.
func (m *Manager) Reset() error {
	if err := os.Remove(m.configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove config: %w", err)
	}

	m.v = viper.New()
	setDefaults(m.v)
	m.v.SetConfigType("yaml")
	m.v.SetConfigFile(m.configPath)

	return nil
}

// AllSettings returns all configuration values as a map.
// This includes defaults merged with any file-based overrides.
func (m *Manager) AllSettings() map[string]interface{} {
	return m.v.AllSettings()
}

// ConfigPath returns the path to the configuration file.
func (m *Manager) ConfigPath() string {
	return m.configPath
}

// HasKey returns true if the given key exists in the configuration.
func (m *Manager) HasKey(key string) bool {
	return m.v.IsSet(key)
}

// ParseValue parses a string value into an appropriate Go type.
// It handles booleans and simple arrays.
func ParseValue(value string) interface{} {
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		inner := strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
		parts := strings.Split(inner, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		return parts
	}
	return value
}
