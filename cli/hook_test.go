package cli

import (
	"context"
	"os"
	"testing"

	"github.com/safedep/gryph/agent"
	"github.com/safedep/gryph/agent/claudecode"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/decision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubService struct {
	resp *decision.HookResponse
	req  *decision.HookRequest
}

func (s *stubService) Handle(_ context.Context, req *decision.HookRequest) (*decision.HookResponse, error) {
	s.req = req
	return s.resp, nil
}

func TestRunHook_RendersDecision(t *testing.T) {
	cases := []struct {
		name     string
		resp     *decision.HookResponse
		wantCode int
		wantMsg  string
	}{
		{name: "allow", resp: &decision.HookResponse{Decision: decision.VerdictOf(security.DecisionAllow)}},
		{name: "block", resp: &decision.HookResponse{Decision: decision.VerdictOf(security.DecisionBlock), Reason: "no"},
			wantCode: 2, wantMsg: "no"},
		{name: "decision from a newer service blocks", resp: &decision.HookResponse{Decision: "defer"},
			wantCode: 2, wantMsg: `gryph: unknown decision "defer"`},
		{name: "missing decision blocks", resp: &decision.HookResponse{},
			wantCode: 2, wantMsg: "gryph: the decision service returned no decision"},
	}

	payload, err := os.ReadFile("../agent/claudecode/testdata/pre_tool_use_bash.json")
	require.NoError(t, err)

	registry := agent.NewRegistry()
	claudecode.Register(registry, nil, config.LoggingFull, false)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &stubService{resp: tc.resp}
			err := runHook(context.Background(), registry, svc, claudecode.AgentName, "PreToolUse", payload)

			require.NotNil(t, svc.req)
			assert.Equal(t, claudecode.AgentName, svc.req.Event.AgentName)

			if tc.wantCode == 0 {
				assert.NoError(t, err)
				return
			}
			var exitErr *exitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, tc.wantCode, exitErr.ExitCode())
			assert.Equal(t, tc.wantMsg, exitErr.Message())
		})
	}
}
