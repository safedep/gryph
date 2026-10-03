package selfprotect

import (
	"context"
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
	changed, err := NewUserProvider(userAssets()).Repair(context.Background(), RepairOptions{})
	assert.ErrorIs(t, err, ErrRepairUnsupported)
	assert.Nil(t, changed)
}
