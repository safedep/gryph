package selfprotect

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAssessor struct {
	name  string
	state HookConfigState
}

func (f fakeAssessor) Name() string                                     { return f.name }
func (f fakeAssessor) AssessHookConfig(context.Context) HookConfigState { return f.state }

// fakeRepairer clears its drift on repair, unless fail is set.
type fakeRepairer struct {
	name    string
	state   *HookConfigState
	fail    error
	repairs int
}

func (f *fakeRepairer) Name() string                                     { return f.name }
func (f *fakeRepairer) AssessHookConfig(context.Context) HookConfigState { return *f.state }
func (f *fakeRepairer) RepairHookConfig(context.Context) error {
	f.repairs++
	if f.fail != nil {
		return f.fail
	}
	f.state.Drift = ""
	return nil
}

func userAssets() UserAssets {
	return UserAssets{
		Binary:      "/opt/safedep/gryph/bin/gryph",
		ConfigFile:  "/home/u/.config/safedep/gryph/config.yml",
		PolicyFile:  "/home/u/.config/safedep/gryph/policy.yaml",
		PoliciesDir: "/home/u/.config/safedep/gryph/policies",
		Store:       "/home/u/.local/share/safedep/gryph/audit.db",
		Keys:        []string{"/home/u/.config/safedep/gryph/keys/receipt.key", "/home/u/.local/share/safedep/gryph/export.key"},
		HookConfigs: []HookConfigAssessor{
			fakeAssessor{name: "cursor", state: HookConfigState{Present: true, Path: "/home/u/.cursor", Drift: "hooks not installed"}},
			fakeAssessor{name: "claude-code", state: HookConfigState{Present: true, Path: "/home/u/.claude"}},
			fakeAssessor{name: "gemini", state: HookConfigState{Present: false}},
		},
	}
}

func byAsset(statuses []AssetStatus, asset Asset) AssetStatus {
	for _, s := range statuses {
		if s.Asset == asset {
			return s
		}
	}
	return AssetStatus{}
}

func TestUserProvider_Assess_Guard(t *testing.T) {
	p := NewUserProvider(userAssets())
	assert.Equal(t, "user", p.Name())

	statuses := p.Assess(context.Background())
	require.Len(t, statuses, 8, "two present agents, binary, policy, config, store and two keys")
	assert.Equal(t, ProfileGuard, ProfileOf(statuses))

	assert.Equal(t, AssetStatus{Asset: AssetHookConfig, Agent: "claude-code", Level: LevelDetect, Provider: "user", Detail: "/home/u/.claude"}, statuses[0], "agents sort by name")
	assert.Equal(t, AssetStatus{Asset: AssetHookConfig, Agent: "cursor", Level: LevelDetect, Provider: "user", Detail: "/home/u/.cursor", Drift: "hooks not installed"}, statuses[1])
	for _, s := range statuses {
		assert.NotEqual(t, "gemini", s.Agent, "an absent agent has no row")
		assert.Equal(t, "user", s.Provider)
		assert.False(t, s.Attest)
	}
	assert.Equal(t, LevelMediated, byAsset(statuses, AssetBinary).Level)
	assert.Equal(t, "/opt/safedep/gryph/bin/gryph", byAsset(statuses, AssetBinary).Detail)
	assert.Equal(t, LevelMediated, byAsset(statuses, AssetPolicy).Level)
	assert.Equal(t, "/home/u/.config/safedep/gryph/policy.yaml, /home/u/.config/safedep/gryph/policies", byAsset(statuses, AssetPolicy).Detail)
	assert.Equal(t, LevelMediated, byAsset(statuses, AssetConfig).Level)
	assert.Equal(t, LevelMediated, byAsset(statuses, AssetStore).Level)
	assert.Equal(t, LevelMediated, statuses[6].Level)
	assert.Equal(t, LevelMediated, statuses[7].Level)
	assert.Equal(t, AssetKey, statuses[7].Asset)
}

func TestUserProvider_Assess_RulesOff(t *testing.T) {
	assets := userAssets()
	assets.RulesOff = "policy.enabled"
	statuses := NewUserProvider(assets).Assess(context.Background())

	assert.Equal(t, ProfileNone, ProfileOf(statuses))
	assert.Equal(t, LevelDetect, statuses[0].Level, "detection does not depend on the rules")
	for _, s := range statuses[2:] {
		assert.Equal(t, LevelNone, s.Level, "%s", s.Asset)
		if s.Asset != AssetBinary {
			assert.Contains(t, s.Detail, "policy.enabled is false, ", "%s", s.Asset)
		}
	}
	assert.Equal(t, "policy.enabled is false, /opt/safedep/gryph/bin/gryph", byAsset(statuses, AssetBinary).Detail)
}

