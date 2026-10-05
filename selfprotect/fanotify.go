package selfprotect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FanotifyProviderName is the name of the kernel watcher provider.
const FanotifyProviderName = "fanotify"

// FanotifyState is what the watcher reports in its state file: the
// process, the paths it marked, and the changes it noticed. The watcher
// writes it and gryph doctor reads it, so the file is readable by every
// account.
type FanotifyState struct {
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	// Files are the files whose open asks the watcher. Dirs are the
	// directories whose direct children get the same protection.
	Files []string `json:"files"`
	Dirs  []string `json:"dirs"`
	// Denied counts the opens the watcher refused.
	Denied int `json:"denied"`
	// Changes are the last changes the watcher noticed and could not stop,
	// newest last.
	Changes []FanotifyChange `json:"changes"`
}

// FanotifyChange is one change the watcher noticed: a modify or an
// attribute change by an exempt account, or a rename or an unlink.
type FanotifyChange struct {
	Time time.Time `json:"time"`
	Op   string    `json:"op"`
	Path string    `json:"path"`
	PID  int       `json:"pid"`
	UID  uint32    `json:"uid"`
}

// ReadFanotifyState reads the state file of the watcher.
func ReadFanotifyState(path string) (*FanotifyState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state FanotifyState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &state, nil
}

// Covers reports whether the watcher protects the path: a marked file, a
// direct child of a marked directory, or a marked directory.
func (s *FanotifyState) Covers(path string) bool {
	for _, f := range s.Files {
		if f == path {
			return true
		}
	}
	for _, d := range s.Dirs {
		if d == path || filepath.Dir(path) == d {
			return true
		}
	}
	return false
}

// FanotifyAssets names the protected paths per asset, as the managed
// configuration gives them.
type FanotifyAssets struct {
	Binary      string
	ConfigFile  string
	PolicyFile  string
	PoliciesDir string
	// HookConfigs maps an agent with a locked managed entry to its
	// managed hook file.
	HookConfigs map[string]string
}

type fanotifyProvider struct {
	stateFile string
	alive     func(pid int) bool
	assets    FanotifyAssets
}

// NewFanotifyProvider returns the provider that reports the kernel
// watcher. It reads the state file the watcher writes, checks that the
// process lives, and reports PreventSameUser for every asset whose paths
// the watcher covers. A watcher that is gone reports nothing, so the rows
// of the other providers stand.
func NewFanotifyProvider(stateFile string, alive func(pid int) bool, assets FanotifyAssets) Provider {
	return &fanotifyProvider{stateFile: stateFile, alive: alive, assets: assets}
}

func (p *fanotifyProvider) Name() string { return FanotifyProviderName }

// Assess implements Provider. It repairs nothing: the watcher stops a
// change before it happens.
func (p *fanotifyProvider) Assess(context.Context) []AssetStatus {
	state, err := ReadFanotifyState(p.stateFile)
	if err != nil || state.PID <= 0 || !p.alive(state.PID) {
		return nil
	}
	var out []AssetStatus
	agents := make([]string, 0, len(p.assets.HookConfigs))
	for name := range p.assets.HookConfigs {
		agents = append(agents, name)
	}
	sort.Strings(agents)
	for _, name := range agents {
		path := p.assets.HookConfigs[name]
		if state.Covers(path) {
			out = append(out, p.status(state, AssetHookConfig, name, path))
		}
	}
	if p.assets.Binary != "" && state.Covers(p.assets.Binary) {
		out = append(out, p.status(state, AssetBinary, "", p.assets.Binary))
	}
	if p.assets.PolicyFile != "" && state.Covers(p.assets.PolicyFile) && (p.assets.PoliciesDir == "" || state.Covers(p.assets.PoliciesDir)) {
		out = append(out, p.status(state, AssetPolicy, "", p.assets.PolicyFile+", "+p.assets.PoliciesDir))
	}
	if p.assets.ConfigFile != "" && state.Covers(p.assets.ConfigFile) {
		out = append(out, p.status(state, AssetConfig, "", p.assets.ConfigFile))
	}
	return out
}

// Repair implements Provider. The watcher owns no repair.
func (p *fanotifyProvider) Repair(context.Context, RepairOptions) ([]AssetStatus, error) {
	return nil, nil
}

// status builds the row of one asset. A rename or an unlink of a
// protected path, which the kernel lets through, is the drift of the row
// until the watcher restarts, so a pass records it once.
func (p *fanotifyProvider) status(state *FanotifyState, asset Asset, agent, detail string) AssetStatus {
	s := AssetStatus{
		Asset:    asset,
		Agent:    agent,
		Level:    LevelPreventSameUser,
		Provider: FanotifyProviderName,
		Detail:   fmt.Sprintf("watched by fanotify (pid %d): %s", state.PID, detail),
	}
	for i := len(state.Changes) - 1; i >= 0; i-- {
		c := state.Changes[i]
		if (c.Op == "deleted" || c.Op == "moved") && c.Path != "" && coversDetail(detail, c.Path) {
			s.Drift = fmt.Sprintf("%s %s by uid %d at %s", c.Path, c.Op, c.UID, c.Time.UTC().Format(time.RFC3339))
			break
		}
	}
	return s
}

// coversDetail reports whether a changed path is one of the paths the
// detail names, or sits in one of them.
func coversDetail(detail, path string) bool {
	for _, d := range splitDetail(detail) {
		if d == path || filepath.Dir(path) == d {
			return true
		}
	}
	return false
}

func splitDetail(detail string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(detail); i++ {
		if i == len(detail) || (detail[i] == ',' && i+1 < len(detail) && detail[i+1] == ' ') {
			if part := detail[start:i]; part != "" {
				out = append(out, part)
			}
			if i < len(detail) {
				i++
			}
			start = i + 1
		}
	}
	return out
}

// Strongest keeps one row per asset reference: the row with the highest
// level, in the order the rows came. The drift of a weaker row joins the
// row that stays, so a finding of another provider is not lost.
func Strongest(statuses []AssetStatus) []AssetStatus {
	index := map[AssetRef]int{}
	var out []AssetStatus
	for _, s := range statuses {
		i, seen := index[s.Ref()]
		if !seen {
			index[s.Ref()] = len(out)
			out = append(out, s)
			continue
		}
		if s.Level > out[i].Level {
			s.Drift = joinDrift(s.Drift, out[i].Drift)
			out[i] = s
			continue
		}
		out[i].Drift = joinDrift(out[i].Drift, s.Drift)
	}
	return out
}
