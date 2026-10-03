package gemini

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedInstall_Gemini(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gemini-cli", "settings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"general":{"vimMode":false},"hooksConfig":{"notifications":true},"hooks":{"BeforeTool":[{"matcher":"shell","hooks":[{"type":"command","command":"/enterprise/check"}]}]}}`), 0o644))
	opts := agent.ManagedInstallOptions{Command: "/opt/safedep/gryph/bin/gryph"}

	res, err := installManagedAt(path, opts)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	assert.False(t, res.Locked)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, false, doc["general"].(map[string]any)["vimMode"], "other settings stay")
	hooksConfig := doc["hooksConfig"].(map[string]any)
	assert.Equal(t, true, hooksConfig["enabled"], "hooks are pinned on in the system scope")
	assert.Equal(t, true, hooksConfig["notifications"])
	before := agent.EntryList(doc["hooks"].(map[string]any)["BeforeTool"])
	require.Len(t, before, 2)
	assert.Equal(t, "shell", before[0]["matcher"], "the enterprise entry stays first")
	assert.Equal(t, "*", before[1]["matcher"])
	assert.Equal(t, "/opt/safedep/gryph/bin/gryph _hook gemini BeforeTool", agent.EntryList(before[1]["hooks"])[0]["command"])
	assert.Len(t, doc["hooks"].(map[string]any), len(HookTypes))

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
	assert.Len(t, doc["hooks"].(map[string]any), 1)
	assert.Equal(t, true, doc["hooksConfig"].(map[string]any)["enabled"], "the pin stays")
}
