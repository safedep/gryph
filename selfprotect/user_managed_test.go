package selfprotect

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHookConfigState_Effective(t *testing.T) {
	cases := []struct {
		name       string
		state      HookConfigState
		level      Level
		detail     string
		drift      string
		repairable bool
	}{
		{name: "user scope only", state: HookConfigState{Path: "/h/.claude", Drift: "hooks not installed"},
			level: LevelDetect, detail: "/h/.claude", drift: "hooks not installed", repairable: true},
		{name: "locked managed entry intact", state: HookConfigState{Path: "/h/.claude", Drift: "hooks not installed", Managed: &ManagedEntry{Path: "/etc/claude-code/x.json", Locked: true}},
			level: LevelPreventSameUser, detail: "managed entry at /etc/claude-code/x.json", drift: ""},
		{name: "locked managed entry differs", state: HookConfigState{Path: "/h/.claude", Managed: &ManagedEntry{Path: "/etc/claude-code/x.json", Locked: true, Drift: "differs"}},
			level: LevelDetect, detail: "/h/.claude", drift: "managed entry: differs"},
		{name: "system path entry", state: HookConfigState{Path: "/h/.cursor", Drift: "hooks not installed", Managed: &ManagedEntry{Path: "/etc/cursor/hooks.json"}},
			level: LevelDetect, detail: "/h/.cursor, managed entry at /etc/cursor/hooks.json (system path, user scope still checked)", drift: "hooks not installed", repairable: true},
		{name: "system path entry differs", state: HookConfigState{Path: "/h/.cursor", Managed: &ManagedEntry{Path: "/etc/cursor/hooks.json", Drift: "differs"}},
			level: LevelDetect, detail: "/h/.cursor, managed entry at /etc/cursor/hooks.json (system path, user scope still checked)", drift: "differs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			level, detail, drift := tc.state.effective()
			assert.Equal(t, tc.level, level)
			assert.Equal(t, tc.detail, detail)
			assert.Equal(t, tc.drift, drift)
			assert.Equal(t, tc.repairable, tc.state.repairable())
		})
	}
}
