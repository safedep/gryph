package config

import (
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
	assert.Equal(t, "minimal", mgr.Get("logging.level"))
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
		{"logging.level", "minimal"},
		{"logging.stdout_max_chars", 1000},
		{"logging.stderr_max_chars", 500},
		{"logging.context_max_chars", 5000},
		{"storage.retention_days", 90},
		{"privacy.hash_file_contents", true},
		{"filters.enabled", false},
		{"agents.claude-code.enabled", true},
		{"agents.cursor.enabled", true},
		{"display.colors", "auto"},
		{"display.timezone", "local"},
		{"streams.targets", []StreamTargetConfig{
			{Name: "stdout", Type: "stdout", Enabled: true},
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

	assert.Equal(t, "minimal", mgr.Get("logging.level"))
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
	assert.Equal(t, "minimal", logging["level"])
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
