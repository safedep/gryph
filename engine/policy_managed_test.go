package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/aarm/loader"
	"github.com/safedep/gryph/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	userRule    = "version: \"1\"\nrules:\n  - id: user-rule\n    action: allow\n"
	managedRule = "version: \"1\"\nrules:\n  - id: managed-rule\n    action: block\n    match:\n      action_types: [command_exec]\n      command_patterns: [\"rm -rf\"]\n"
	managedDir  = "version: \"1\"\nrules:\n  - id: managed-dir-rule\n    action: warn\n"
)

func trustAll(string) error { return nil }

func managedFixture(t *testing.T, active bool, trust func(string) error) (config.ManagedPolicy, *config.Paths) {
	t.Helper()
	sys := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sys, "policy.yaml"), []byte(managedRule), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(sys, "policies"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sys, "policies", "a.yaml"), []byte(managedDir), 0o644))
	user := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(user, "policy.yaml"), []byte(userRule), 0o644))
	managed := config.ManagedPolicy{Active: active, File: filepath.Join(sys, "policy.yaml"), Dir: filepath.Join(sys, "policies"), Trust: trust}
	return managed, &config.Paths{ConfigDir: user}
}

func policyCfg(allowUser bool) *config.Config {
	cfg := config.Default()
	cfg.Policy.Enabled = true
	cfg.Policy.SelfProtection.Enabled = true
	cfg.Policy.AllowUserPolicy = allowUser
	return cfg
}

func TestPolicySources_ManagedAndUser(t *testing.T) {
	managed, paths := managedFixture(t, true, trustAll)
	sources := policySources(policyCfg(true), paths, managed)
	require.Len(t, sources, 5)
	assert.Equal(t, loader.ScopeManaged, loader.ScopeOf(sources[0]))
	assert.Equal(t, "managed-file:"+managed.File, sources[0].Name())
	assert.Equal(t, "managed-dir:"+managed.Dir, sources[1].Name())
	assert.Equal(t, loader.ScopeUser, loader.ScopeOf(sources[2]))
	assert.Equal(t, loader.ScopeBuiltin, loader.ScopeOf(sources[4]))

	policy, err := loader.New(sources...).Load(context.Background())
	require.NoError(t, err)
	ids := ruleIDs(policy)
	assert.Contains(t, ids, "managed-rule")
	assert.Contains(t, ids, "managed-dir-rule")
	assert.Contains(t, ids, "user-rule")
	assert.Contains(t, ids, "gryph-builtin-protected-files")
}

func TestPolicySources_UserPolicyDropped(t *testing.T) {
	managed, paths := managedFixture(t, true, trustAll)
	sources := policySources(policyCfg(false), paths, managed)
	require.Len(t, sources, 3, "managed file, managed dir, builtin")
	policy, err := loader.New(sources...).Load(context.Background())
	require.NoError(t, err)
	ids := ruleIDs(policy)
	assert.Contains(t, ids, "managed-rule")
	assert.NotContains(t, ids, "user-rule")

	inactive, _ := managedFixture(t, false, trustAll)
	assert.Len(t, policySources(policyCfg(false), paths, inactive), 5, "allow_user_policy has no effect without a managed config in force")
}

func TestPolicySources_UntrustedManagedFileFails(t *testing.T) {
	managed, paths := managedFixture(t, true, func(path string) error {
		if filepath.Base(path) == "policy.yaml" {
			return errors.New("not owned by root")
		}
		return nil
	})
	_, err := loader.New(policySources(policyCfg(true), paths, managed)...).Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not trusted")
	assert.Contains(t, err.Error(), "not owned by root")
}

func TestPolicySources_NoManagedLocation(t *testing.T) {
	_, paths := managedFixture(t, false, trustAll)
	sources := policySources(policyCfg(true), paths, config.ManagedPolicy{})
	require.Len(t, sources, 3, "user file, user dir, builtin")
	assert.Equal(t, loader.ScopeUser, loader.ScopeOf(sources[0]))
}

func TestPolicySources_MissingManagedFilesAreOptional(t *testing.T) {
	_, paths := managedFixture(t, false, trustAll)
	sys := t.TempDir()
	managed := config.ManagedPolicy{File: filepath.Join(sys, "policy.yaml"), Dir: filepath.Join(sys, "policies"), Trust: func(string) error {
		return errors.New("the trust check must not run on a missing file")
	}}
	policy, err := loader.New(policySources(policyCfg(true), paths, managed)...).Load(context.Background())
	require.NoError(t, err)
	assert.Contains(t, ruleIDs(policy), "user-rule")
}
