package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedInstall_Cursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"version":1,"hooks":{"beforeShellExecution":[{"command":"/enterprise/audit.sh","timeout":5}]}}`), 0o644))
	opts := agent.ManagedInstallOptions{Command: "/opt/safedep/gryph/bin/gryph", Lock: true}

	res, err := installManagedAt(path, opts)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.False(t, res.Locked, "Cursor has no lock")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc struct {
		Version int                         `json:"version"`
		Hooks   map[string][]map[string]any `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, 1, doc.Version)
	shell := doc.Hooks["beforeShellExecution"]
	require.Len(t, shell, 2)
	assert.Equal(t, "/enterprise/audit.sh", shell[0]["command"], "the enterprise entry stays first")
	assert.Equal(t, "/opt/safedep/gryph/bin/gryph _hook cursor beforeShellExecution", shell[1]["command"])
	assert.Equal(t, true, shell[1]["failClosed"], "every Gryph entry fails closed")
	assert.Len(t, doc.Hooks, len(HookTypes))
	for _, hookType := range HookTypes {
		assert.NotEmpty(t, doc.Hooks[hookType], hookType)
	}

	res, err = installManagedAt(path, opts)
	require.NoError(t, err)
	assert.False(t, res.Changed, "a repeated run changes nothing")

	res, err = uninstallManagedAt(path, agent.ManagedInstallOptions{})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	doc.Hooks = nil
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Len(t, doc.Hooks, 1, "only the enterprise event stays")
	assert.Equal(t, "/enterprise/audit.sh", doc.Hooks["beforeShellExecution"][0]["command"])

	_, err = installManagedAt(path, agent.ManagedInstallOptions{Command: "gryph"})
	assert.Error(t, err)
}
