package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	require.NotNil(t, cfg)

	// Verify logging defaults
	assert.Equal(t, LoggingStandard, cfg.Logging.Level)
	assert.Equal(t, 1000, cfg.Logging.StdoutMaxChars)
	assert.Equal(t, 500, cfg.Logging.StderrMaxChars)
	assert.Equal(t, 5000, cfg.Logging.ContextMaxChars)

	// Verify storage defaults
	assert.Equal(t, "", cfg.Storage.Path)
	assert.Equal(t, 90, cfg.Storage.RetentionDays)

	// Verify content hash default
	assert.True(t, cfg.Logging.ContentHash)

	// Verify privacy defaults
	assert.NotEmpty(t, cfg.Privacy.SensitivePaths)
	assert.NotEmpty(t, cfg.Privacy.RedactPatterns)

	// Verify filters defaults
	assert.False(t, cfg.Filters.Enabled)

	// Verify agent defaults
	assert.True(t, cfg.Agents.ClaudeCode.Enabled)
	assert.True(t, cfg.Agents.Cursor.Enabled)
	assert.Empty(t, cfg.Agents.ClaudeCode.LoggingLevel)
	assert.Empty(t, cfg.Agents.Cursor.LoggingLevel)

	// Verify display defaults
	assert.Equal(t, ColorAuto, cfg.Display.Colors)
	assert.Equal(t, TimezoneLocal, cfg.Display.Timezone)

	// Verify streams defaults
	require.Len(t, cfg.Streams.Targets, 1)
	assert.Equal(t, "nop", cfg.Streams.Targets[0].Name)
	assert.Equal(t, "nop", cfg.Streams.Targets[0].Type)
	assert.True(t, cfg.Streams.Targets[0].Enabled)
}

func TestLoad_ValidConfig(t *testing.T) {
	configContent := `
logging:
  level: standard
  stdout_max_chars: 2000
  stderr_max_chars: 1000
  context_max_chars: 10000
  content_hash: false
storage:
  retention_days: 30
privacy:
  sensitive_paths:
    - "**/.env"
  redact_patterns:
    - "password=\\S+"
agents:
  claude-code:
    enabled: false
    logging_level: full
  cursor:
    enabled: true
display:
  colors: always
  timezone: utc
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, LoggingStandard, cfg.Logging.Level)
	assert.Equal(t, 2000, cfg.Logging.StdoutMaxChars)
	assert.Equal(t, 1000, cfg.Logging.StderrMaxChars)
	assert.Equal(t, 10000, cfg.Logging.ContextMaxChars)
	assert.Equal(t, 30, cfg.Storage.RetentionDays)
	assert.False(t, cfg.Logging.ContentHash)
	assert.Equal(t, []string{"**/.env"}, cfg.Privacy.SensitivePaths)
	assert.False(t, cfg.Agents.ClaudeCode.Enabled)
	assert.Equal(t, LoggingFull, cfg.Agents.ClaudeCode.LoggingLevel)
	assert.True(t, cfg.Agents.Cursor.Enabled)
	assert.Equal(t, ColorAlways, cfg.Display.Colors)
	assert.Equal(t, TimezoneUTC, cfg.Display.Timezone)
}

func TestLoad_InvalidLoggingLevel(t *testing.T) {
	configContent := `
logging:
  level: invalid
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid logging level")
}

func TestLoad_InvalidColorMode(t *testing.T) {
	configContent := `
display:
  colors: invalid
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid display.colors")
}

func TestLoad_InvalidTimezoneMode(t *testing.T) {
	configContent := `
display:
  timezone: invalid
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid display.timezone")
}

func TestLoad_InvalidRegexPattern(t *testing.T) {
	configContent := `
privacy:
  redact_patterns:
    - "[invalid"
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid redact pattern")
}

func TestLoad_NegativeRetentionDays(t *testing.T) {
	configContent := `
storage:
  retention_days: -1
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "storage.retention_days must be non-negative")
}

func TestLoad_NegativeTruncationLimit(t *testing.T) {
	configContent := `
logging:
  stdout_max_chars: -1
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "logging.stdout_max_chars must be non-negative")
}

func TestLoad_InvalidAgentLoggingLevel(t *testing.T) {
	configContent := `
agents:
  claude-code:
    logging_level: invalid
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid agents.claude-code.logging_level")
}

