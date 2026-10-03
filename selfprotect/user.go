package selfprotect

import (
	"context"
	"sort"
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
}

// HookConfigAssessor reads the Gryph hook entries of one agent. It compares
// the entries only, so the user's other settings never count as drift.
type HookConfigAssessor interface {
	Name() string
	AssessHookConfig(ctx context.Context) HookConfigState
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
		out = append(out, p.status(AssetHookConfig, a.Name(), LevelDetect, state.Path, state.Drift))
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

// Repair implements Provider. The user provider assesses only.
func (p *userProvider) Repair(context.Context, RepairOptions) ([]AssetStatus, error) {
	return nil, ErrRepairUnsupported
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
