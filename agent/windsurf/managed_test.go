package windsurf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedInstall_Windsurf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devin", "hooks.json")
	opts := agent.ManagedInstallOptions{Command: "/opt/safedep/gryph/bin/gryph"}

	res, err := installManagedAt(path, opts)
	require.NoError(t, err)
	assert.True(t, res.Changed)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	hooks := doc["hooks"].(map[string]any)
	assert.Len(t, hooks, len(HookTypes))
	assert.Equal(t, "/opt/safedep/gryph/bin/gryph _hook windsurf pre_run_command", agent.EntryList(hooks["pre_run_command"])[0]["command"])

	res, err = installManagedAt(path, opts)
	require.NoError(t, err)
	assert.False(t, res.Changed)

	res, err = uninstallManagedAt(path, agent.ManagedInstallOptions{})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	doc = map[string]any{}
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Empty(t, doc["hooks"])
}
