package opencode

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
		{"allow blocking hook", "tool.execute.before", agent.DecisionAllow, "", nil, "", 0},
		{"allow other hook", "tool.execute.after", agent.DecisionAllow, "", nil, "", 0},
		{"block blocking hook", "tool.execute.before", agent.DecisionBlock, "reason", NewBlockResponse("reason").JSON(), "reason", 2},
		{"block other hook", "tool.execute.after", agent.DecisionBlock, "reason", nil, "reason", 2},
		{"guidance blocking hook", "tool.execute.before", agent.DecisionGuidance, "advisory", NewGuidanceResponse("advisory").JSON(), "", 0},
		{"guidance other hook", "tool.execute.after", agent.DecisionGuidance, "advisory", nil, "advisory", 0},
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
