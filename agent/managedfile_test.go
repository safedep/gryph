package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateManagedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "hooks.json")
	set := func(doc map[string]any) error { doc["a"] = 1; return nil }

	res, err := UpdateManagedJSON(path, ManagedInstallOptions{}, func(map[string]any) error { return nil })
	require.NoError(t, err)
	assert.False(t, res.Changed, "a no-op edit of a missing file writes nothing")
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)

	res, err = UpdateManagedJSON(path, ManagedInstallOptions{DryRun: true}, set)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "a dry run writes nothing")

	res, err = UpdateManagedJSON(path, ManagedInstallOptions{}, set)
	require.NoError(t, err)
	assert.True(t, res.Changed)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"a\": 1\n}\n", string(data))

	res, err = UpdateManagedJSON(path, ManagedInstallOptions{}, set)
	require.NoError(t, err)
	assert.False(t, res.Changed, "an unchanged document is not written")

	require.NoError(t, os.WriteFile(path, []byte("{bad"), 0o644))
	_, err = UpdateManagedJSON(path, ManagedInstallOptions{}, set)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")

	res, err = RemoveManagedFile(path, ManagedInstallOptions{})
	require.NoError(t, err)
	assert.True(t, res.Changed)
	res, err = RemoveManagedFile(path, ManagedInstallOptions{})
	require.NoError(t, err)
	assert.False(t, res.Changed)
}

func TestReplaceGryphEntries(t *testing.T) {
	hooks := map[string]any{
		"pre": []any{
			map[string]any{"command": "/other/tool"},
			map[string]any{"command": "/old/gryph _hook x pre"},
		},
		"post": []any{map[string]any{"command": "/old/gryph _hook x post"}},
	}
	entry := func(hookType string) map[string]any {
		return map[string]any{"command": "/new/gryph _hook x " + hookType}
	}
	ReplaceGryphEntries(hooks, []string{"pre", "post", "other"}, IsGryphCommandEntry, entry, false)
	pre := EntryList(hooks["pre"])
	require.Len(t, pre, 2)
	assert.Equal(t, "/other/tool", pre[0]["command"], "the other program's entry stays first")
	assert.Equal(t, "/new/gryph _hook x pre", pre[1]["command"])
	assert.Len(t, EntryList(hooks["post"]), 1)
	assert.Len(t, EntryList(hooks["other"]), 1)

	ReplaceGryphEntries(hooks, []string{"pre", "post", "other"}, IsGryphCommandEntry, nil, true)
	assert.Len(t, EntryList(hooks["pre"]), 1)
	_, hasPost := hooks["post"]
	assert.False(t, hasPost, "an event with Gryph entries only goes away")
	_, hasOther := hooks["other"]
	assert.False(t, hasOther)

	assert.True(t, IsGryphMatcherEntry(map[string]any{"hooks": []any{map[string]any{"command": "gryph _hook a b"}}}))
	assert.False(t, IsGryphMatcherEntry(map[string]any{"hooks": []any{map[string]any{"command": "gryph _hook a b"}, map[string]any{"command": "/x"}}}))
	assert.False(t, IsGryphMatcherEntry(map[string]any{"matcher": "*"}))
}
