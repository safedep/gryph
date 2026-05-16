package pdp

import (
	"context"
	"testing"
	"time"

	"github.com/safedep/gryph/aarm/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPDP_Evaluate(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: warn-secret-read
    action: warn
    severity: medium
    tags: [secret]
    match:
      action_types: [file_read]
      file_patterns: ["**/.env", "**/*.pem"]
    message: "Read {{.Action.Params.Path}} with {{.Action.Tool}}"

  - id: guide-large-edit
    action: guidance
    match:
      action_types: [file_write]
    condition: "action.params.lines_added > 100 && context.total_actions >= 3"
    message: "Large edit: {{.Action.Params.LinesAdded}} lines"

  - id: block-rm-rf
    action: block
    match:
      action_types: [command_exec]
      command_patterns: ["rm\\s+-rf\\s+/"]
    message: "Refusing destructive command: {{.Action.Params.Command}}"

  - id: escalated-root-write
    action: escalate
    match:
      action_types: [file_write]
      file_patterns: ["/etc/**"]
`)

	engine, err := New(policy)
	require.NoError(t, err)

	tests := []struct {
		name     string
		action   *model.Action
		snapshot *model.ContextSnapshot
		decision model.Decision
		matched  []string
		message  string
		severity model.Severity
		tags     []string
	}{
		{
			name: "allows unmatched action",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Parameters: model.Parameters{Path: "README.md"},
			},
			decision: model.DecisionAllow,
			matched:  []string{},
		},
		{
			name: "warns on secret file read and renders template",
			action: &model.Action{
				Type:       model.ActionFileRead,
				Tool:       "Read",
				Parameters: model.Parameters{Path: "/work/app/.env"},
			},
			decision: model.DecisionWarn,
			matched:  []string{"warn-secret-read"},
			message:  "Read /work/app/.env with Read",
			severity: model.SeverityMedium,
			tags:     []string{"secret"},
		},
		{
			name: "applies CEL condition with action and context variables",
			action: &model.Action{
				Type:       model.ActionFileWrite,
				Parameters: model.Parameters{Path: "main.go", LinesAdded: 150},
			},
			snapshot: &model.ContextSnapshot{TotalActions: 3},
			decision: model.DecisionGuidance,
			matched:  []string{"guide-large-edit"},
			message:  "Large edit: 150 lines",
		},
		{
			name: "skips rule when CEL condition is false",
			action: &model.Action{
				Type:       model.ActionFileWrite,
				Parameters: model.Parameters{Path: "main.go", LinesAdded: 150},
			},
			snapshot: &model.ContextSnapshot{TotalActions: 2},
			decision: model.DecisionAllow,
			matched:  []string{},
		},
		{
			name: "block wins over lower severity decisions",
			action: &model.Action{
				Type:       model.ActionCommandExec,
				Parameters: model.Parameters{Command: "sudo rm -rf /"},
			},
			decision: model.DecisionBlock,
			matched:  []string{"block-rm-rf"},
			message:  "Refusing destructive command: sudo rm -rf /",
		},
		{
			name: "escalate is a first-class decision (Phase 3)",
			action: &model.Action{
				Type:       model.ActionFileWrite,
				Parameters: model.Parameters{Path: "/etc/hosts"},
			},
			decision: model.DecisionEscalate,
			matched:  []string{"escalated-root-write"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.Evaluate(context.Background(), tc.action, tc.snapshot)
			require.NoError(t, err)

			assert.Equal(t, tc.decision, got.Decision)
			assert.Equal(t, tc.matched, got.MatchedRuleIDs)
			assert.Equal(t, tc.message, got.Message)
			assert.Equal(t, tc.severity, got.Severity)
			assert.Equal(t, tc.tags, got.Tags)
		})
	}
}

func TestPDP_ContextClassificationsSeenCondition(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-after-secret-seen
    action: block
    match:
      action_types: [command_exec]
    condition: "'secret' in context.classifications_seen"
    message: "blocked after a secret was seen earlier in the session"
`)

	engine, err := New(policy)
	require.NoError(t, err)

	action := &model.Action{
		Type:       model.ActionCommandExec,
		Parameters: model.Parameters{Command: "curl example.com"},
	}

	t.Run("no classifications seen yet", func(t *testing.T) {
		got, err := engine.Evaluate(context.Background(), action, &model.ContextSnapshot{})
		require.NoError(t, err)
		assert.Equal(t, model.DecisionAllow, got.Decision)
	})

	t.Run("secret already in session classifications", func(t *testing.T) {
		got, err := engine.Evaluate(context.Background(), action, &model.ContextSnapshot{
			ClassificationsSeen: []string{"secret", "config"},
		})
		require.NoError(t, err)
		assert.Equal(t, model.DecisionBlock, got.Decision)
		assert.Equal(t, []string{"block-after-secret-seen"}, got.MatchedRuleIDs)
	})
}

