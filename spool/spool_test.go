package spool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/decision/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrite(t *testing.T) {
	root := t.TempDir()
	path, err := Write(root, "1000", Entry{Verdict: "allow", Reason: "unreachable", Frame: ipc.MustFrame(ipc.TypeHandle, ipc.Handle{Agent: "a", HookType: "x", RawPayload: []byte("{}")})})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "1000"), filepath.Dir(path))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var e Entry
	require.NoError(t, json.Unmarshal(data, &e))
	assert.Equal(t, "allow", e.Verdict)
	assert.Equal(t, ipc.TypeHandle, e.Frame.Type)
	assert.False(t, e.RecordedAt.IsZero())

	_, err = Write(filepath.Join(root, "missing"), "1000", Entry{Reason: "r"})
	assert.Error(t, err, "a missing root is not created")
	_, err = Write("", "1000", Entry{Reason: "r"})
	assert.Error(t, err)
}
