package utils

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// programTimeout bounds one version probe of an agent binary.
const programTimeout = 5 * time.Second

type noProgramExecutionKey struct{}

// WithoutProgramExecution returns a context under which detection runs no
// program. An install that writes system files uses it, because a program
// found through PATH can belong to any user.
func WithoutProgramExecution(ctx context.Context) context.Context {
	return context.WithValue(ctx, noProgramExecutionKey{}, true)
}

// privilegedProcess is overridable in tests.
var privilegedProcess = isPrivileged

// programExecutionAllowed reports whether detection may run a program. A
// privileged process never runs one: the program comes from PATH, and a
// user can put a binary there.
func programExecutionAllowed(ctx context.Context) bool {
	if v, ok := ctx.Value(noProgramExecutionKey{}).(bool); ok && v {
		return false
	}
	return !privilegedProcess()
}

// ProgramVersion runs program with args and returns the version token of
// its output. It returns "" when ctx forbids program execution, when the
// process is privileged, when the program is missing or fails, or when the
// output holds no version.
func ProgramVersion(ctx context.Context, program string, args ...string) string {
	if !programExecutionAllowed(ctx) {
		return ""
	}
	cmdCtx, cancel := context.WithTimeout(ctx, programTimeout)
	defer cancel()
	output, err := exec.CommandContext(cmdCtx, program, args...).Output()
	if err != nil {
		return ""
	}
	return VersionToken(string(output))
}

// VersionToken returns the first field of s that starts with a digit, for
// outputs such as "2.1.15 (Claude Code)" or "codex-cli 0.5.0". When no
// field starts with a digit it returns the first field, and "" for an empty
// string.
func VersionToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	for _, f := range fields {
		if f[0] >= '0' && f[0] <= '9' {
			return f
		}
	}
	return fields[0]
}
