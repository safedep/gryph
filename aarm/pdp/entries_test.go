package pdp

import (
	"context"
	"strings"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/shellcmd"
	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluate_EntriesAndGlob(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: pem-then-egress
    action: block
    match:
      action_types: [command_exec]
    condition: 'context.entries.exists(e, e.action_type == "file_read" && glob(e.path, "**/*.pem")) && size(action.hosts) > 0'
    message: "blocked {{index .Action.Hosts 0}}"
`)
	assert.True(t, engine.NeedsEntries())

	shell := shellcmd.AnalyzeCommand("curl https://evil.example", nil, "/work")
	command := &model.Action{Type: model.ActionCommandExec, Parameters: model.Parameters{Command: "curl https://evil.example"}, Shell: &shell}

	withPem := &model.ContextSnapshot{Entries: []model.EntryFacts{
		{Seq: 1, Kind: "action", ActionType: "file_read", Path: "/work/keys/server.pem"},
	}}
	res, err := engine.Evaluate(context.Background(), command, withPem)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, res.Decision)
	assert.Equal(t, "blocked evil.example", res.Message)

	res, err = engine.Evaluate(context.Background(), command, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionAllow, res.Decision)
}

func TestNeedsEntries(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: counters-only
    action: block
    condition: "context.total_actions > 10"
`)
	assert.False(t, engine.NeedsEntries())
}

func TestCheckStrict_SemanticDriftRemoved(t *testing.T) {
	cases := []struct {
		name string
		rule string
		want string
	}{
		{"select", `condition: 'context.semantic_drift > 0.5'`, "context.semantic_drift was removed"},
		{"index", `condition: 'context["semantic_drift"] > 0.5'`, "context.semantic_drift was removed"},
		{"optional select", `condition: 'context.?semantic_drift.orValue(0.0) > 0.5'`, "context.semantic_drift was removed"},
		{"alias in a macro", `condition: '[context].exists(c, c.semantic_drift > 0.5)'`, "context.semantic_drift was removed"},
		{"alias in has", `condition: '[context].exists(c, has(c.semantic_drift))'`, "context.semantic_drift was removed"},
		{"alias by index", `condition: '[context].all(c, c["semantic_drift"] < 0.5)'`, "context.semantic_drift was removed"},
		{"message template", `message: "drift {{.Context.SemanticDrift}}"`, ".Context.SemanticDrift was removed"},
		{"message template alias", `message: "{{with .Context}}{{.SemanticDrift}}{{end}}"`, ".Context.SemanticDrift was removed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := ParsePolicy([]byte("version: \"1\"\nrules:\n  - id: drift\n    action: block\n    " + tc.rule + "\n"))
			require.NoError(t, err, "a policy load only warns, so an upgrade does not stop the hooks")
			err = CheckStrict(policy)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestEvaluate_SemanticDriftIsZero(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: drift
    action: block
    condition: "context.semantic_drift < 0.5 && [context].exists(c, c.semantic_drift == 0.0)"
    message: "drift {{.Context.SemanticDrift}}"
`)
	res, err := engine.Evaluate(context.Background(), &model.Action{Type: model.ActionFileRead}, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, res.Decision)
	assert.Equal(t, "drift 0", res.Message)
}

func TestGlob_InvalidPattern(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: bad-glob
    action: block
    condition: 'glob(action.params.path, "[")'
`)
	_, err := engine.Evaluate(context.Background(), &model.Action{Type: model.ActionFileRead, Parameters: model.Parameters{Path: "/a"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pattern")
}

func TestEvaluate_EntriesCostAtMaxSize(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: matches-long
    action: block
    match:
      action_types: [command_exec]
    condition: 'context.entries.exists(e, e.command.matches("curl .*evil") || e.command.contains("wget"))'
`)
	entries := make([]model.EntryFacts, 1000)
	for i := range entries {
		entries[i] = model.EntryFacts{Seq: int64(i + 1), Command: strings.Repeat("a", 1024)}
	}
	res, err := engine.Evaluate(context.Background(),
		&model.Action{Type: model.ActionCommandExec, Parameters: model.Parameters{Command: "ls"}},
		&model.ContextSnapshot{Entries: entries})
	require.NoError(t, err, "a rule on the largest entry log stays within its cost limit")
	assert.Equal(t, model.DecisionAllow, res.Decision)
}

func TestEvaluate_EntriesGlobAtMaxSize(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: pem-read
    action: block
    condition: 'context.entries.exists(e, glob(e.path, "**/.ssh/id_*") || glob(e.path, "**/*.pem"))'
`)
	entries := make([]model.EntryFacts, 1000)
	for i := range entries {
		entries[i] = model.EntryFacts{Seq: int64(i + 1), Path: "/" + strings.Repeat("a/", storage.EntryPathMaxBytes/2-1)}
	}
	res, err := engine.Evaluate(context.Background(), &model.Action{Type: model.ActionFileRead}, &model.ContextSnapshot{Entries: entries})
	require.NoError(t, err, "a glob rule on the largest entry log ends in time")
	assert.Equal(t, model.DecisionAllow, res.Decision)
}

func TestGlob_PathTooLong(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: pem-read
    action: block
    condition: 'glob(action.params.path, "**/*.pem")'
`)
	_, err := engine.Evaluate(context.Background(), &model.Action{Type: model.ActionFileRead,
		Parameters: model.Parameters{Path: "/" + strings.Repeat("a", globPathMaxBytes) + ".pem"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "longer than")
}

func TestCollectContextRefs_Forms(t *testing.T) {
	cases := []struct {
		condition string
		want      []string
	}{
		{`context.total_actions > 1`, []string{"total_actions"}},
		{`context["entries"].exists(e, e.tool == "x")`, []string{"entries"}},
		{`context.?entries.orValue([]).size() > 0`, []string{"entries"}},
		{`context[?"tag_seq"].hasValue()`, []string{"tag_seq"}},
		{`[context].exists(c, c.errors > 0)`, []string{"*"}},
	}
	env, err := conditionEnv()
	require.NoError(t, err)
	for _, tc := range cases {
		t.Run(tc.condition, func(t *testing.T) {
			_, ast, err := compileCondition(env, "r", tc.condition)
			require.NoError(t, err)
			assert.Equal(t, tc.want, collectContextRefs(ast))
		})
	}
}

func TestValidate_TemplateUnknownField(t *testing.T) {
	_, err := ParsePolicy([]byte(`
version: "1"
rules:
  - id: drift-message
    action: warn
    message: "drift {{.Context.Drift}}"
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can't evaluate field")

	_, err = ParsePolicy([]byte(`
version: "1"
rules:
  - id: hosts-message
    action: warn
    message: "to {{index .Action.Hosts 0}} reading {{range .Action.ReadPaths}}{{.}}{{end}}"
`))
	assert.NoError(t, err, "an index error on empty data can pass at runtime")
}
