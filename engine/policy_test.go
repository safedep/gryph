package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/safedep/gryph/aarm/model"
	"github.com/safedep/gryph/aarm/pdp"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/core/events"
	"github.com/safedep/gryph/core/privacy"
	coresecurity "github.com/safedep/gryph/core/security"
	"github.com/safedep/gryph/core/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePolicyFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
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

	ldr := BuildPolicyLoader(cfg, paths)
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

	ldr := BuildPolicyLoader(cfg, paths)
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

	globs := SelfProtectionGlobs(cfg, paths)

	assert.Contains(t, globs, filepath.ToSlash(tmp)+"/**")
	assert.Contains(t, globs, "**/.claude/settings.json")
	assert.Contains(t, globs, filepath.ToSlash(cfg.ExportKeyFile()))
	assert.NotContains(t, globs, "**/.gryph-policy.yml")
	assert.NotContains(t, globs, "**/.gryph-policy.yaml")
}

func TestSelfProtectionReadGlobs(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Storage.Path = filepath.Join(tmp, "audit.db")
	paths := &config.Paths{ConfigDir: tmp}
	db := filepath.ToSlash(cfg.Storage.Path)

	globs := SelfProtectionReadGlobs(cfg, paths)

	assert.Equal(t, []string{
		db, db + "-wal", db + "-shm", db + "-journal",
		filepath.ToSlash(filepath.Join(tmp, "keys", "receipt.key")),
		filepath.ToSlash(filepath.Join(tmp, "export.key")),
	}, globs)
	assert.NotContains(t, globs, "**/.claude/settings.json")
	assert.Empty(t, SelfProtectionReadGlobs(nil, paths))
}

func TestNewClassifier_KeepsCustomLabel(t *testing.T) {
	cfg := config.Default()
	cfg.Policy.Classify.ExtraPatterns = map[string][]string{
		"secret":        {"**/*.vault"},
		"customer_data": {"**/customers/**"},
	}

	h := NewClassifier(cfg)
	require.NotNil(t, h)
	assert.Equal(t, []privacy.Class{privacy.ClassSecret}, h.ClassifyPaths([]string{"/work/a.vault"}, ""))

	action := &model.Action{Type: model.ActionFileRead, Parameters: model.Parameters{Path: "/work/customers/list.txt"}}
	assert.Contains(t, h.Classify(action), privacy.Class("customer_data"))

	event := events.NewEvent(uuid.New(), "claude-code", events.ActionFileRead)
	require.NoError(t, event.SetPayload(&events.FileReadPayload{Path: "/work/customers/list.txt"}))
	assert.Equal(t, []privacy.Class{privacy.ClassPII}, h.ClassifyEvent(event))

	cfg.Policy.Classify.Enabled = false
	assert.Nil(t, NewClassifier(cfg))
}