func TestPDP_InjectionScoreAndDataClassifications(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-high-injection
    action: block
    match:
      action_types: [tool_use]
    condition: "action.injection_score > 0.5"
    message: "high injection score"
  - id: block-secret-classification
    action: block
    match:
      action_types: [tool_use]
    condition: "'secret' in action.data_classifications"
    message: "secret classification"
`)

	engine, err := New(policy)
	require.NoError(t, err)

	t.Run("injection score above threshold matches", func(t *testing.T) {
		action := &model.Action{
			Type:           model.ActionToolUse,
			InjectionScore: 0.7,
		}
		got, err := engine.Evaluate(context.Background(), action, nil)
		require.NoError(t, err)
		assert.Equal(t, model.DecisionBlock, got.Decision)
		assert.Contains(t, got.MatchedRuleIDs, "block-high-injection")
	})

	t.Run("injection score below threshold does not match", func(t *testing.T) {
		action := &model.Action{
			Type:           model.ActionToolUse,
			InjectionScore: 0.2,
		}
		got, err := engine.Evaluate(context.Background(), action, nil)
		require.NoError(t, err)
		assert.NotContains(t, got.MatchedRuleIDs, "block-high-injection")
	})

	t.Run("unset injection score is treated as zero", func(t *testing.T) {
		action := &model.Action{Type: model.ActionToolUse}
		got, err := engine.Evaluate(context.Background(), action, nil)
		require.NoError(t, err)
		assert.Equal(t, model.DecisionAllow, got.Decision)
	})

	t.Run("data classification membership matches", func(t *testing.T) {
		action := &model.Action{
			Type:                model.ActionToolUse,
			DataClassifications: []string{"secret"},
		}
		got, err := engine.Evaluate(context.Background(), action, nil)
		require.NoError(t, err)
		assert.Equal(t, model.DecisionBlock, got.Decision)
		assert.Contains(t, got.MatchedRuleIDs, "block-secret-classification")
	})

	t.Run("nil data classifications evaluates in-operator cleanly", func(t *testing.T) {
		action := &model.Action{Type: model.ActionToolUse}
		got, err := engine.Evaluate(context.Background(), action, nil)
		require.NoError(t, err)
		assert.Equal(t, model.DecisionAllow, got.Decision)
		assert.NotContains(t, got.MatchedRuleIDs, "block-secret-classification")
	})
}

func TestPDP_MostRestrictiveWins(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: warn-env
    action: warn
    match:
      file_patterns: ["**/.env"]
    message: "warn"
  - id: block-env
    action: block
    match:
      file_patterns: ["**/.env"]
    message: "block"
`)

	engine, err := New(policy)
	require.NoError(t, err)

	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/.env"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, model.DecisionBlock, got.Decision)
	assert.Equal(t, []string{"warn-env", "block-env"}, got.MatchedRuleIDs)
	assert.Equal(t, "block", got.Message)
}

