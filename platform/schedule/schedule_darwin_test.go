package schedule

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallRemove_Darwin(t *testing.T) {
	dir := t.TempDir()
	restore := agentsDir
	agentsDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { agentsDir = restore })
	calls := stubRunner(t)

	job := Job{Name: "gryph-reconcile", Description: "Gryph reconcile", Command: []string{"/opt/safe dep/gryph", "supervisor", "reconcile", "--once"}, Interval: 15 * time.Minute}
	res, err := Install(context.Background(), job)
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	plist := filepath.Join(dir, "io.safedep.gryph-reconcile.plist")
	assert.Equal(t, []string{plist}, res.Paths)
	data, err := os.ReadFile(plist)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<string>io.safedep.gryph-reconcile</string>")
	assert.Contains(t, string(data), "<string>/opt/safe dep/gryph</string>")
	assert.Contains(t, string(data), "<integer>900</integer>")
	require.Len(t, *calls, 2)
	assert.Equal(t, "bootstrap", (*calls)[1].args[0])

	res, err = Remove(context.Background(), "gryph-reconcile")
	require.NoError(t, err)
	assert.Equal(t, []string{plist}, res.Paths)
	_, err = os.Stat(plist)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestInstallRemoveSystemWide_Darwin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Library", "LaunchAgents")
	restore := systemAgentsDir
	systemAgentsDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { systemAgentsDir = restore })
	calls := stubRunner(t)

	job := Job{Name: "gryph-reconcile", Description: "Gryph reconcile", Command: []string{"/Library/SafeDep/gryph/bin/gryph", "supervisor", "reconcile", "--once"}, Interval: 15 * time.Minute}
	res, err := InstallSystemWide(context.Background(), job)
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	path := filepath.Join(dir, "io.safedep.gryph-reconcile.plist")
	assert.Equal(t, []string{path}, res.Paths)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<string>/Library/SafeDep/gryph/bin/gryph</string>")
	assert.Contains(t, string(data), "<integer>900</integer>")
	assert.Empty(t, *calls, "launchd loads the agent at login, no command runs")

	res, err = RemoveSystemWide(context.Background(), "gryph-reconcile")
	require.NoError(t, err)
	assert.Equal(t, []string{path}, res.Paths)
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}
