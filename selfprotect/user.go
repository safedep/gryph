package selfprotect

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
)

// UserProviderName is the name of the user-scope provider.
const UserProviderName = "user"

// HookConfigState is the state of one agent's Gryph hook entries.
type HookConfigState struct {
	// Present is true when the agent is on the host. An absent agent has no
	// hook configuration to protect.
	Present bool
	// Path names the file that holds the entries.
	Path string
	// Drift says how the entries differ from a current install. It is empty
	// when they match.
	Drift string
	// Managed is the state of the agent's managed entries, when a managed
	// configuration names the agent. Nil otherwise.
	Managed *ManagedEntry
}

// ManagedEntry is the state of the Gryph entries in an agent's managed hook
// file, the one that root owns.
type ManagedEntry struct {
	// Path names the managed file.
	Path string
	// Locked is true when the vendor documents that the user cannot turn the
	// managed hooks off. Then the user scope no longer decides the level.
	Locked bool
	// Drift says how the managed entries differ from the managed
	// configuration. It is empty when they match. Only root repairs them.
	Drift string
}

// effective turns the state into the level, the detail and the drift of
// the hook_config row. A locked managed entry that matches earns
// LevelPreventSameUser, whatever the user scope holds. A managed entry at
// a system path only adds detail: the user scope still decides.
func (s HookConfigState) effective() (Level, string, string) {
	m := s.Managed
	switch {
	case m == nil:
		return LevelDetect, s.Path, s.Drift
	case m.Locked && m.Drift == "":
		return LevelPreventSameUser, "managed entry at " + m.Path, ""
	case m.Locked:
		return LevelDetect, s.Path, joinDrift("managed entry: "+m.Drift, s.Drift)
	default:
		return LevelDetect, s.Path + ", managed entry at " + m.Path + " (system path, user scope still checked)", joinDrift(s.Drift, m.Drift)
	}
}

// repairable reports whether a repair in the user scope changes the row. A
// locked managed entry that matches leaves nothing for the user scope to
// repair, and a managed entry that differs needs root.
func (s HookConfigState) repairable() bool {
	if s.Drift == "" {
		return false
	}
	return s.Managed == nil || !s.Managed.Locked || s.Managed.Drift != ""
}

func joinDrift(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " and ")
}

// HookConfigAssessor reads the Gryph hook entries of one agent. It compares
// the entries only, so the user's other settings never count as drift.
type HookConfigAssessor interface {
	Name() string
	AssessHookConfig(ctx context.Context) HookConfigState
}

// HookConfigRepairer rewrites the Gryph hook entries of one agent to the
// current install. An assessor without it is assessed only.
type HookConfigRepairer interface {
	HookConfigAssessor
	RepairHookConfig(ctx context.Context) error
}

// UserAssets names the assets of an install in the user scope.
type UserAssets struct {
	// RulesOff is empty when the built-in policy rules run. Otherwise it
	// names the config key that turns them off, and every asset that only
	// those rules protect is at LevelNone.
	RulesOff string
	// Binary is the absolute path of the gryph binary, or empty when the
	// running program has none.
	Binary string
	// ConfigFile is the configuration file in force.
	ConfigFile string
	// ConfigManaged is true when the system managed file is in force. Root
	// owns it and its directories, so the user cannot change it.
	ConfigManaged bool
	// ConfigDrift is set when a managed file exists but Gryph ignores it.
	ConfigDrift string
	PolicyFile  string
	PoliciesDir string
	Store       string
	Keys        []string
	HookConfigs []HookConfigAssessor
}

type userProvider struct {
	assets UserAssets
}

// NewUserProvider returns the provider for an install in the user scope. It
// runs as the user, so it reads only what the user can read, and it repairs
// nothing yet.
func NewUserProvider(assets UserAssets) Provider {
	return &userProvider{assets: assets}
}

func (p *userProvider) Name() string { return UserProviderName }

