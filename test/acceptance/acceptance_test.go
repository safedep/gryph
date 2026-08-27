//go:build acceptance

package acceptance

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/stretchr/testify/require"
)

// TestAcceptance runs the real gryph binary through testscript scripts. Each
// script isolates itself with HOME, XDG_CONFIG_HOME, and XDG_DATA_HOME under
// its own $WORK directory. Nothing touches the host system.
func TestAcceptance(t *testing.T) {
	bin := os.Getenv("GRYPH_BIN")
	if bin == "" {
		bin = filepath.Join(t.TempDir(), "gryph")
		build := exec.Command("go", "build", "-o", bin, "../../cmd/gryph")
		build.Stderr = os.Stderr
		require.NoError(t, build.Run(), "build gryph for acceptance run")
	}
	// testscript resolves exec targets via PATH from each script's cwd, so a
	// relative GRYPH_BIN would never resolve. Anchor it to an absolute path.
	bin, err := filepath.Abs(bin)
	require.NoError(t, err)
	binDir := filepath.Dir(bin)

	cat, err := LoadCatalog("catalog.yaml")
	require.NoError(t, err)
	sel := selectorFromEnv()

	const root = "scripts"
	files, err := discoverScriptFiles(root)
	require.NoError(t, err)
	require.NotEmptyf(t, files, "no acceptance scripts found under %s", root)

	// Group the selected scripts by directory. testscript names each subtest
	// by the script's base name. Wrapping a directory in t.Run(relDir)
	// rebuilds the full path-derived feature id in the test name.
	byDir := map[string][]string{}
	dirs := []string{}
	for _, f := range files {
		if !cat.Selects(f.id, sel) {
			continue
		}
		if _, seen := byDir[f.relDir]; !seen {
			dirs = append(dirs, f.relDir)
		}
		byDir[f.relDir] = append(byDir[f.relDir], f.path)
	}
	if len(byDir) == 0 {
		t.Skipf("no acceptance scripts match selector %+v", sel)
	}
	sort.Strings(dirs)

	for _, relDir := range dirs {
		relDir, scripts := relDir, byDir[relDir]
		t.Run(relDir, func(t *testing.T) {
			testscript.Run(t, testscript.Params{
				Files: scripts,
				Setup: func(env *testscript.Env) error {
					env.Setenv("PATH", binDir+string(os.PathListSeparator)+env.Getenv("PATH"))
					// The status and doctor commands run an async update check
					// against the GitHub API. Forward proxy and TLS settings so
					// the check works in proxied environments. The check fails
					// soft, so no network never fails a script.
					forwardHostEnv(env,
						"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
						"http_proxy", "https_proxy", "no_proxy",
						"SSL_CERT_FILE", "SSL_CERT_DIR")
					return nil
				},
				Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
					"execexit":  cmdExecExit,
					"expandenv": cmdExpandEnv,
					"replace":   cmdReplace,
				},
			})
		})
	}
}

// cmdExecExit runs a command and asserts its exact exit code:
//
//	execexit 2 gryph _hook claude-code PreToolUse
//
// The plain `exec` builtin only distinguishes zero from non-zero. Exit codes
// are a user-facing contract (see cli/errors.go), so scripts assert them
// exactly.
func cmdExecExit(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("execexit does not support negation. Assert the expected code instead")
	}
	if len(args) < 2 {
		ts.Fatalf("usage: execexit <code> <command> [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("execexit: bad exit code %q: %v", args[0], err)
	}

	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			ts.Fatalf("execexit: %s did not run: %v", args[1], err)
		}
		got = ee.ExitCode()
	}
	if got != want {
		ts.Fatalf("execexit: %s exited %d, want %d", strings.Join(args[1:], " "), got, want)
	}
}

// cmdExpandEnv rewrites ${VAR} references in the given files with values from
// the script environment. txtar file sections are static, so this is how a
// fixture embeds a sandbox-absolute path:
//
//	expandenv payload.json
func cmdExpandEnv(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("expandenv does not support negation")
	}
	if len(args) == 0 {
		ts.Fatalf("usage: expandenv <file>...")
	}
	for _, name := range args {
		path := ts.MkAbs(name)
		data, err := os.ReadFile(path)
		ts.Check(err)
		expanded := os.Expand(string(data), func(key string) string {
			return ts.Getenv(key)
		})
		ts.Check(os.WriteFile(path, []byte(expanded), 0o600))
	}
}

// cmdReplace substitutes every occurrence of a literal string in a file:
//
//	replace receipts.jsonl old new
//
// Tamper cases use it to alter one exported field before verification.
func cmdReplace(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("replace does not support negation")
	}
	if len(args) != 3 {
		ts.Fatalf("usage: replace <file> <old> <new>")
	}
	path := ts.MkAbs(args[0])
	data, err := os.ReadFile(path)
	ts.Check(err)
	if !strings.Contains(string(data), args[1]) {
		ts.Fatalf("replace: %s does not contain %q", args[0], args[1])
	}
	ts.Check(os.WriteFile(path, []byte(strings.ReplaceAll(string(data), args[1], args[2])), 0o600))
}

type scriptFile struct {
	path   string // path to the .txtar, relative to the working directory
	relDir string // directory relative to the scripts root, "/"-separated
	id     string // path-derived feature id
}

func discoverScriptFiles(root string) ([]scriptFile, error) {
	var out []scriptFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".txtar" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, scriptFile{
			path:   path,
			relDir: filepath.ToSlash(filepath.Dir(rel)),
			id:     DeriveFeatureID(rel),
		})
		return nil
	})
	return out, err
}

// selectorFromEnv reads the optional category and label filters. CI passes
// them as environment variables, never as shell arguments, so a dispatch
// input cannot inject into a command.
func selectorFromEnv() Selector {
	sel := Selector{Category: strings.TrimSpace(os.Getenv("ACCEPTANCE_CATEGORY"))}
	for _, l := range strings.Split(os.Getenv("ACCEPTANCE_LABELS"), ",") {
		if l = strings.TrimSpace(l); l != "" {
			sel.Labels = append(sel.Labels, l)
		}
	}
	return sel
}

func forwardHostEnv(env *testscript.Env, keys ...string) {
	for _, key := range keys {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			env.Setenv(key, v)
		}
	}
}
