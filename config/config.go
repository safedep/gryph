// Package config provides configuration management using Viper.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Agent names as constants. Must be in sync with agent/adapter.go.
// We cannot depend on agent/adapter.go because it would create a circular dependency.
const (
	agentNameClaudeCode = "claude-code"
	agentNameCursor     = "cursor"
)

// LoggingLevel represents the verbosity level for logging.
// This is for agent event logging only. Not for our own internal logging.
type LoggingLevel string

const (
	// LoggingMinimal logs action type, file path, timestamp, result only.
	LoggingMinimal LoggingLevel = "minimal"
	// LoggingStandard adds diff stats, command exit codes, truncated output.
	LoggingStandard LoggingLevel = "standard"
	// LoggingFull adds raw events, conversation context, full command output, file diffs.
	LoggingFull LoggingLevel = "full"
)

// ColorMode represents the color output mode.
type ColorMode string

const (
	// ColorAuto automatically detects terminal support.
	ColorAuto ColorMode = "auto"
	// ColorAlways always uses colors.
	ColorAlways ColorMode = "always"
	// ColorNever never uses colors.
	ColorNever ColorMode = "never"
)

// TimezoneMode represents the timezone display mode.
type TimezoneMode string

const (
	// TimezoneLocal uses the local timezone.
	TimezoneLocal TimezoneMode = "local"
	// TimezoneUTC uses UTC.
	TimezoneUTC TimezoneMode = "utc"
)

// Config holds all configuration values.
type Config struct {
	Logging LoggingConfig `mapstructure:"logging"`
	Storage StorageConfig `mapstructure:"storage"`
	Privacy PrivacyConfig `mapstructure:"privacy"`
	Filters FiltersConfig `mapstructure:"filters"`
	Agents  AgentsConfig  `mapstructure:"agents"`
	Display DisplayConfig `mapstructure:"display"`
	Streams StreamsConfig `mapstructure:"streams"`
}

// LoggingConfig holds logging-related settings.
type LoggingConfig struct {
	Level           LoggingLevel `mapstructure:"level"`
	StdoutMaxChars  int          `mapstructure:"stdout_max_chars"`
	StderrMaxChars  int          `mapstructure:"stderr_max_chars"`
	ContextMaxChars int          `mapstructure:"context_max_chars"`
}

// StorageConfig holds storage-related settings.
type StorageConfig struct {
	Path          string `mapstructure:"path"`
	RetentionDays int    `mapstructure:"retention_days"`
}

// PrivacyConfig holds privacy-related settings.
type PrivacyConfig struct {
	HashFileContents bool     `mapstructure:"hash_file_contents"`
	SensitivePaths   []string `mapstructure:"sensitive_paths"`
	RedactPatterns   []string `mapstructure:"redact_patterns"`
}

// FiltersConfig holds content filter settings.
type FiltersConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// AgentConfig holds settings for a specific agent.
type AgentConfig struct {
	Enabled      bool         `mapstructure:"enabled"`
	LoggingLevel LoggingLevel `mapstructure:"logging_level,omitempty"`
}

// AgentsConfig holds per-agent settings.
type AgentsConfig struct {
	ClaudeCode AgentConfig `mapstructure:"claude-code"`
	Cursor     AgentConfig `mapstructure:"cursor"`
}

// DisplayConfig holds display-related settings.
type DisplayConfig struct {
	Colors   ColorMode    `mapstructure:"colors"`
	Timezone TimezoneMode `mapstructure:"timezone"`
}

// StreamsConfig holds stream target settings.
type StreamsConfig struct {
	Targets []StreamTargetConfig `mapstructure:"targets"`
}

// StreamTargetConfig holds settings for a single stream target.
type StreamTargetConfig struct {
	Name    string         `mapstructure:"name"`
	Type    string         `mapstructure:"type"`
	Enabled bool           `mapstructure:"enabled"`
	Config  map[string]any `mapstructure:"config"`
}

// Paths holds resolved filesystem paths.
type Paths struct {
	ConfigFile   string
	ConfigDir    string
	DataDir      string
	DatabaseFile string
	CacheDir     string
	BackupsDir   string
}

// Load loads configuration from the given path or default locations.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Set config type
	v.SetConfigType("yaml")

	// Determine config file path
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		paths := ResolvePaths()

		v.SetConfigName("config")
		v.AddConfigPath(paths.ConfigDir)
	}

	// Bind environment variables
	v.SetEnvPrefix("GRYPH")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Unmarshal into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	// Validate config
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Default returns a Config with all default values.
func Default() *Config {
	v := viper.New()
	setDefaults(v)

	var cfg Config
	_ = v.Unmarshal(&cfg)

	return &cfg
}

// ResolvePaths returns the resolved filesystem paths for the current platform.
func ResolvePaths() *Paths {
	configDir := getConfigDir()
	dataDir := getDataDir()
	cacheDir := getCacheDir()

	return &Paths{
		ConfigFile:   filepath.Join(configDir, "config.yaml"),
		ConfigDir:    configDir,
		DataDir:      dataDir,
		DatabaseFile: filepath.Join(dataDir, "audit.db"),
		CacheDir:     cacheDir,
		BackupsDir:   filepath.Join(dataDir, "backups"),
	}
}

// GetDatabasePath returns the resolved database path from config or default.
func (c *Config) GetDatabasePath() string {
	if c.Storage.Path != "" {
		return c.Storage.Path
	}

	paths := ResolvePaths()
	return paths.DatabaseFile
}

// ShouldUseColors returns true if colors should be used based on config and terminal.
func (c *Config) ShouldUseColors() bool {
	switch c.Display.Colors {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	default:
		// Auto: check if stdout is a terminal
		fileInfo, _ := os.Stdout.Stat()
		return (fileInfo.Mode() & os.ModeCharDevice) != 0
	}
}

// GetAgentLoggingLevel returns the logging level for a specific agent.
// Falls back to global level if not set.
func (c *Config) GetAgentLoggingLevel(agentName string) LoggingLevel {
	switch agentName {
	case agentNameClaudeCode:
		if c.Agents.ClaudeCode.LoggingLevel != "" {
			return c.Agents.ClaudeCode.LoggingLevel
		}
	case agentNameCursor:
		if c.Agents.Cursor.LoggingLevel != "" {
			return c.Agents.Cursor.LoggingLevel
		}
	}

	return c.Logging.Level
}

// IsAgentEnabled returns true if the given agent is enabled.
func (c *Config) IsAgentEnabled(agentName string) bool {
	switch agentName {
	case agentNameClaudeCode:
		return c.Agents.ClaudeCode.Enabled
	case agentNameCursor:
		return c.Agents.Cursor.Enabled
	default:
		return true
	}
}
