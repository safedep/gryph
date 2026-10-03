package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallRemove_Windows(t *testing.T) {
	calls := stubRunner(t)
	job := Job{Name: "gryph-reconcile", Description: "Gryph reconcile", Command: []string{`C:\Program Files\SafeDep\gryph.exe`, "supervisor", "reconcile", "--once"}, Interval: 15 * time.Minute}
	res, err := Install(context.Background(), job)
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	assert.Empty(t, res.Paths)
	require.Len(t, *calls, 1)
	assert.Equal(t, []string{"/Create", "/F", "/SC", "MINUTE", "/MO", "15", "/TN", `SafeDep\gryph-reconcile`, "/TR", `"C:\Program Files\SafeDep\gryph.exe" supervisor reconcile --once`}, (*calls)[0].args)

	res, err = Remove(context.Background(), "gryph-reconcile")
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	assert.Equal(t, []string{"/Delete", "/F", "/TN", `SafeDep\gryph-reconcile`}, (*calls)[1].args)
}

func TestInstallRemoveSystemWide_Windows(t *testing.T) {
	calls := stubRunner(t)
	job := Job{Name: "gryph-reconcile", Description: "Gryph reconcile", Command: []string{`C:\Program Files\SafeDep\gryph.exe`, "supervisor", "reconcile", "--once"}, Interval: 15 * time.Minute}
	res, err := InstallSystemWide(context.Background(), job)
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	require.Len(t, *calls, 1)
	args := (*calls)[0].args
	assert.Equal(t, []string{"/Create", "/F", "/TN", `SafeDep\gryph-reconcile`, "/XML"}, args[:5])

	xml := renderTaskXML(job)
	assert.Contains(t, xml, "<GroupId>S-1-5-32-545</GroupId>")
	assert.Contains(t, xml, "<Interval>PT15M</Interval>")
	assert.Contains(t, xml, `<Command>C:\Program Files\SafeDep\gryph.exe</Command>`)
	assert.Contains(t, xml, "<Arguments>supervisor reconcile --once</Arguments>")
	assert.Contains(t, xml, "<LogonTrigger>")

	res, err = RemoveSystemWide(context.Background(), "gryph-reconcile")
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	assert.Equal(t, []string{"/Delete", "/F", "/TN", `SafeDep\gryph-reconcile`}, (*calls)[1].args)
}
