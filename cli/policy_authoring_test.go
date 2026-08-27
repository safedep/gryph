package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runPolicyCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	err := cmd.Execute()
	return buf.String(), err
}

func writePolicyFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func gryphConfigDir(root string) string { return filepath.Join(root, "gryph") }

func TestResolvePolicyTarget(t *testing.T) {
	paths := &config.Paths{ConfigDir: "/cfg"}
	cases := []struct {
		name     string
		args     []string
		wantPath string
		wantKind policyTargetKind
	}{
		{"no arg targets global", nil, "/cfg/policy.yaml", policyTargetGlobal},
		{"bare name normalizes extension", []string{"guard"}, "/cfg/policies/guard.yaml", policyTargetNamed},
		{"bare name keeps yaml", []string{"guard.yaml"}, "/cfg/policies/guard.yaml", policyTargetNamed},
		{"bare name keeps yml", []string{"guard.yml"}, "/cfg/policies/guard.yml", policyTargetNamed},
		{"dot path is literal", []string{"./x.yml"}, "./x.yml", policyTargetPath},
		{"separator path is literal", []string{"sub/x.yaml"}, "sub/x.yaml", policyTargetPath},
		{"absolute path is literal", []string{"/tmp/x.yaml"}, "/tmp/x.yaml", policyTargetPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvePolicyTarget(paths, tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPath, got.path)
			assert.Equal(t, tc.wantKind, got.kind)
		})
	}
}

func TestResolvePolicyTarget_TildeExpands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := resolvePolicyTarget(&config.Paths{ConfigDir: "/cfg"}, []string{"~/x.yml"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "x.yml"), got.path)
	assert.Equal(t, policyTargetPath, got.kind)
}

func TestPolicyInit_Forms(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	cfgDir := gryphConfigDir(root)

	_, err := runPolicyCmd(t, newPolicyInitCmd())
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(cfgDir, "policy.yaml"))

	_, err = runPolicyCmd(t, newPolicyInitCmd(), "agent-guardrails")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(cfgDir, "policies", "agent-guardrails.yaml"))

	cand := filepath.Join(t.TempDir(), "cand.yml")
	out, err := runPolicyCmd(t, newPolicyInitCmd(), cand)
	require.NoError(t, err)
	assert.Contains(t, out, "candidate")
	assert.FileExists(t, cand)
}

func TestPolicyInit_RefusesOverwriteWithoutForce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	_, err := runPolicyCmd(t, newPolicyInitCmd(), "guard")
	require.NoError(t, err)
	_, err = runPolicyCmd(t, newPolicyInitCmd(), "guard")
	require.Error(t, err)
	_, err = runPolicyCmd(t, newPolicyInitCmd(), "guard", "--force")
	require.NoError(t, err)
}

func TestPolicyEdit_NameValidatesMerged(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")

	out, err := runPolicyCmd(t, newPolicyEditCmd(), "guard")
	require.NoError(t, err)
	assert.Contains(t, out, "Policy valid")
	assert.FileExists(t, filepath.Join(gryphConfigDir(root), "policies", "guard.yaml"))
}

func TestPolicyEdit_PathValidatesFileAlone(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true")

	cand := filepath.Join(t.TempDir(), "cand.yml")
	out, err := runPolicyCmd(t, newPolicyEditCmd(), cand)
	require.NoError(t, err)
	assert.Contains(t, out, "File valid")
}

func TestPolicyValidateFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	good := filepath.Join(t.TempDir(), "good.yaml")
	writePolicyFile(t, good, "version: \"1\"\nrules:\n  - id: ok\n    action: allow\n")
	out, err := runPolicyCmd(t, newPolicyValidateCmd(), "--file", good)
	require.NoError(t, err)
	assert.Contains(t, out, "File valid")

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	writePolicyFile(t, bad, "rules:\n  - id: \"\"\n    action: allow\n")
	_, err = runPolicyCmd(t, newPolicyValidateCmd(), "--file", bad)
	require.Error(t, err)
}

func TestPolicyInstall_LifecycleAndList(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	cfgDir := gryphConfigDir(root)
	dest := filepath.Join(cfgDir, "policies", "no-prod.yaml")

	cand := filepath.Join(t.TempDir(), "cand.yaml")
	writePolicyFile(t, cand, "version: \"1\"\nrules:\n  - id: team-no-prod\n    action: block\n")

	out, err := runPolicyCmd(t, newPolicyInstallCmd(), cand, "--name", "no-prod", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "Would install")
	assert.NoFileExists(t, dest)

	out, err = runPolicyCmd(t, newPolicyInstallCmd(), cand, "--name", "no-prod")
	require.NoError(t, err)
	assert.Contains(t, out, "Installed to")
	assert.FileExists(t, dest)

	_, err = runPolicyCmd(t, newPolicyInstallCmd(), cand, "--name", "no-prod")
	require.Error(t, err)

	// force re-install of the same rule IDs must not collide with the file it replaces
	_, err = runPolicyCmd(t, newPolicyInstallCmd(), cand, "--name", "no-prod", "--force")
	require.NoError(t, err)

	out, err = runPolicyCmd(t, newPolicyListCmd())
	require.NoError(t, err)
	assert.Contains(t, out, "no-prod.yaml")
	assert.Contains(t, out, "rules merged.")
}

