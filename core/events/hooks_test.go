package events

import (
	"testing"

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