// Assess implements Provider. Hook configurations come first, sorted by
// agent, then the other assets in a fixed order.
func (p *userProvider) Assess(ctx context.Context) []AssetStatus {
	var out []AssetStatus
	assessors := append([]HookConfigAssessor(nil), p.assets.HookConfigs...)
	sort.Slice(assessors, func(i, j int) bool { return assessors[i].Name() < assessors[j].Name() })
	for _, a := range assessors {
		state := a.AssessHookConfig(ctx)
		if !state.Present {
			continue
		}
		level, detail, drift := state.effective()
		out = append(out, p.status(AssetHookConfig, a.Name(), level, detail, drift))
	}

	if p.assets.Binary == "" {
		out = append(out, p.status(AssetBinary, "", LevelNone, "the running program has no absolute path", ""))
	} else {
		out = append(out, p.mediated(AssetBinary, p.assets.Binary, ""))
	}
	out = append(out, p.mediated(AssetPolicy, p.assets.PolicyFile+", "+p.assets.PoliciesDir, ""))
	if p.assets.ConfigManaged {
		out = append(out, p.status(AssetConfig, "", LevelPreventSameUser, p.assets.ConfigFile, ""))
	} else {
		out = append(out, p.mediated(AssetConfig, p.assets.ConfigFile, p.assets.ConfigDrift))
	}
	out = append(out, p.mediated(AssetStore, p.assets.Store, ""))
	for _, key := range p.assets.Keys {
		out = append(out, p.mediated(AssetKey, key, ""))
	}
	return out
}

// Repair implements Provider. It rewrites the Gryph entries of every hook
// configuration with drift, or of the ones in opts.Only, and returns the
// state of each after the repair. A failed asset is a RepairError in the
// joined error, and the other assets are still repaired.
func (p *userProvider) Repair(ctx context.Context, opts RepairOptions) ([]AssetStatus, error) {
	var (
		out  []AssetStatus
		errs []error
	)
	for _, a := range p.assets.HookConfigs {
		repairer, ok := a.(HookConfigRepairer)
		if !ok {
			continue
		}
		ref := AssetRef{Asset: AssetHookConfig, Agent: a.Name()}
		if len(opts.Only) > 0 && !slices.Contains(opts.Only, ref) {
			continue
		}
		before := a.AssessHookConfig(ctx)
		if !before.Present || !before.repairable() {
			continue
		}
		beforeLevel, beforeDetail, beforeDrift := before.effective()
		if opts.DryRun {
			out = append(out, p.status(AssetHookConfig, a.Name(), beforeLevel, beforeDetail, beforeDrift))
			continue
		}
		if err := repairer.RepairHookConfig(ctx); err != nil {
			errs = append(errs, &RepairError{Ref: ref, Err: err})
			out = append(out, p.status(AssetHookConfig, a.Name(), beforeLevel, beforeDetail, beforeDrift))
			continue
		}
		after := a.AssessHookConfig(ctx)
		if after.Drift != "" {
			errs = append(errs, &RepairError{Ref: ref, Err: errors.New("the entries still differ after the repair: " + after.Drift)})
		}
		level, detail, drift := after.effective()
		out = append(out, p.status(AssetHookConfig, a.Name(), level, detail, drift))
	}
	return out, errors.Join(errs...)
}

// mediated builds the row of an asset that only the built-in rules protect.
// When the rules are off, the detail names the config key, so the operator
// sees what to change.
func (p *userProvider) mediated(asset Asset, detail, drift string) AssetStatus {
	if p.assets.RulesOff != "" {
		return p.status(asset, "", LevelNone, p.assets.RulesOff+" is false, "+detail, drift)
	}
	return p.status(asset, "", LevelMediated, detail, drift)
}

func (p *userProvider) status(asset Asset, agent string, level Level, detail, drift string) AssetStatus {
	return AssetStatus{
		Asset:    asset,
		Agent:    agent,
		Level:    level,
		Provider: UserProviderName,
		Drift:    drift,
		Detail:   detail,
	}
}
