package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewManager_NoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)
	require.NotNil(t, mgr)

	assert.Equal(t, configFile, mgr.ConfigPath())
	assert.NotNil(t, mgr.AllSettings())
	assert.Equal(t, "standard", mgr.Get("logging.level"))
}

func TestNewManager_WithExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
logging:
  level: full
storage:
  retention_days: 30
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	mgr, err := NewManager(configFile)
	require.NoError(t, err)
	require.NotNil(t, mgr)

	assert.Equal(t, "full", mgr.Get("logging.level"))
	assert.Equal(t, 30, mgr.Get("storage.retention_days"))
}

func TestManager_Get_ReturnsDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	tests := []struct {
		key      string
		expected interface{}
	}{
		{"logging.level", "standard"},
		{"logging.stdout_max_chars", 1000},
		{"logging.stderr_max_chars", 500},
		{"logging.context_max_chars", 5000},
		{"logging.content_hash", true},
		{"storage.retention_days", 90},
		{"filters.enabled", false},
		{"agents.claude-code.enabled", true},
		{"agents.cursor.enabled", true},
		{"display.colors", "auto"},
		{"display.timezone", "local"},
		{"streams.targets", []StreamTargetConfig{
			{Name: "nop", Type: "nop", Enabled: true},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			assert.Equal(t, tt.expected, mgr.Get(tt.key))
		})
	}
}

func TestManager_Set_CreatesCompleteConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	err = mgr.Set("logging.level", "full")
	require.NoError(t, err)

	data, err := os.ReadFile(configFile)
	require.NoError(t, err)

	var configMap map[string]interface{}
	err = yaml.Unmarshal(data, &configMap)
	require.NoError(t, err)

	assert.Contains(t, configMap, "logging")
	assert.Contains(t, configMap, "storage")
	assert.Contains(t, configMap, "privacy")
	assert.Contains(t, configMap, "filters")
	assert.Contains(t, configMap, "agents")
	assert.Contains(t, configMap, "display")
	assert.Contains(t, configMap, "streams")

	logging, ok := configMap["logging"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "full", logging["level"])
	assert.Equal(t, 1000, logging["stdout_max_chars"])
}

func TestManager_Set_PreservesExistingValues(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
logging:
  level: standard
  stdout_max_chars: 2000
storage:
  retention_days: 60
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	err = mgr.Set("display.colors", "always")
	require.NoError(t, err)

	assert.Equal(t, "standard", mgr.Get("logging.level"))
	assert.Equal(t, 2000, mgr.Get("logging.stdout_max_chars"))
	assert.Equal(t, 60, mgr.Get("storage.retention_days"))
	assert.Equal(t, "always", mgr.Get("display.colors"))
}

func TestManager_Set_UpdatesExistingValue(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
logging:
  level: minimal
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	assert.Equal(t, "minimal", mgr.Get("logging.level"))

	err = mgr.Set("logging.level", "full")
	require.NoError(t, err)

	assert.Equal(t, "full", mgr.Get("logging.level"))

	newMgr, err := NewManager(configFile)
	require.NoError(t, err)
	assert.Equal(t, "full", newMgr.Get("logging.level"))
}

func TestManager_Reset_RemovesConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
logging:
  level: full
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	assert.Equal(t, "full", mgr.Get("logging.level"))

	err = mgr.Reset()
	require.NoError(t, err)

	_, err = os.Stat(configFile)
	assert.True(t, os.IsNotExist(err))

	assert.Equal(t, "standard", mgr.Get("logging.level"))
}

func TestManager_Reset_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "nonexistent.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	err = mgr.Reset()
	require.NoError(t, err)
}

func TestManager_AllSettings_IncludesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	settings := mgr.AllSettings()

	assert.Contains(t, settings, "logging")
	assert.Contains(t, settings, "storage")
	assert.Contains(t, settings, "privacy")
	assert.Contains(t, settings, "filters")
	assert.Contains(t, settings, "agents")
	assert.Contains(t, settings, "display")
	assert.Contains(t, settings, "streams")

	logging, ok := settings["logging"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "standard", logging["level"])
	assert.Equal(t, 1000, logging["stdout_max_chars"])
}

func TestManager_HasKey(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	assert.True(t, mgr.HasKey("logging.level"))
	assert.True(t, mgr.HasKey("storage.retention_days"))
	assert.False(t, mgr.HasKey("nonexistent.key"))
}

func TestKnownKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"default scalar key", "logging.level", true},
		{"default nested key", "policy.defer.timeout_seconds", true},
		{"default list key", "privacy.sensitive_paths", true},
		{"default agent toggle", "agents.claude-code.enabled", true},
		{"optional per-agent logging level", "agents.claude-code.logging_level", true},
		{"arbitrary agent name override", "agents.some-agent.logging_level", true},
		{"deprecated receipts sign alias", "policy.receipts.sign", true},
		{"agent extra path segment", "agents.foo.bar.enabled", false},
		{"unknown top level key", "hello.world", false},
		{"unknown deep key", "x.y.z", false},
		{"typo in known section", "loggin.level", false},
		{"agents leaf missing", "agents", false},
		{"agents unknown leaf", "agents.claude-code.noise", false},
		{"agents empty name", "agents..enabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, knownKey(tt.key))
		})
	}
}

