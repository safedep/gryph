package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/platform/procs"
	"github.com/safedep/gryph/selfprotect"
)

// CensusProviderName is the provider name of a hook_traffic status.
const CensusProviderName = "census"

// Census lists the live agent processes of the account and compares each
// agent with its hook traffic. It returns one hook_traffic status per live
// agent. The status has drift when a process of the agent has run for the
// whole census window and the agent sent no hook call in it: the hooks do
// not reach Gryph, because they were removed, stopped, or never fired.
func (a *Runtime) Census(ctx context.Context) ([]selfprotect.AssetStatus, error) {
	if a.Store == nil {
		return nil, errors.New("engine: the store is not open")
	}
	processes, err := procs.List()
	if err != nil {
		return nil, err
	}
	window := a.Config.Policy.SelfProtection.EffectiveCensusWindow()
	now := time.Now()

	live := map[string][]procs.Process{}
	for _, adapter := range a.Registry.All() {
		namer, ok := adapter.(agent.ProcessNamer)
		if !ok {
			continue
		}
		for _, p := range processes {
			for _, name := range namer.ProcessNames() {
				if p.Matches(name) {
					live[adapter.Name()] = append(live[adapter.Name()], p)
					break
				}
			}
		}
	}

	agents := make([]string, 0, len(live))
	for name := range live {
		agents = append(agents, name)
	}
	sort.Strings(agents)

	var out []selfprotect.AssetStatus
	for _, name := range agents {
		status, err := a.censusStatus(ctx, name, live[name], window, now)
		if err != nil {
			return out, err
		}
		out = append(out, status)
	}
	return out, nil
}

func (a *Runtime) censusStatus(ctx context.Context, name string, processes []procs.Process, window time.Duration, now time.Time) (selfprotect.AssetStatus, error) {
	status := selfprotect.AssetStatus{
		Asset:    selfprotect.AssetHookTraffic,
		Agent:    name,
		Level:    selfprotect.LevelDetect,
		Provider: CensusProviderName,
		Detail:   fmt.Sprintf("%d process(es), pid %s%s", len(processes), pidList(processes), launcherDetail(processes)),
	}

	rows, err := a.Store.QueryEvents(ctx, events.NewEventFilter().WithAgents(name).WithLimit(1))
	if err != nil {
		return status, fmt.Errorf("census: read the events of %s: %w", name, err)
	}
	if len(rows) > 0 && now.Sub(rows[0].Timestamp) <= window {
		status.Detail += ", last hook event " + now.Sub(rows[0].Timestamp).Round(time.Second).String() + " ago"
		return status, nil
	}

	// A process that started inside the window has had no time to send a
	// hook call. Only a process that ran through the whole window counts.
	if !anyOlderThan(processes, now.Add(-window)) {
		status.Detail += ", started inside the census window, no hook event yet"
		return status, nil
	}
	status.Drift = fmt.Sprintf("silent agent: no hook event for %s while %d process(es) run", window, len(processes))
	return status, nil
}

// RunEnv is the variable gryph run sets in the environment of the agent.
// The census reads it from the process, as a signal that the agent runs
// under the Landlock ruleset of the launcher. It is a signal: any process
// can set a variable.
const RunEnv = "GRYPH_RUN"

// launcherDetail says how many of the processes started through gryph
// run, on a platform that shows the environment of a process.
func launcherDetail(processes []procs.Process) string {
	under := 0
	for _, p := range processes {
		ok, err := procs.HasEnv(p.PID, RunEnv)
		if errors.Is(err, procs.ErrUnsupported) {
			return ""
		}
		if ok {
			under++
		}
	}
	switch {
	case under == len(processes):
		return ", under gryph run"
	case under == 0:
		return ", not under gryph run"
	}
	return fmt.Sprintf(", %d of %d under gryph run", under, len(processes))
}

// anyOlderThan reports whether a process started before t. A process with
// an unknown start time counts as old.
func anyOlderThan(processes []procs.Process, t time.Time) bool {
	for _, p := range processes {
		if p.Started.IsZero() || p.Started.Before(t) {
			return true
		}
	}
	return false
}

func pidList(processes []procs.Process) string {
	pids := make([]string, 0, len(processes))
	for _, p := range processes {
		pids = append(pids, strconv.Itoa(p.PID))
	}
	return strings.Join(pids, " ")
}