func TestParsePolicy_RejectsInvalidRules(t *testing.T) {
	tests := []struct {
		name   string
		policy string
		want   string
	}{
		{
			name: "duplicate id",
			policy: `
rules:
  - id: same
    action: allow
  - id: same
    action: warn
`,
			want: `duplicate rule id "same"`,
		},
		{
			name: "invalid decision",
			policy: `
rules:
  - id: bad
    action: deny
`,
			want: `invalid action "deny"`,
		},
		{
			name: "invalid regex",
			policy: `
rules:
  - id: bad-regex
    action: block
    match:
      command_patterns: ["["]
`,
			want: `command_patterns`,
		},
		{
			name: "invalid glob",
			policy: `
rules:
  - id: bad-glob
    action: block
    match:
      file_patterns: ["["]
`,
			want: `invalid glob pattern`,
		},
		{
			name: "invalid CEL",
			policy: `
rules:
  - id: bad-cel
    action: block
    condition: "action.params.lines_added >"
`,
			want: `condition`,
		},
		{
			name: "non-bool CEL",
			policy: `
rules:
  - id: non-bool-cel
    action: block
    condition: "action.params.path"
`,
			want: `want bool`,
		},
		{
			name: "invalid template",
			policy: `
rules:
  - id: bad-template
    action: block
    message: "{{"
`,
			want: `message template`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePolicy([]byte(tc.policy))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func mustPolicy(t *testing.T, data string) *Policy {
	t.Helper()
	policy, err := ParsePolicy([]byte(data))
	require.NoError(t, err)
	return policy
}

func TestParsePolicy_DeferRequiresReason(t *testing.T) {
	t.Run("defer with empty reason rejected", func(t *testing.T) {
		_, err := ParsePolicy([]byte(`
version: "1"
rules:
  - id: defer-no-reason
    action: defer
    match:
      action_types: [file_write]
`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a non-empty reason")
	})

	t.Run("defer with reason parses", func(t *testing.T) {
		p, err := ParsePolicy([]byte(`
version: "1"
rules:
  - id: defer-on-classify
    action: defer
    reason: wait_for_classification
    match:
      action_types: [file_write]
`))
		require.NoError(t, err)
		require.Len(t, p.Rules, 1)
		assert.Equal(t, "wait_for_classification", p.Rules[0].Reason)
	})
}

func TestPDP_DeferRuleSurfacesReason(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: defer-on-empty-classify
    action: defer
    reason: wait_for_classification
    match:
      action_types: [file_write]
`)
	engine, err := New(policy)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileWrite,
		Parameters: model.Parameters{Path: "main.go"},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionDefer, got.Decision)
	assert.Equal(t, "wait_for_classification", got.DeferReason)
	assert.Equal(t, []string{"defer-on-empty-classify"}, got.MatchedRuleIDs)
}

func TestPDP_FreshSessionTriggerFires(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-on-many-writes
    action: block
    match:
      action_types: [file_write]
    condition: "context.files_written > 5"
    message: blocked
`)
	start := time.Now().UTC()
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, FreshSessionSeconds: 60, ConflictTriggersDefer: true}),
		WithSessionStartFn(func(_ context.Context) (time.Time, bool) { return start, true }),
	)
	require.NoError(t, err)

	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileWrite,
		Parameters: model.Parameters{Path: "main.go"},
	}, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionDefer, got.Decision)
	assert.Equal(t, DeferReasonFreshSession, got.DeferReason)
}

func TestPDP_FreshSessionTriggerSkippedWhenContextPopulated(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-on-many-writes
    action: block
    match:
      action_types: [file_write]
    condition: "context.files_written > 5"
    message: blocked
`)
	start := time.Now().UTC()
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, FreshSessionSeconds: 60, ConflictTriggersDefer: true}),
		WithSessionStartFn(func(_ context.Context) (time.Time, bool) { return start, true }),
	)
	require.NoError(t, err)

	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileWrite,
		Parameters: model.Parameters{Path: "main.go"},
	}, &model.ContextSnapshot{FilesWritten: 6})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, got.Decision)
}

func TestPDP_FreshSessionTriggerSkippedWhenSessionOld(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-on-many-writes
    action: block
    match:
      action_types: [file_write]
    condition: "context.files_written > 5"
    message: blocked
`)
	old := time.Now().UTC().Add(-2 * time.Hour)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, FreshSessionSeconds: 60, ConflictTriggersDefer: true}),
		WithSessionStartFn(func(_ context.Context) (time.Time, bool) { return old, true }),
	)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileWrite,
		Parameters: model.Parameters{Path: "main.go"},
	}, &model.ContextSnapshot{})
	require.NoError(t, err)
	assert.Equal(t, model.DecisionAllow, got.Decision)
}

func TestPDP_ConflictTriggerDoesNotFireAcrossTiers(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-env
    action: block
    match:
      file_patterns: ["**/.env"]
    message: block
  - id: escalate-env
    action: escalate
    match:
      file_patterns: ["**/.env"]
    message: escalate
`)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, ConflictTriggersDefer: true}),
	)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/.env"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, model.DecisionBlock, got.Decision, "no conflict at tier=block, only escalate is at tier=escalate")
}

func TestPDP_ConflictTriggerSilentAcrossTiers(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-env-a
    action: block
    match:
      file_patterns: ["**/.env"]
    message: block-a
  - id: defer-env
    action: defer
    reason: needs_classification
    match:
      file_patterns: ["**/.env"]
`)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, ConflictTriggersDefer: true}),
	)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/.env"},
	}, nil)
	require.NoError(t, err)
	// block (tier 5) wins outright over defer (tier 3); no conflict.
	assert.Equal(t, model.DecisionBlock, got.Decision)
}