func TestManager_Set_RejectsUnknownKey(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	err = mgr.Set("x.y.z", true)
	assert.ErrorIs(t, err, ErrUnknownKey)

	_, statErr := os.Stat(configFile)
	assert.True(t, os.IsNotExist(statErr))
}

func TestParseValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected interface{}
	}{
		{"boolean true", "true", true},
		{"boolean false", "false", false},
		{"string value", "hello", "hello"},
		{"numeric string", "42", "42"},
		{"simple array", "[a, b, c]", []string{"a", "b", "c"}},
		{"array with spaces", "[foo, bar, baz]", []string{"foo", "bar", "baz"}},
		{"empty array", "[]", []string{""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestManager_Set_CreatesConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "config", "dir")
	configFile := filepath.Join(nestedDir, "config.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	err = mgr.Set("logging.level", "full")
	require.NoError(t, err)

	_, err = os.Stat(configFile)
	require.NoError(t, err)
}

func TestManager_Set_MultipleValues(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	err = mgr.Set("logging.level", "full")
	require.NoError(t, err)

	err = mgr.Set("storage.retention_days", 30)
	require.NoError(t, err)

	err = mgr.Set("display.colors", "always")
	require.NoError(t, err)

	newMgr, err := NewManager(configFile)
	require.NoError(t, err)

	assert.Equal(t, "full", newMgr.Get("logging.level"))
	assert.Equal(t, 30, newMgr.Get("storage.retention_days"))
	assert.Equal(t, "always", newMgr.Get("display.colors"))
}

func TestManagerSet_RemovesStaleLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, legacyConfigFileName)
	require.NoError(t, os.WriteFile(stale, []byte("logging:\n  level: full\n"), 0o600))

	mgr, err := NewManager(filepath.Join(dir, configFileName))
	require.NoError(t, err)
	require.NoError(t, mgr.Set("logging.level", "minimal"))

	assert.FileExists(t, filepath.Join(dir, configFileName))
	assert.NoFileExists(t, stale)
}

func TestManager_Set_RejectsInvalidValue(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yml")
	mgr, err := NewManager(configFile)
	require.NoError(t, err)
	require.NoError(t, mgr.Set("policy.enabled", true))

	err = mgr.Set("policy.context.cel_entries", 5001)
	require.ErrorIs(t, err, ErrInvalidValue)
	assert.Equal(t, 100, mgr.Get("policy.context.cel_entries"), "the old value stays")

	data, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "5001")
	require.NoError(t, mgr.Set("policy.context.cel_entries", 200))
}

func TestManager_Set_ClampsOtherContextKeys(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yml")
	content := "policy:\n  enabled: false\n  context:\n    window_max_entries: 0\n    window_max_bytes: -1\n"
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0o600))

	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	require.NoError(t, mgr.Set("policy.enabled", true), "an out-of-range value in the file must not block another key")
	require.NoError(t, mgr.Set("policy.context.window_max_entries", "50"))
	assert.ErrorIs(t, mgr.Set("policy.context.window_max_entries", "0"), ErrInvalidValue)
}

func TestManager_Set_RejectsOutOfRangeContextKey(t *testing.T) {
	tests := []struct {
		key   string
		value interface{}
		ok    bool
	}{
		{"policy.context.cel_entries", 0, false},
		{"policy.context.cel_entries", MaxCELEntries + 1, false},
		{"policy.context.cel_entries", MaxCELEntries, true},
		{"policy.context.window_max_entries", 0, false},
		{"policy.context.window_max_entries", MaxWindowEntries + 1, false},
		{"policy.context.window_max_entries", 1, true},
		{"policy.context.window_max_bytes", -1, false},
		{"policy.context.window_max_bytes", -0.5, false},
		{"policy.context.window_max_bytes", 0, true},
	}
	for _, tt := range tests {
		for _, enabled := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s=%v/enabled=%v", tt.key, tt.value, enabled), func(t *testing.T) {
				mgr, err := NewManager(filepath.Join(t.TempDir(), "config.yml"))
				require.NoError(t, err)
				require.NoError(t, mgr.Set("policy.enabled", enabled))

				err = mgr.Set(tt.key, tt.value)
				if tt.ok {
					assert.NoError(t, err)
					return
				}
				assert.ErrorIs(t, err, ErrInvalidValue)
			})
		}
	}
}

func TestManager_Set_RejectsInvalidExportProfile(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(configFile, []byte(`
export:
  profiles:
    team:
      default: hide
`), 0o600))
	mgr, err := NewManager(configFile)
	require.NoError(t, err)

	err = mgr.Set("logging.level", "full")
	require.ErrorIs(t, err, ErrInvalidValue)
	assert.Contains(t, err.Error(), `unknown default treatment "hide"`)
}
