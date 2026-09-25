package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
	"github.com/safedep/gryph/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderSources_DefaultIsCompact(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	require.NoError(t, os.WriteFile(policyPath, []byte("version: \"1\"\nrules: []\n"), 0o644))

	var buf bytes.Buffer
	renderSources(&buf, tui.NewColorizer(false), []loader.Source{
		loader.NewFileSource(policyPath),
		loader.NewOptionalFileSource(filepath.Join(dir, "missing.yaml")),
	}, false)

	out := buf.String()
	assert.Contains(t, out, "Policy sources\n")
	assert.Contains(t, out, "1  file")
	assert.Contains(t, out, policyPath)
	assert.Contains(t, out, "found")
	assert.Contains(t, out, "2  file")
	assert.Contains(t, out, "missing")
	assert.Contains(t, out, "optional")
	assert.NotContains(t, out, "evaluated in order")
	assert.NotContains(t, out, "Loads every")
}

func TestRenderSources_VerboseIncludesBuiltinHints(t *testing.T) {
	var buf bytes.Buffer
	renderSources(&buf, tui.NewColorizer(false), []loader.Source{
		loader.NewBuiltinSource("**/.claude/settings.json"),
	}, true)

	out := buf.String()
	assert.Contains(t, out, "Policy sources\n")
	assert.Contains(t, out, "builtin")
	assert.Contains(t, out, "Built-in rules protecting")
}

func TestSourceToRow_File(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	require.NoError(t, os.WriteFile(policyPath, []byte("version: \"1\"\nrules: []\n"), 0o644))

	row := sourceToRow(loader.NewFileSource(policyPath))
	assert.Equal(t, "file", row.Kind)
	assert.Equal(t, policyPath, row.Path)
	assert.Equal(t, sourceStatusFound, row.Status)
	assert.False(t, row.Problem)
}

func ruleIDs(p *pdp.Policy) []string {
	ids := make([]string, 0, len(p.Rules))
	for _, r := range p.Rules {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestBuildPolicyLoader_SingleGlobalFilePlusBuiltins(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "policy.yaml"),
		[]byte("version: \"1\"\nrules:\n  - id: user-rule\n    action: allow\n"), 0o644))
	cfg := config.Default()
	cfg.Policy.Enabled = true
	cfg.Policy.SelfProtection.Enabled = true
	paths := &config.Paths{ConfigDir: tmp}

	ldr := buildPolicyLoader(cfg, paths)
	policy, err := ldr.Load(context.Background())
	require.NoError(t, err)

	ids := ruleIDs(policy)
	assert.Contains(t, ids, "user-rule")
	assert.Contains(t, ids, "gryph-builtin-protected-files")
}

func TestBuildPolicyLoader_MissingFileBuiltinsOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Policy.Enabled = true
	cfg.Policy.SelfProtection.Enabled = true
	paths := &config.Paths{ConfigDir: tmp}

	ldr := buildPolicyLoader(cfg, paths)
	policy, err := ldr.Load(context.Background())
	require.NoError(t, err)

	ids := ruleIDs(policy)
	assert.NotContains(t, ids, "user-rule")
	assert.Contains(t, ids, "gryph-builtin-protected-files")
}

func TestLazyPolicyCheck_PassesSessionToMediator(t *testing.T) {
	tmp := t.TempDir()
	writePolicyFile(t, filepath.Join(tmp, "policy.yaml"), `version: "1"
rules:
  - id: block-on-project
    action: block
    scope:
      projects: [payments]
    match:
      action_types: [file_write]
    message: "blocked write on {{.Action.Project}}"
`)
	cfg := config.Default()
	cfg.Policy.Enabled = true
	check := newLazyPolicyCheck(cfg, &config.Paths{ConfigDir: tmp}, nil)

	sessID := uuid.New()
	event := &events.Event{
		ID:         uuid.New(),
		SessionID:  sessID,
		Timestamp:  time.Now(),
		ActionType: events.ActionFileWrite,
		AgentName:  "claude-code",
		Payload:    []byte(`{"path":"/work/app.go"}`),
	}

	tests := []struct {
		name     string
		sess     *session.Session
		decision coresecurity.Decision
	}{
		{"session in scoped project blocks", &session.Session{ID: sessID, ProjectName: "payments"}, coresecurity.DecisionBlock},
		{"session in other project allows", &session.Session{ID: sessID, ProjectName: "billing"}, coresecurity.DecisionAllow},
		{"nil session allows", nil, coresecurity.DecisionAllow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := check.Check(context.Background(), event, tc.sess)
			require.NoError(t, err)
			assert.Equal(t, tc.decision, res.Decision)
		})
	}
}

func TestSelfProtectionGlobs_StaticSetNoRepoLocal(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	paths := &config.Paths{ConfigDir: tmp}

	globs := selfProtectionGlobs(cfg, paths)

	assert.Contains(t, globs, filepath.ToSlash(tmp)+"/**")
	assert.Contains(t, globs, "**/.claude/settings.json")
	assert.NotContains(t, globs, "**/.gryph-policy.yml")
	assert.NotContains(t, globs, "**/.gryph-policy.yaml")
}

func TestSelfProtectionReadGlobs(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(tmp, "audit.db")
	paths := &config.Paths{ConfigDir: tmp}
	db := filepath.ToSlash(cfg.Storage.Path)

	globs := selfProtectionReadGlobs(cfg, paths)

	assert.Equal(t, []string{
		db, db + "-wal", db + "-shm", db + "-journal",
		filepath.ToSlash(filepath.Join(tmp, "keys", "receipt.key")),
	}, globs)
	assert.NotContains(t, globs, "**/.claude/settings.json")
	assert.Empty(t, selfProtectionReadGlobs(nil, paths))
}

func TestWriteExamplePolicy_WritesAndRefuses(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "sub", "policy.yaml")

	require.NoError(t, writeExamplePolicy(target, false))
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Gryph Security Policy")

	err = writeExamplePolicy(target, false)
	require.Error(t, err)

	require.NoError(t, writeExamplePolicy(target, true))
}

func TestPolicyFilePath_UsesConfigDir(t *testing.T) {
	tmp := t.TempDir()
	assert.Equal(t, filepath.Join(tmp, "policy.yaml"), config.DefaultPolicyFilePath(&config.Paths{ConfigDir: tmp}))
}

func TestResolveEditor_Precedence(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	assert.Equal(t, "vi", resolveEditor())

	t.Setenv("EDITOR", "nano")
	assert.Equal(t, "nano", resolveEditor())

	t.Setenv("VISUAL", "code -w")
	assert.Equal(t, "code -w", resolveEditor())
}

func TestNewClassifier_SkipsUnknownClass(t *testing.T) {
	cfg := config.Default()
	cfg.Policy.Classify.ExtraPatterns = map[string][]string{
		"secret":        {"**/*.vault"},
		"customer_data": {"**/customers.csv"},
	}

	h := newClassifier(cfg)
	require.NotNil(t, h)
	assert.Equal(t, []privacy.Class{privacy.ClassSecret}, h.ClassifyPaths([]string{"/work/a.vault"}, ""))
	assert.Empty(t, h.ClassifyPaths([]string{"/work/customers.csv"}, ""))

	cfg.Policy.Classify.Enabled = false
	assert.Nil(t, newClassifier(cfg))
}
