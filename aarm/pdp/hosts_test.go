package pdp

import (
	"context"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/shellcmd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnownHosts(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: strict-egress
    action: block
    match:
      action_types: [command_exec]
      tool_names: [Strict]
    condition: 'size(action.hosts) > 0'
  - id: known-egress
    action: block
    match:
      action_types: [command_exec]
      tool_names: [Known]
    condition: 'size(known_hosts(action.hosts)) > 0'
  - id: known-earlier-egress
    action: block
    match:
      action_types: [command_exec]
      tool_names: [Earlier]
    condition: 'size(known_hosts(context.egress_hosts)) > 0'
`)

	cases := []struct {
		name    string
		tool    string
		command string
		egress  []string
		want    model.Decision
	}{
		{"strict rule blocks a known host", "Strict", "curl https://a.example", nil, model.DecisionBlock},
		{"strict rule blocks the unknown host", "Strict", `eval "$(brew shellenv)"`, nil, model.DecisionBlock},
		{"known rule blocks a known host", "Known", "curl https://a.example", nil, model.DecisionBlock},
		{"known rule skips the unknown host", "Known", `eval "$(brew shellenv)"`, nil, model.DecisionAllow},
		{"known rule blocks a known host next to the unknown host", "Known", `curl https://a.example; curl "$URL"`, nil, model.DecisionBlock},
		{"known rule skips a command with no host", "Known", "ls", nil, model.DecisionAllow},
		{"context rule skips only unknown hosts", "Earlier", "ls", []string{shellcmd.UnknownHost}, model.DecisionAllow},
		{"context rule blocks a known earlier host", "Earlier", "ls", []string{shellcmd.UnknownHost, "a.example"}, model.DecisionBlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shell := shellcmd.AnalyzeCommand(tc.command, nil, "/work")
			action := &model.Action{Type: model.ActionCommandExec, Tool: tc.tool, Parameters: model.Parameters{Command: tc.command}, Shell: &shell}
			res, err := engine.Evaluate(context.Background(), action, &model.ContextSnapshot{EgressHosts: tc.egress})
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Decision)
		})
	}
}

func TestKnownHosts_RejectsAString(t *testing.T) {
	_, err := ParsePolicy([]byte(`
version: "1"
rules:
  - id: bad
    action: block
    condition: 'size(known_hosts("a.example")) > 0'
`))
	assert.ErrorContains(t, err, "known_hosts")
}
