package pdp

import (
	"context"
	"testing"
	"time"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluate_MatchedTagsUnion(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: tag-secret-read
    action: allow
    tags: [secret_read, audit]
    match:
      action_types: [file_read]
      file_patterns: ["**/.env"]
      file_access: [read]
  - id: tag-dotfile
    action: allow
    tags: [dotfile]
    match:
      action_types: [file_read]
  - id: warn-env
    action: warn
    tags: [audit]
    match:
      action_types: [file_read]
      file_patterns: ["**/.env"]
      file_access: [read]
`)
	res, err := engine.Evaluate(context.Background(),
		&model.Action{Type: model.ActionFileRead, Parameters: model.Parameters{Path: "/work/.env"}}, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionWarn, res.Decision, "an allow tag rule does not change the decision")
	assert.Equal(t, []string{"audit"}, res.Tags, "Tags holds the tags of the winning rule")
	assert.Equal(t, []string{"audit", "dotfile", "secret_read"}, res.MatchedTags)
}

func TestCheckTagNames(t *testing.T) {
	cases := []struct {
		tag   string
		valid bool
	}{
		{"secret_read", true},
		{"self-protection", true},
		{"a1", true},
		{"Secret", false},
		{"1tag", false},
		{"has space", false},
		{"dot.tag", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			policy := &Policy{Version: "1", Rules: []Rule{{ID: "r", Action: model.DecisionAllow, Tags: []string{tc.tag}}}}
			err := CheckTagNames(policy)
			if tc.valid {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid tag")

			_, err = New(policy)
			assert.NoError(t, err, "a legacy tag only warns when the policy loads")
		})
	}
}

func TestEvaluate_OriginAndTagContext(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: untrusted-observation
    action: warn
    match:
      action_types: [tool_use]
    condition: 'action.kind == "observation" && action.origin in ["web", "mcp"] && action.source == "github"'
  - id: egress-after-secret
    action: block
    match:
      action_types: [command_exec]
    condition: '"secret_read" in context.tags_seen && context.tag_seq["secret_read"] < 5 && "mcp:github" in context.origins_seen'
    message: "blocked after {{index .Context.TagSeq \"secret_read\"}} with {{.Action.Origin}}"
`)
	observation := &model.Action{Type: model.ActionToolUse, Kind: events.KindObservation, Origin: privacy.OriginMCP, Source: "github"}
	res, err := engine.Evaluate(context.Background(), observation, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionWarn, res.Decision)

	command := &model.Action{Type: model.ActionCommandExec, Kind: events.KindAction, Origin: privacy.OriginCommand, Parameters: model.Parameters{Command: "curl x"}}
	snapshot := &model.ContextSnapshot{TagsSeen: map[string]int64{"secret_read": 3}, OriginsSeen: []string{"mcp:github"}}
	res, err = engine.Evaluate(context.Background(), command, snapshot)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, res.Decision)
	assert.Equal(t, "blocked after 3 with command", res.Message)

	res, err = engine.Evaluate(context.Background(), command, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionAllow, res.Decision, "an empty tag map and origin list evaluate without error")
}

func TestShouldDeferFreshSession_TagFieldsAreKnown(t *testing.T) {
	policy, err := ParsePolicy([]byte(`
version: "1"
rules:
  - id: egress-after-secret
    action: block
    match:
      action_types: [command_exec]
    condition: '"secret_read" in context.tags_seen || "web" in context.origins_seen'
`))
	require.NoError(t, err)
	engine, err := New(policy, WithDeferConfig(DeferConfig{Enabled: true, FreshSessionSeconds: 300}))
	require.NoError(t, err)

	res, err := engine.Evaluate(context.Background(),
		&model.Action{Type: model.ActionCommandExec, Parameters: model.Parameters{Command: "ls"}},
		&model.ContextSnapshot{SessionStartedAt: time.Now()})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionAllow, res.Decision, "a fresh session with no tag is a fact, not missing data")
}

func TestFreshSessionDefer_TagRuleDoesNotSuppressIt(t *testing.T) {
	policy, err := ParsePolicy([]byte(`
version: "1"
rules:
  - id: tag-cmd
    action: allow
    tags: [cmd]
    match:
      action_types: [command_exec]
  - id: block-many
    action: block
    match:
      action_types: [command_exec]
    condition: "context.commands_executed > 3"
`))
	require.NoError(t, err)
	engine, err := New(policy, WithDeferConfig(DeferConfig{Enabled: true, FreshSessionSeconds: 300}))
	require.NoError(t, err)

	res, err := engine.Evaluate(context.Background(),
		&model.Action{Type: model.ActionCommandExec, Parameters: model.Parameters{Command: "ls"}},
		&model.ContextSnapshot{SessionStartedAt: time.Now()})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionDefer, res.Decision)
	assert.Equal(t, DeferReasonFreshSession, res.DeferReason)
	assert.Equal(t, []string{"cmd"}, res.MatchedTags)
}

func TestTagSeq_MissingTag(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: guarded
    action: block
    match:
      action_types: [command_exec]
    condition: '"secret_read" in context.tag_seq && context.tag_seq["secret_read"] > 0'
    message: 'seq {{index .Context.TagSeq "other"}}'
  - id: optional
    action: warn
    match:
      action_types: [command_exec]
    condition: 'context.tag_seq[?"secret_read"].orValue(0) == 0'
`)
	res, err := engine.Evaluate(context.Background(),
		&model.Action{Type: model.ActionCommandExec, Parameters: model.Parameters{Command: "ls"}},
		&model.ContextSnapshot{})
	require.NoError(t, err, "a guarded lookup of a missing tag does not fail")
	assert.Equal(t, model.DecisionWarn, res.Decision)

	res, err = engine.Evaluate(context.Background(),
		&model.Action{Type: model.ActionCommandExec, Parameters: model.Parameters{Command: "ls"}},
		&model.ContextSnapshot{TagsSeen: map[string]int64{"secret_read": 4}})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, res.Decision)
	assert.Equal(t, "seq 0", res.Message, "index on a missing tag gives zero")
}

func TestEvaluate_DenyRuleOnAmbiguousMCPServer(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: deny-evil
    action: block
    match:
      action_types: [tool_use]
    condition: '"evil" in action.sources'
`)
	cases := []struct {
		name   string
		action *model.Action
		want   model.Decision
	}{
		{"ambiguous tool name", &model.Action{Type: model.ActionToolUse, Tool: "mcp__evil__read__file", Origin: privacy.OriginMCP, Sources: []string{"evil", "evil__read"}}, model.DecisionBlock},
		{"other server", &model.Action{Type: model.ActionToolUse, Tool: "mcp__github__get", Origin: privacy.OriginMCP, Source: "github", Sources: []string{"github"}}, model.DecisionAllow},
		{"no sources", &model.Action{Type: model.ActionToolUse, Tool: "Read"}, model.DecisionAllow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := engine.Evaluate(context.Background(), tc.action, &model.ContextSnapshot{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Decision)
		})
	}
}
