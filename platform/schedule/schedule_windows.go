package schedule

import (
	"context"
	"strconv"
	"strings"
)

const taskFolder = `SafeDep\`

func install(ctx context.Context, job Job) (*Result, error) {
	task := taskFolder + job.Name
	res := &Result{Next: "schtasks /Create /F /SC MINUTE /MO " + strconv.Itoa(job.minutes()) + " /TN " + task + " /TR " + taskCommand(job.Command)}
	if err := runCommand(ctx, "schtasks", "/Create", "/F", "/SC", "MINUTE", "/MO", strconv.Itoa(job.minutes()),
		"/TN", task, "/TR", taskCommand(job.Command)); err != nil {
		return res, nil
	}
	res.Enabled = true
	return res, nil
}

func remove(ctx context.Context, name string) (*Result, error) {
	task := taskFolder + name
	res := &Result{Enabled: true}
	if err := runCommand(ctx, "schtasks", "/Delete", "/F", "/TN", task); err != nil {
		// A task that does not exist is not an error for a remove.
		if !strings.Contains(err.Error(), "does not exist") && !strings.Contains(err.Error(), "cannot find") {
			res.Enabled = false
			res.Next = "schtasks /Delete /F /TN " + task
		}
	}
	return res, nil
}

// taskCommand renders the /TR value: the program in double quotes, then
// the arguments.
func taskCommand(command []string) string {
	parts := []string{`"` + command[0] + `"`}
	for _, arg := range command[1:] {
		if strings.ContainsAny(arg, " \t") {
			arg = `"` + arg + `"`
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}