func TestUserProvider_Assess_BinaryWithoutPath(t *testing.T) {
	assets := userAssets()
	assets.Binary = ""
	statuses := NewUserProvider(assets).Assess(context.Background())

	binary := byAsset(statuses, AssetBinary)
	assert.Equal(t, LevelNone, binary.Level)
	assert.Equal(t, "the running program has no absolute path", binary.Detail)
	assert.Equal(t, ProfileNone, ProfileOf(statuses))
}

func TestUserProvider_Assess_ManagedConfig(t *testing.T) {
	assets := userAssets()
	assets.ConfigFile = "/etc/safedep/gryph/config.yml"
	assets.ConfigManaged = true
	statuses := NewUserProvider(assets).Assess(context.Background())

	cfg := byAsset(statuses, AssetConfig)
	assert.Equal(t, LevelPreventSameUser, cfg.Level)
	assert.Equal(t, "/etc/safedep/gryph/config.yml", cfg.Detail)
	assert.Equal(t, ProfileGuard, ProfileOf(statuses), "one asset above guard does not make locked")

	assets.ConfigManaged = false
	assets.ConfigDrift = "managed file ignored: not owned by root"
	cfg = byAsset(NewUserProvider(assets).Assess(context.Background()), AssetConfig)
	assert.Equal(t, LevelMediated, cfg.Level)
	assert.Equal(t, "managed file ignored: not owned by root", cfg.Drift)
}

func TestUserProvider_Repair(t *testing.T) {
	ctx := context.Background()
	broken := &fakeRepairer{name: "claude-code", state: &HookConfigState{Present: true, Path: "/home/u/.claude", Drift: "hooks not installed"}}
	stuck := &fakeRepairer{name: "cursor", state: &HookConfigState{Present: true, Path: "/home/u/.cursor", Drift: "hooks are invalid"}, fail: errors.New("settings.json is a symbolic link")}
	fine := &fakeRepairer{name: "gemini", state: &HookConfigState{Present: true, Path: "/home/u/.gemini"}}
	absent := &fakeRepairer{name: "codex", state: &HookConfigState{Present: false, Drift: "hooks not installed"}}
	readOnly := fakeAssessor{name: "devin", state: HookConfigState{Present: true, Drift: "hooks not installed"}}
	assets := userAssets()
	assets.HookConfigs = []HookConfigAssessor{broken, stuck, fine, absent, readOnly}
	p := NewUserProvider(assets)

	dry, err := p.Repair(ctx, RepairOptions{DryRun: true})
	require.NoError(t, err)
	require.Len(t, dry, 2, "the two present assets with drift")
	assert.Equal(t, 0, broken.repairs, "a dry run repairs nothing")

	out, err := p.Repair(ctx, RepairOptions{})
	require.Error(t, err)
	var repairErr *RepairError
	require.ErrorAs(t, err, &repairErr)
	assert.Equal(t, AssetRef{Asset: AssetHookConfig, Agent: "cursor"}, repairErr.Ref)
	assert.EqualError(t, repairErr, "hook_config cursor: settings.json is a symbolic link")
	require.Len(t, out, 2)
	assert.Equal(t, AssetStatus{Asset: AssetHookConfig, Agent: "claude-code", Level: LevelDetect, Provider: "user", Detail: "/home/u/.claude"}, out[0], "repaired")
	assert.Equal(t, "hooks are invalid", out[1].Drift, "the failed asset keeps its drift")
	assert.Equal(t, 1, broken.repairs)
	assert.Equal(t, 1, stuck.repairs)
	assert.Equal(t, 0, fine.repairs, "no drift, no repair")
	assert.Equal(t, 0, absent.repairs, "an absent agent is not repaired")

	broken.state.Drift = "hooks not installed"
	out, err = p.Repair(ctx, RepairOptions{Only: []AssetRef{{Asset: AssetHookConfig, Agent: "claude-code"}}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, 2, broken.repairs)
	assert.Equal(t, 1, stuck.repairs, "not in Only")
}
