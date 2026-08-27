package cursor

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
	}{
		{
			"allow preToolUse", "preToolUse", agent.DecisionAllow, "",
			GeneratePreToolUseResponse(NewAllowResponse()), "",
		},
		{
			"allow permission hook", "beforeShellExecution", agent.DecisionAllow, "",
			GeneratePermissionResponse(NewAllowResponse()), "",
		},
		{
			"allow continue hook", "beforeSubmitPrompt", agent.DecisionAllow, "",
			GenerateContinueResponse(true, ""), "",
		},
		{
			"allow stop hook", "stop", agent.DecisionAllow, "",
			GenerateStopResponse(""), "",
		},
		{
			"allow post hook", "afterFileEdit", agent.DecisionAllow, "",
			[]byte("{}"), "",
		},
		{
			"block preToolUse", "preToolUse", agent.DecisionBlock, "reason",
			GeneratePreToolUseResponse(NewDenyResponse("reason")), "",
		},
		{
			"block permission hook", "beforeReadFile", agent.DecisionBlock, "reason",
			GeneratePermissionResponse(NewDenyResponse("reason")), "",
		},
		{
			"block continue hook", "sessionStart", agent.DecisionBlock, "reason",
			GenerateContinueResponse(false, "reason"), "",
		},
		{
			"block post hook", "afterFileEdit", agent.DecisionBlock, "reason",
			[]byte("{}"), "",
		},
		{
			"guidance preToolUse routes text to stderr", "preToolUse", agent.DecisionGuidance, "advisory",
			GeneratePreToolUseResponse(NewGuidanceResponse("advisory")), "advisory",
		},
		{
			"guidance permission hook", "beforeMCPExecution", agent.DecisionGuidance, "advisory",
			GeneratePermissionResponse(NewGuidanceResponse("advisory")), "",
		},
		{
			"guidance continue hook", "beforeSubmitPrompt", agent.DecisionGuidance, "advisory",
			GenerateContinueResponse(true, "advisory"), "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := a.RenderResponse(tt.hookType, tt.decision, tt.detail)
			assert.Equal(t, tt.wantOut, resp.Stdout())
			assert.Equal(t, tt.wantErr, resp.Stderr())
			assert.Equal(t, 0, resp.ExitCode())
		})
	}
}
