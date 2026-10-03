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

func tempUnitsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	restore := unitsDir
	unitsDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { unitsDir = restore })
	return dir
}

var testJob = Job{
	Name:        "gryph-reconcile",
	Description: "Gryph reconcile",
	Command:     []string{"/opt/safe dep/gryph", "supervisor", "reconcile", "--once"},
	Interval:    15 * time.Minute,
}

func TestInstall_Linux(t *testing.T) {
	dir := tempUnitsDir(t)
	calls := stubRunner(t)

	res, err := Install(context.Background(), testJob)
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	assert.Equal(t, []string{filepath.Join(dir, "gryph-reconcile.service"), filepath.Join(dir, "gryph-reconcile.timer")}, res.Paths)

	service, err := os.ReadFile(filepath.Join(dir, "gryph-reconcile.service"))
	require.NoError(t, err)
	assert.Contains(t, string(service), `ExecStart="/opt/safe dep/gryph" supervisor reconcile --once`)
	assert.Contains(t, string(service), "Type=oneshot")
	timer, err := os.ReadFile(filepath.Join(dir, "gryph-reconcile.timer"))
	require.NoError(t, err)
	assert.Contains(t, string(timer), "OnUnitActiveSec=15min")
	assert.Contains(t, string(timer), "WantedBy=timers.target")

	require.Len(t, *calls, 2)
	assert.Equal(t, []string{"--user", "daemon-reload"}, (*calls)[0].args)
	assert.Equal(t, []string{"--user", "enable", "--now", "gryph-reconcile.timer"}, (*calls)[1].args)
}

func TestInstall_Linux_NoSystemd(t *testing.T) {
	dir := tempUnitsDir(t)
	stubRunner(t, "systemctl")

	res, err := Install(context.Background(), testJob)
	require.NoError(t, err, "a scheduler that refuses does not fail the install")
	assert.False(t, res.Enabled)
	assert.Equal(t, "systemctl --user enable --now gryph-reconcile.timer", res.Next)
	_, err = os.Stat(filepath.Join(dir, "gryph-reconcile.timer"))
	assert.NoError(t, err, "the files stay for the user to enable")
}

func TestRemove_Linux(t *testing.T) {
	dir := tempUnitsDir(t)
	calls := stubRunner(t)
	_, err := Install(context.Background(), testJob)
	require.NoError(t, err)

	res, err := Remove(context.Background(), "gryph-reconcile")
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	assert.Len(t, res.Paths, 2)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.Equal(t, []string{"--user", "disable", "--now", "gryph-reconcile.timer"}, (*calls)[2].args)

	res, err = Remove(context.Background(), "gryph-reconcile")
	require.NoError(t, err, "a second remove is not an error")
	assert.Empty(t, res.Paths)
}