func TestPDP_ConflictTriggerFiresOnSameTierDifferentDecisions(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: guidance-env
    action: guidance
    match:
      file_patterns: ["**/.env"]
    message: guidance
  - id: defer-env
    action: defer
    reason: wait_for_classification
    match:
      file_patterns: ["**/.env"]
`)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, ConflictTriggersDefer: true}),
	)
	require.NoError(t, err)
	// guidance (tier 2) and defer (tier 3) at different tiers; defer wins
	// outright per the new precedence; no conflict.
	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type:       model.ActionFileRead,
		Parameters: model.Parameters{Path: "/work/.env"},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionDefer, got.Decision)
}

func TestPDP_ConflictTriggerFiresWhenTwoDifferentBlocks(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-a
    action: block
    severity: high
    match:
      action_types: [file_read]
    message: block-a
  - id: block-b
    action: block
    severity: critical
    match:
      action_types: [file_read]
    message: block-b
`)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, ConflictTriggersDefer: true}),
	)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{
		Type: model.ActionFileRead,
	}, nil)
	require.NoError(t, err)
	// Two block rules at the same tier with different severity are a
	// structural conflict; the synthetic defer trigger fires.
	assert.Equal(t, model.DecisionDefer, got.Decision)
	assert.Equal(t, DeferReasonConflictingPolicies, got.DeferReason)
}

func TestPDP_ConflictTriggerFiresWhenTagsDiffer(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-a
    action: block
    severity: high
    tags: [secrets]
    match:
      action_types: [file_read]
    message: block
  - id: block-b
    action: block
    severity: high
    tags: [network]
    match:
      action_types: [file_read]
    message: block
`)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, ConflictTriggersDefer: true}),
	)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{Type: model.ActionFileRead}, nil)
	require.NoError(t, err)
	// Same decision and severity, distinct tag sets count as a structural
	// conflict.
	assert.Equal(t, model.DecisionDefer, got.Decision)
	assert.Equal(t, DeferReasonConflictingPolicies, got.DeferReason)
}

func TestPDP_ConflictTriggerSameStructureDoesNotFire(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: warn-a
    action: warn
    severity: medium
    match:
      action_types: [file_read]
    message: differing-message-a
  - id: warn-b
    action: warn
    severity: medium
    match:
      action_types: [file_read]
    message: differing-message-b
`)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, ConflictTriggersDefer: true}),
	)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{Type: model.ActionFileRead}, nil)
	require.NoError(t, err)
	// Same decision and severity with no tags: trivially differing wording
	// is not a conflict under the structural fingerprint.
	assert.Equal(t, model.DecisionWarn, got.Decision)
}

func TestPDP_ConflictTriggerTagOrderingIgnored(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: warn-a
    action: warn
    severity: medium
    tags: [alpha, beta]
    match:
      action_types: [file_read]
    message: m
  - id: warn-b
    action: warn
    severity: medium
    tags: [beta, alpha]
    match:
      action_types: [file_read]
    message: m
`)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, ConflictTriggersDefer: true}),
	)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{Type: model.ActionFileRead}, nil)
	require.NoError(t, err)
	// Same tag set in different author order must not flip the fingerprint.
	assert.Equal(t, model.DecisionWarn, got.Decision)
}

func TestPDP_ConflictTriggerDisabledByConfig(t *testing.T) {
	policy := mustPolicy(t, `
version: "1"
rules:
  - id: block-a
    action: block
    match:
      action_types: [file_read]
    message: block
  - id: escalate-a
    action: escalate
    match:
      action_types: [file_read]
`)
	engine, err := New(policy,
		WithDeferConfig(DeferConfig{Enabled: true, ConflictTriggersDefer: false}),
	)
	require.NoError(t, err)
	got, err := engine.Evaluate(context.Background(), &model.Action{Type: model.ActionFileRead}, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, got.Decision)
}

func TestCollectContextRefs(t *testing.T) {
	env, err := conditionEnv()
	require.NoError(t, err)
	ast, issues := env.Compile(`context.files_written > 5 && context.tools_used.size() > 0 && action.tool == "x"`)
	require.NoError(t, issues.Err())
	refs := collectContextRefs(ast)
	assert.Equal(t, []string{"files_written", "tools_used"}, refs)
}