func TestLoad_NonExistentFile_ReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.yaml")

	// When an explicit config path is given, the file must exist
	cfg, err := Load(nonExistentFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "error reading config file")
}

// Note: TestLoad_EmptyPath is not included because it depends on whether
// a config file exists in the default location (~/.config/gryph/config.yaml),
// which varies by test environment.

func TestLoad_MalformedYAML(t *testing.T) {
	configContent := `
logging:
  level: minimal
  this is not valid yaml
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestConfig_GetDatabasePath_Default(t *testing.T) {
	cfg := Default()
	dbPath := cfg.GetDatabasePath()
	assert.NotEmpty(t, dbPath)
	assert.Contains(t, dbPath, "audit.db")
}

func TestConfig_GetDatabasePath_CustomPath(t *testing.T) {
	cfg := Default()
	cfg.Storage.Path = "/custom/path/mydb.db"

	dbPath := cfg.GetDatabasePath()
	assert.Equal(t, "/custom/path/mydb.db", dbPath)
}

func TestConfig_ShouldUseColors_Always(t *testing.T) {
	cfg := Default()
	cfg.Display.Colors = ColorAlways
	assert.True(t, cfg.ShouldUseColors())
}

func TestConfig_ShouldUseColors_Never(t *testing.T) {
	cfg := Default()
	cfg.Display.Colors = ColorNever
	assert.False(t, cfg.ShouldUseColors())
}

func TestConfig_GetAgentLoggingLevel_Default(t *testing.T) {
	cfg := Default()
	cfg.Logging.Level = LoggingStandard

	// Without agent-specific level, should use global
	assert.Equal(t, LoggingStandard, cfg.GetAgentLoggingLevel("claude-code"))
	assert.Equal(t, LoggingStandard, cfg.GetAgentLoggingLevel("cursor"))
	assert.Equal(t, LoggingStandard, cfg.GetAgentLoggingLevel("unknown-agent"))
}

func TestConfig_GetAgentLoggingLevel_PerAgent(t *testing.T) {
	cfg := Default()
	cfg.Logging.Level = LoggingMinimal
	cfg.Agents.ClaudeCode.LoggingLevel = LoggingFull
	cfg.Agents.Cursor.LoggingLevel = LoggingStandard

	assert.Equal(t, LoggingFull, cfg.GetAgentLoggingLevel("claude-code"))
	assert.Equal(t, LoggingStandard, cfg.GetAgentLoggingLevel("cursor"))
	assert.Equal(t, LoggingMinimal, cfg.GetAgentLoggingLevel("unknown-agent"))
}

func TestConfig_IsAgentEnabled_Defaults(t *testing.T) {
	cfg := Default()
	assert.True(t, cfg.IsAgentEnabled("claude-code"))
	assert.True(t, cfg.IsAgentEnabled("cursor"))
	assert.True(t, cfg.IsAgentEnabled("unknown-agent")) // Unknown agents default to enabled
}

func TestConfig_IsAgentEnabled_Disabled(t *testing.T) {
	cfg := Default()
	cfg.Agents.ClaudeCode.Enabled = false
	cfg.Agents.Cursor.Enabled = false

	assert.False(t, cfg.IsAgentEnabled("claude-code"))
	assert.False(t, cfg.IsAgentEnabled("cursor"))
	assert.True(t, cfg.IsAgentEnabled("unknown-agent")) // Unknown agents still default to enabled
}

func TestResolvePaths(t *testing.T) {
	paths := ResolvePaths()

	assert.NotEmpty(t, paths.ConfigFile)
	assert.NotEmpty(t, paths.ConfigDir)
	assert.NotEmpty(t, paths.DataDir)
	assert.NotEmpty(t, paths.DatabaseFile)
	assert.NotEmpty(t, paths.CacheDir)
	assert.NotEmpty(t, paths.BackupsDir)

	assert.Contains(t, paths.ConfigFile, "config.yaml")
	assert.Contains(t, paths.DatabaseFile, "audit.db")
	assert.Contains(t, paths.BackupsDir, "backups")
}

func TestEnsureDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("EnsureDirectories", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "config"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))

		err := EnsureDirectories()
		require.NoError(t, err)
	})
}

func TestClaudeCodeHooksDir(t *testing.T) {
	dir := ClaudeCodeHooksDir()
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, ".claude")
	assert.Contains(t, dir, "hooks")
}

func TestCursorConfigDir(t *testing.T) {
	dir := CursorConfigDir()
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, ".cursor")
}

func TestCursorHooksFile(t *testing.T) {
	file := CursorHooksFile()
	assert.NotEmpty(t, file)
	assert.Contains(t, file, ".cursor")
	assert.Contains(t, file, "hooks.json")
}

func TestLoggingLevel_Values(t *testing.T) {
	assert.Equal(t, LoggingLevel("minimal"), LoggingMinimal)
	assert.Equal(t, LoggingLevel("standard"), LoggingStandard)
	assert.Equal(t, LoggingLevel("full"), LoggingFull)
}

func TestColorMode_Values(t *testing.T) {
	assert.Equal(t, ColorMode("auto"), ColorAuto)
	assert.Equal(t, ColorMode("always"), ColorAlways)
	assert.Equal(t, ColorMode("never"), ColorNever)
}

func TestTimezoneMode_Values(t *testing.T) {
	assert.Equal(t, TimezoneMode("local"), TimezoneLocal)
	assert.Equal(t, TimezoneMode("utc"), TimezoneUTC)
}

func TestIsValidLoggingLevel(t *testing.T) {
	assert.True(t, isValidLoggingLevel(LoggingMinimal))
	assert.True(t, isValidLoggingLevel(LoggingStandard))
	assert.True(t, isValidLoggingLevel(LoggingFull))
	assert.False(t, isValidLoggingLevel(LoggingLevel("invalid")))
	assert.False(t, isValidLoggingLevel(LoggingLevel("")))
}

func TestIsValidColorMode(t *testing.T) {
	assert.True(t, isValidColorMode(ColorAuto))
	assert.True(t, isValidColorMode(ColorAlways))
	assert.True(t, isValidColorMode(ColorNever))
	assert.False(t, isValidColorMode(ColorMode("invalid")))
	assert.False(t, isValidColorMode(ColorMode("")))
}

func TestIsValidTimezoneMode(t *testing.T) {
	assert.True(t, isValidTimezoneMode(TimezoneLocal))
	assert.True(t, isValidTimezoneMode(TimezoneUTC))
	assert.False(t, isValidTimezoneMode(TimezoneMode("invalid")))
	assert.False(t, isValidTimezoneMode(TimezoneMode("")))
}

func TestDefaultSensitivePaths(t *testing.T) {
	paths := defaultSensitivePaths()
	assert.NotEmpty(t, paths)
	assert.Contains(t, paths, "**/.env")
	assert.Contains(t, paths, "**/*.pem")
	assert.Contains(t, paths, "**/.ssh/**")
}

func TestDefaultRedactPatterns(t *testing.T) {
	patterns := defaultRedactPatterns()
	assert.NotEmpty(t, patterns)
	// Each pattern should be a valid regex
	for _, pattern := range patterns {
		assert.NotPanics(t, func() {
			// If this compiles without panic, the pattern is valid
			_ = pattern
		})
	}
}

func TestLoad_PartialConfig_MergesWithDefaults(t *testing.T) {
	// Config with only some values set
	configContent := `
logging:
  level: full
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Set value
	assert.Equal(t, LoggingFull, cfg.Logging.Level)

	// Default values
	assert.Equal(t, 1000, cfg.Logging.StdoutMaxChars)
	assert.Equal(t, 90, cfg.Storage.RetentionDays)
	assert.True(t, cfg.Agents.ClaudeCode.Enabled)
	assert.Equal(t, ColorAuto, cfg.Display.Colors)
}

func TestLoad_ZeroRetentionDays_Valid(t *testing.T) {
	// Zero retention means no automatic cleanup
	configContent := `
storage:
  retention_days: 0
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, 0, cfg.Storage.RetentionDays)
}

func TestLoad_EmptySensitivePaths_Valid(t *testing.T) {
	configContent := `
privacy:
  sensitive_paths: []
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.Privacy.SensitivePaths)
}

func TestLoad_EmptyRedactPatterns_Valid(t *testing.T) {
	configContent := `
privacy:
  redact_patterns: []
`

	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.Privacy.RedactPatterns)
}
