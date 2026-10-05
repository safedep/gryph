package landlock

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The restricted part runs in a child process, because a domain cannot be
// lifted from the process that took it.
const childEnv = "GRYPH_LANDLOCK_TEST_CHILD"

func TestMain(m *testing.M) {
	if dir := os.Getenv(childEnv); dir != "" {
		os.Exit(child(dir))
	}
	os.Exit(m.Run())
}

func child(dir string) int {
	protected := filepath.Join(dir, "protected")
	if err := Restrict(Options{ReadOnly: []string{protected}}); err != nil {
		_, _ = os.Stderr.WriteString("restrict: " + err.Error() + "\n")
		return 2
	}
	code := 0
	check := func(name string, err error, wantErr bool) {
		if (err != nil) != wantErr {
			_, _ = os.Stderr.WriteString(name + ": unexpected outcome: " + errString(err) + "\n")
			code = 1
		}
	}
	_, err := os.ReadFile(filepath.Join(protected, "policy.yaml"))
	check("read protected", err, false)
	check("write protected", os.WriteFile(filepath.Join(protected, "policy.yaml"), []byte("x"), 0o644), true)
	check("create in protected", os.WriteFile(filepath.Join(protected, "new"), []byte("x"), 0o644), true)
	check("remove in protected", os.Remove(filepath.Join(protected, "policy.yaml")), true)
	check("rename protected", os.Rename(protected, protected+".bak"), true)
	check("write sibling", os.WriteFile(filepath.Join(dir, "sibling", "f"), []byte("x"), 0o644), false)
	check("create in ancestor", os.WriteFile(filepath.Join(dir, "new"), []byte("x"), 0o644), true)
	// The temporary directory is an ancestor of the protected path, so a
	// new entry there is refused too: the kernel grants a right to a whole
	// directory or not at all.
	check("create in tmp", os.WriteFile(filepath.Join(os.TempDir(), "gryph-landlock-child"), []byte("x"), 0o644), true)
	return code
}

func errString(err error) string {
	if err == nil {
		return "no error"
	}
	return err.Error()
}

func TestRestrict(t *testing.T) {
	if _, err := ABI(); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "protected"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sibling"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "protected", "policy.yaml"), []byte("version: 1\n"), 0o644))

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), childEnv+"="+dir)
	out, err := cmd.CombinedOutput()
	assert.NoError(t, err, string(out))
	assert.Equal(t, "version: 1\n", readFile(t, filepath.Join(dir, "protected", "policy.yaml")))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestAncestors(t *testing.T) {
	dir := t.TempDir()
	protected := filepath.Join(dir, "a", "b")
	require.NoError(t, os.MkdirAll(protected, 0o755))
	got := Ancestors(Options{ReadOnly: []string{protected}})
	assert.Contains(t, got, filepath.Join(dir, "a"))
	assert.Contains(t, got, "/")
	assert.NotContains(t, got, protected)
}
