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
