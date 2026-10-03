package engine

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strconv"
	"time"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/utils"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/platform/kernel"
)

// PostureStatus says how a posture item reads.
type PostureStatus string

const (
	// PostureOK is a setting that helps Gryph resist a same-user adversary.
	PostureOK PostureStatus = "ok"
	// PostureWarn is a setting that leaves a known bypass open.
	PostureWarn PostureStatus = "warn"
	// PostureInfo is a fact with no better value to set.
	PostureInfo PostureStatus = "info"
)

// PostureItem is one fact about the host that decides how far Gryph
// resists a same-user adversary.
type PostureItem struct {
	Name   string
	Value  string
	Status PostureStatus
	// Note says what the value means and, for a warning, what to set.
	Note string
}

// Posture reports the host settings and the agent behavior that decide how
// far a same-user adversary gets: the user namespace limit, the ptrace
// scope, and what each present agent does when a hook fails.
func (a *Runtime) Posture(ctx context.Context) []PostureItem {
	items := kernelPosture()
	adapters := a.Registry.All()
	sort.Slice(adapters, func(i, j int) bool { return adapters[i].Name() < adapters[j].Name() })
	for _, adapter := range adapters {
		detection, err := adapter.Detect(utils.WithoutProgramExecution(ctx))
		if err != nil || detection == nil || !detection.Installed {
			continue
		}
		items = append(items, agentPosture(ctx, adapter)...)
	}
	return items
}

// kernelPosture reads the Linux settings. Another platform has none.
func kernelPosture() []PostureItem {
	var items []PostureItem
	if item, ok := usernsPosture(); ok {
		items = append(items, item)
	}
	value, err := kernel.Setting("kernel.yama.ptrace_scope")
	switch {
	case errors.Is(err, kernel.ErrUnsupported):
	case errors.Is(err, fs.ErrNotExist):
		items = append(items, PostureItem{Name: "kernel.yama.ptrace_scope", Value: "absent", Status: PostureWarn,
			Note: "the kernel has no Yama, so a process of the user can trace the hook and change its answer"})
	case err != nil:
		items = append(items, PostureItem{Name: "kernel.yama.ptrace_scope", Value: "unreadable", Status: PostureInfo, Note: err.Error()})
	case value == "0":
		items = append(items, PostureItem{Name: "kernel.yama.ptrace_scope", Value: value, Status: PostureWarn,
			Note: "a process of the user can trace the hook and change its answer. Set kernel.yama.ptrace_scope=1"})
	default:
		items = append(items, PostureItem{Name: "kernel.yama.ptrace_scope", Value: value, Status: PostureOK,
			Note: "only a parent can trace the hook"})
	}
	return items
}

// usernsPosture reads the two settings that stop an unprivileged user
// namespace, in which a user can bind-mount over the managed settings.
func usernsPosture() (PostureItem, bool) {
	if value, err := kernel.Setting("kernel.apparmor_restrict_unprivileged_userns"); err == nil && value == "1" {
		return PostureItem{Name: "kernel.apparmor_restrict_unprivileged_userns", Value: value, Status: PostureOK,
			Note: "an unprivileged user cannot make a user namespace"}, true
	} else if errors.Is(err, kernel.ErrUnsupported) {
		return PostureItem{}, false
	}
	value, err := kernel.Setting("user.max_user_namespaces")
	switch {
	case errors.Is(err, kernel.ErrUnsupported), errors.Is(err, fs.ErrNotExist):
		return PostureItem{}, false
	case err != nil:
		return PostureItem{Name: "user.max_user_namespaces", Value: "unreadable", Status: PostureInfo, Note: err.Error()}, true
	case value == "0":
		return PostureItem{Name: "user.max_user_namespaces", Value: value, Status: PostureOK,
			Note: "an unprivileged user cannot make a user namespace"}, true
	}
	return PostureItem{Name: "user.max_user_namespaces", Value: value, Status: PostureWarn,
		Note: "an unprivileged user can make a user namespace and bind-mount over the managed settings. Set kernel.apparmor_restrict_unprivileged_userns=1 or user.max_user_namespaces=0"}, true
}

// agentPosture reports what the agent does when a hook fails, from the
// declared hook timeouts and, where the agent has one, its fail-closed
// setting.
func agentPosture(ctx context.Context, adapter agent.Adapter) []PostureItem {
	var items []PostureItem
	if reporter, ok := adapter.(agent.FailModeReporter); ok {
		closed, err := reporter.FailClosed(ctx)
		switch {
		case err != nil:
			items = append(items, PostureItem{Name: adapter.Name() + " failClosed", Value: "unknown", Status: PostureInfo, Note: err.Error()})
		case closed:
			items = append(items, PostureItem{Name: adapter.Name() + " failClosed", Value: "set", Status: PostureOK,
				Note: "the agent blocks the action when the hook fails"})
		default:
			items = append(items, PostureItem{Name: adapter.Name() + " failClosed", Value: "not set", Status: PostureWarn,
				Note: "the agent lets the action through when the hook fails. Gryph does not set failClosed in the user scope"})
		}
	}
	items = append(items, PostureItem{
		Name:   adapter.Name() + " on a hook error or timeout",
		Value:  "lets the action through",
		Status: PostureInfo,
		Note:   timeoutNote(adapter.Hooks()),
	})
	return items
}

// timeoutNote names the longest hook timeout of the agent. A block exists
// only while Gryph answers before it.
func timeoutNote(specs []events.HookSpec) string {
	var longest time.Duration
	for _, s := range specs {
		if s.Timeout > longest {
			longest = s.Timeout
		}
	}
	if longest == 0 {
		return "hook timeout not documented by the vendor"
	}
	return "hook timeout " + strconv.Itoa(int(longest/time.Second)) + " s"
}
