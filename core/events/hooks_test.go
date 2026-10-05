package events

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindOf(t *testing.T) {
	prompt := func(origin privacy.Origin) *Event {
		e := NewEvent(uuid.New(), "test", ActionUserPrompt)
		require.NoError(t, e.SetPrompt("fix the bug", origin))
		return e
	}
	post := NewEvent(uuid.New(), "test", ActionCommandExec)
	post.Phase = PhasePost

	cases := []struct {
		name   string
		event  *Event
		linked bool
		want   Kind
	}{
		{"nil event", nil, false, KindAction},
		{"user prompt", prompt(privacy.OriginUser), false, KindIntent},
		{"prompt from agent code", prompt(privacy.OriginAgent), false, KindObservation},
		{"linked post event", post, true, KindObservation},
		{"post event with no pre event", post, false, KindAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, KindOf(tc.event, tc.linked))
		})
	}
}

func TestWithTimeout(t *testing.T) {
	specs := WithTimeout(30*time.Second, []HookSpec{
		{Type: "a"},
		{Type: "b", Timeout: 10 * time.Second},
	})
	assert.Equal(t, 30*time.Second, specs[0].Timeout, "a zero timeout takes the default")
	assert.Equal(t, 10*time.Second, specs[1].Timeout, "a set timeout stays")
}

func TestHookSpec_FailColumn(t *testing.T) {
	tests := []struct {
		name string
		spec HookSpec
		want FailColumn
	}{
		{"blocking pre hook", HookSpec{Phase: PhasePre, Blocking: true}, FailColumnBlocking},
		{"prompt hook", HookSpec{Phase: PhasePre, Blocking: true, Prompt: true}, FailColumnPrompt},
		{"pre hook that cannot block", HookSpec{Phase: PhasePre}, FailColumnOther},
		{"post hook", HookSpec{Phase: PhasePost}, FailColumnOther},
		{"lifecycle hook", HookSpec{Phase: PhaseUnknown}, FailColumnOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.spec.FailColumn())
		})
	}
}
