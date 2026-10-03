package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type call struct {
	name string
	args []string
}

// stubRunner records scheduler commands and fails the ones in fail.
func stubRunner(t *testing.T, fail ...string) *[]call {
	t.Helper()
	var calls []call
	restore := runCommand
	runCommand = func(_ context.Context, name string, args ...string) error {
		calls = append(calls, call{name: name, args: args})
		for _, f := range fail {
			if f == name+" "+args[0] || f == name {
				return errors.New("no scheduler")
			}
		}
		return nil
	}
	t.Cleanup(func() { runCommand = restore })
	return &calls
}

func TestJob_Validate(t *testing.T) {
	good := Job{Name: "gryph-reconcile", Description: "d", Command: []string{"/opt/gryph", "supervisor", "reconcile", "--once"}, Interval: 15 * time.Minute}
	assert.NoError(t, good.validate())
	assert.Equal(t, 15, good.minutes())

	cases := map[string]func(*Job){
		"empty name":       func(j *Job) { j.Name = "" },
		"name with slash":  func(j *Job) { j.Name = "a/b" },
		"name with space":  func(j *Job) { j.Name = "a b" },
		"relative program": func(j *Job) { j.Command = []string{"gryph"} },
		"no command":       func(j *Job) { j.Command = nil },
		"zero interval":    func(j *Job) { j.Interval = 0 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			j := good
			change(&j)
			assert.Error(t, j.validate())
		})
	}
	assert.Equal(t, 1, Job{Interval: 10 * time.Second}.minutes(), "at least one minute")
}

func TestRemove_RejectsBadName(t *testing.T) {
	_, err := Remove(context.Background(), "a/b")
	require.Error(t, err)
}
