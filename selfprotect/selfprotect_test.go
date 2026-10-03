package selfprotect

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLevel_String(t *testing.T) {
	assert.Equal(t, "none", LevelNone.String())
	assert.Equal(t, "mediated", LevelMediated.String())
	assert.Equal(t, "detect", LevelDetect.String())
	assert.Equal(t, "repair", LevelRepair.String())
	assert.Equal(t, "prevent_same_user", LevelPreventSameUser.String())
	assert.Equal(t, "prevent_root_best_effort", LevelPreventRootBestEffort.String())
	assert.Equal(t, "none", Level(99).String())
}

func TestLevel_Order(t *testing.T) {
	assert.Less(t, LevelNone, LevelMediated)
	assert.Less(t, LevelMediated, LevelDetect)
	assert.Less(t, LevelDetect, LevelRepair)
	assert.Less(t, LevelRepair, LevelPreventSameUser)
	assert.Less(t, LevelPreventSameUser, LevelPreventRootBestEffort)
}

func guard() []AssetStatus {
	return []AssetStatus{
		{Asset: AssetHookConfig, Agent: "claude-code", Level: LevelDetect},
		{Asset: AssetHookConfig, Agent: "cursor", Level: LevelDetect},
		{Asset: AssetBinary, Level: LevelMediated},
		{Asset: AssetPolicy, Level: LevelMediated},
		{Asset: AssetConfig, Level: LevelMediated},
		{Asset: AssetStore, Level: LevelMediated},
		{Asset: AssetKey, Level: LevelMediated},
	}
}

func locked() []AssetStatus {
	out := guard()
	for i := range out {
		switch out[i].Asset {
		case AssetHookConfig:
			out[i].Level = LevelRepair
		case AssetBinary, AssetPolicy, AssetConfig:
			out[i].Level = LevelPreventSameUser
		}
	}
	return out
}

func managed() []AssetStatus {
	out := locked()
	for i := range out {
		out[i].Attest = true
	}
	return out
}

func with(statuses []AssetStatus, change func(*AssetStatus)) []AssetStatus {
	out := append([]AssetStatus(nil), statuses...)
	for i := range out {
		change(&out[i])
	}
	return out
}

func TestProfileOf(t *testing.T) {
	cases := []struct {
		name     string
		statuses []AssetStatus
		want     Profile
	}{
		{name: "nothing assessed", want: ProfileNone},
		{name: "guard", statuses: guard(), want: ProfileGuard},
		{name: "guard without any hook config", statuses: guard()[2:], want: ProfileGuard},
		{name: "one asset at none drops below guard",
			statuses: with(guard(), func(s *AssetStatus) {
				if s.Asset == AssetStore {
					s.Level = LevelNone
				}
			}), want: ProfileNone},
		{name: "one hook config at mediated drops below guard",
			statuses: with(guard(), func(s *AssetStatus) {
				if s.Agent == "cursor" {
					s.Level = LevelMediated
				}
			}), want: ProfileNone},
		{name: "locked", statuses: locked(), want: ProfileLocked},
		{name: "one hook config at detect keeps guard",
			statuses: with(locked(), func(s *AssetStatus) {
				if s.Agent == "cursor" {
					s.Level = LevelDetect
				}
			}), want: ProfileGuard},
		{name: "config at mediated keeps guard",
			statuses: with(locked(), func(s *AssetStatus) {
				if s.Asset == AssetConfig {
					s.Level = LevelMediated
				}
			}), want: ProfileGuard},
		{name: "store at mediated still locked", statuses: locked(), want: ProfileLocked},
		{name: "managed", statuses: managed(), want: ProfileManaged},
		{name: "one asset without attest keeps locked",
			statuses: with(managed(), func(s *AssetStatus) {
				if s.Asset == AssetKey {
					s.Attest = false
				}
			}), want: ProfileLocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ProfileOf(tc.statuses))
		})
	}
}