func TestPolicyInstall_RefusesMergeConflict(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	writePolicyFile(t, filepath.Join(gryphConfigDir(root), "policy.yaml"),
		"version: \"1\"\nrules:\n  - id: dup\n    action: allow\n")

	cand := filepath.Join(t.TempDir(), "cand.yaml")
	writePolicyFile(t, cand, "version: \"1\"\nrules:\n  - id: dup\n    action: block\n")

	_, err := runPolicyCmd(t, newPolicyInstallCmd(), cand)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts")
}

func TestInstallDestName(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		flag    string
		want    string
		wantErr bool
	}{
		{"basename with yaml", "/tmp/cand.yaml", "", "cand.yaml", false},
		{"basename without extension normalizes", "/tmp/cand", "", "cand.yaml", false},
		{"name flag normalizes", "/tmp/cand.yaml", "guard", "guard.yaml", false},
		{"name flag keeps yml", "/tmp/cand.yaml", "guard.yml", "guard.yml", false},
		{"name with separator rejected", "/tmp/cand.yaml", "sub/guard", "", true},
		{"name with parent rejected", "/tmp/cand.yaml", "../guard", "", true},
		{"name dotdot rejected", "/tmp/cand.yaml", "..", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := installDestName(tc.src, tc.flag)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPolicyInstall_RejectsPathName(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	cand := filepath.Join(t.TempDir(), "cand.yaml")
	writePolicyFile(t, cand, "version: \"1\"\nrules:\n  - id: r\n    action: allow\n")

	_, err := runPolicyCmd(t, newPolicyInstallCmd(), cand, "--name", "sub/guard")
	require.Error(t, err)
}

func TestPolicyInit_WarnsOnMergedConflict(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	_, err := runPolicyCmd(t, newPolicyInitCmd())
	require.NoError(t, err)

	out, err := runPolicyCmd(t, newPolicyInitCmd(), "guard")
	require.NoError(t, err)
	assert.Contains(t, out, "Warning")
	assert.Contains(t, out, "duplicate rule id")
}

func TestPolicyTest_FileFlag(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	draft := filepath.Join(t.TempDir(), "draft.yaml")
	writePolicyFile(t, draft, "version: \"1\"\nrules:\n  - id: draft-no-prod\n    action: block\n    match:\n      action_types: [file_write]\n      file_patterns: [\"**/prod/**\"]\n")

	out, err := runPolicyCmd(t, newPolicyTestCmd(), "--file", draft, "--action", "file_write", "--path", "/app/prod/config.yaml")
	require.NoError(t, err)
	assert.Contains(t, out, "BLOCK")
	assert.Contains(t, out, "draft-no-prod")

	out, err = runPolicyCmd(t, newPolicyTestCmd(), "--file", draft, "--action", "file_write", "--path", "/app/dev/config.yaml")
	require.NoError(t, err)
	assert.Contains(t, out, "ALLOW")

	_, err = runPolicyCmd(t, newPolicyTestCmd(), "--file", filepath.Join(t.TempDir(), "absent.yaml"), "--action", "file_write", "--path", "/x")
	require.Error(t, err)
}

func TestPolicyList_EmptyHostShowsBuiltinOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	out, err := runPolicyCmd(t, newPolicyListCmd())
	require.NoError(t, err)
	assert.Contains(t, out, "builtin")
	assert.Contains(t, out, "Only built-in rules are active")
}

func TestPolicyList_BrokenFileDoesNotHideOthers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	pdir := filepath.Join(gryphConfigDir(root), "policies")
	writePolicyFile(t, filepath.Join(pdir, "good.yaml"), "version: \"1\"\nrules:\n  - id: g\n    action: allow\n")
	writePolicyFile(t, filepath.Join(pdir, "bad.yaml"), "rules:\n  - id: \"\"\n    action: allow\n")

	out, err := runPolicyCmd(t, newPolicyListCmd())
	require.NoError(t, err)
	assert.Contains(t, out, "good.yaml")
	assert.Contains(t, out, "bad.yaml")
	assert.Contains(t, out, "!")
}
