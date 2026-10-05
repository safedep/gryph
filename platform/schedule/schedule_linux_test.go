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

func tempSystemUnitsDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "etc", "systemd", "user")
	restore := systemUnitsDir
	systemUnitsDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { systemUnitsDir = restore })
	return dir
}

func TestInstallRemoveSystemWide_Linux(t *testing.T) {
	dir := tempSystemUnitsDir(t)
	calls := stubRunner(t)

	res, err := InstallSystemWide(context.Background(), testJob)
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	assert.Equal(t, []string{filepath.Join(dir, "gryph-reconcile.service"), filepath.Join(dir, "gryph-reconcile.timer")}, res.Paths)
	service, err := os.ReadFile(filepath.Join(dir, "gryph-reconcile.service"))
	require.NoError(t, err)
	assert.Contains(t, string(service), `ExecStart="/opt/safe dep/gryph" supervisor reconcile --once`)
	require.Len(t, *calls, 1)
	assert.Equal(t, []string{"--global", "enable", "gryph-reconcile.timer"}, (*calls)[0].args, "no daemon-reload: the user managers reload on their own")
	assert.True(t, res.Changed)

	res, err = InstallSystemWide(context.Background(), testJob)
	require.NoError(t, err)
	assert.False(t, res.Changed, "a repeated install changes nothing")
	assert.Len(t, res.Paths, 2, "the files are still the job's")

	// The link that systemctl --global enable would make.
	wants := filepath.Join(dir, "timers.target.wants")
	require.NoError(t, os.MkdirAll(wants, 0o755))
	require.NoError(t, os.Symlink(filepath.Join(dir, "gryph-reconcile.timer"), filepath.Join(wants, "gryph-reconcile.timer")))

	res, err = RemoveSystemWide(context.Background(), "gryph-reconcile")
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	assert.Len(t, res.Paths, 3, "the link, the timer and the service")
	assert.Equal(t, []string{"--global", "disable", "gryph-reconcile.timer"}, (*calls)[2].args)
	assert.True(t, res.Changed)
	_, err = os.Stat(filepath.Join(dir, "gryph-reconcile.service"))
	assert.ErrorIs(t, err, os.ErrNotExist)

	res, err = RemoveSystemWide(context.Background(), "gryph-reconcile")
	require.NoError(t, err)
	assert.Empty(t, res.Paths, "a second remove finds nothing")
}

func TestInstallSystemWide_Linux_NoSystemd(t *testing.T) {
	dir := tempSystemUnitsDir(t)
	stubRunner(t, "systemctl")

	res, err := InstallSystemWide(context.Background(), testJob)
	require.NoError(t, err)
	assert.False(t, res.Enabled)
	assert.Equal(t, "systemctl --global enable gryph-reconcile.timer", res.Next)
	_, err = os.Stat(filepath.Join(dir, "gryph-reconcile.timer"))
	assert.NoError(t, err, "the files stay for the administrator to enable")
}
