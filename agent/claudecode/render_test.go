package claudecode

import (
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/stretchr/testify/assert"
)

func TestRenderResponse(t *testing.T) {
	a := New(nil, "", false)

	tests := []struct {
		name     string
		hookType string
		decision agent.HookDecision
		detail   string
		wantOut  []byte
		wantErr  string
		wantCode int
	}{
		{"allow pre hook", "PreToolUse", agent.DecisionAllow, "", nil, "", 0},
		{"allow post hook", "PostToolUse", agent.DecisionAllow, "", nil, "", 0},
		{"block pre hook", "PreToolUse", agent.DecisionBlock, "blocked reason", nil, "blocked reason", 2},
		{"guidance pre hook", "PreToolUse", agent.DecisionGuidance, "advisory", nil, "advisory", 0},
		{"guidance post hook", "PostToolUse", agent.DecisionGuidance, "advisory", nil, "advisory", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := a.RenderResponse(tt.hookType, tt.decision, tt.detail)
			assert.Equal(t, tt.wantOut, resp.Stdout())
			assert.Equal(t, tt.wantErr, resp.Stderr())
			assert.Equal(t, tt.wantCode, resp.ExitCode())
		})
	}
}
