package gemini

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
		{"allow BeforeTool", "BeforeTool", agent.DecisionAllow, "", NewAllowResponse().JSON(), "", 0},
		{"allow other hook", "AfterTool", agent.DecisionAllow, "", []byte("{}"), "", 0},
		{"block BeforeTool", "BeforeTool", agent.DecisionBlock, "reason", NewBlockResponse("reason").JSON(), "reason", 2},
		{"block other hook", "AfterTool", agent.DecisionBlock, "reason", nil, "reason", 2},
		{"guidance BeforeTool", "BeforeTool", agent.DecisionGuidance, "advisory", NewGuidanceResponse("advisory").JSON(), "", 0},
		{"guidance other hook", "AfterTool", agent.DecisionGuidance, "advisory", nil, "advisory", 0},
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
