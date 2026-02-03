package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		setup  func(env *testEnv)
		assert func(t *testing.T, stdout string, err error)
	}{
		{
			name: "show_defaults",
			args: []string{"config", "show"},
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "logging")
			},
		},
		{
			name: "show_json",
			args: []string{"config", "show", "--format", "json"},
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				var result json.RawMessage
				assert.NoError(t, json.Unmarshal([]byte(stdout), &result))
			},
		},
		{
			name: "get_logging_level",
			args: []string{"config", "get", "logging.level"},
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "full")
			},
		},
		{
			name: "get_nonexistent_key",
			args: []string{"config", "get", "nonexistent.key"},
			assert: func(t *testing.T, stdout string, err error) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "key not found")
			},
		},
		{
			name: "set_value",
			args: []string{"config", "set", "logging.level", "minimal"},
			assert: func(t *testing.T, stdout string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "Set logging.level")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			stdout, _, err := env.run(tt.args...)
			tt.assert(t, stdout, err)
		})
	}
}

func TestConfig_SetAndVerify(t *testing.T) {
	env := newTestEnv(t)

	_, _, err := env.run("config", "set", "logging.level", "minimal")
	require.NoError(t, err)

	stdout, _, err := env.run("config", "get", "logging.level")
	require.NoError(t, err)
	assert.Contains(t, stdout, "minimal")
}

func TestConfig_Reset(t *testing.T) {
	env := newTestEnv(t)

	// Set a non-default value
	_, _, err := env.run("config", "set", "logging.level", "minimal")
	require.NoError(t, err)

	// Reset
	stdout, _, err := env.run("config", "reset")
	require.NoError(t, err)
	assert.Contains(t, stdout, "reset to defaults")

	// Verify the default is restored
	stdout, _, err = env.run("config", "get", "logging.level")
	require.NoError(t, err)
	// After reset, should be the standard default
	assert.Contains(t, stdout, "standard")
}

func TestConfig_SetBoolean(t *testing.T) {
	env := newTestEnv(t)

	_, _, err := env.run("config", "set", "logging.content_hash", "false")
	require.NoError(t, err)

	stdout, _, err := env.run("config", "get", "logging.content_hash")
	require.NoError(t, err)
	assert.Contains(t, stdout, "false")
}
