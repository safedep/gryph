//go:build perf

// Package perf measures the latency of the hook path through the real gryph
// binary, as an agent sees it: one process per hook, payload on stdin,
// decision on exit. Run it with:
//
//	go test -tags perf -run '^$' -bench . ./test/perf/
//
// Set GRYPH_PERF_REPORT to a file path to get a Markdown report with the
// percentiles of each benchmark.
package perf

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// phases are the hook kinds of the fail-mode matrix, each with a Claude
// Code fixture. A blocking pre hook is the one that matters for the agent,
// so it comes first.
var phases = []struct {
	name     string
	hookType string
	fixture  string
}{
	{"pre-blocking", "PreToolUse", "pre_tool_use_bash.json"},
	{"prompt", "UserPromptSubmit", "user_prompt_submit.json"},
	{"post", "PostToolUse", "post_tool_use_read.json"},
	{"lifecycle", "SessionStart", "session_start.json"},
}

// policyModes are the two configurations an install runs in. Policy on
// loads the built-in rules and one user rule, so the PDP and the receipt
// chain are on the path.
var policyModes = []struct {
	name   string
	config string
}{
	{"policy-off", "policy:\n  enabled: false\n"},
	{"policy-on", "policy:\n  enabled: true\n  fail_mode: closed\n"},
}

const userPolicy = `version: "1"
rules:
  - id: perf-warn-large-edit
    action: warn
    match:
      action_types: [file_write]
    condition: action.params.lines_added > 200
`

var (
	binOnce sync.Once
	binPath string
	binErr  error

	// results keeps the last run of each benchmark. The runner calls a
	// benchmark more than once while it finds b.N, and only the last run
	// has the requested count.
	resultsMu sync.Mutex
	results   = map[string]result{}
)

type result struct {
	name          string
	n             int
	p50, p95, p99 time.Duration
}

func TestMain(m *testing.M) {
	code := m.Run()
	if path := os.Getenv("GRYPH_PERF_REPORT"); path != "" && code == 0 {
		if err := writeReport(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

func gryphBinary(b *testing.B) string {
	binOnce.Do(func() {
		if env := os.Getenv("GRYPH_BIN"); env != "" {
			binPath, binErr = filepath.Abs(env)
			return
		}
		dir, err := os.MkdirTemp("", "gryph-perf")
		if err != nil {
			binErr = err
			return
		}
		binPath = filepath.Join(dir, "gryph")
		if runtime.GOOS == "windows" {
			binPath += ".exe"
		}
		build := exec.Command("go", "build", "-o", binPath, "../../cmd/gryph")
		build.Stderr = os.Stderr
		binErr = build.Run()
	})
	if binErr != nil {
		b.Fatalf("build gryph: %v", binErr)
	}
	return binPath
}

// sandbox gives one benchmark its own home, config and policy, so runs do
// not share a database.
func sandbox(b *testing.B, policyConfig string) []string {
	home := b.TempDir()
	configDir := filepath.Join(home, ".config", "safedep", "gryph")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		b.Fatal(err)
	}
	config := "logging:\n  level: full\ndisplay:\n  colors: never\n" + policyConfig
	if err := os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(config), 0o600); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "policy.yaml"), []byte(userPolicy), 0o600); err != nil {
		b.Fatal(err)
	}
	return []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"PATH=" + os.Getenv("PATH"),
	}
}

func fixture(b *testing.B, name string) []byte {
	data, err := os.ReadFile(filepath.Join("..", "..", "agent", "claudecode", "testdata", name))
	if err != nil {
		b.Fatal(err)
	}
	return data
}

func BenchmarkHook(b *testing.B) {
	bin := gryphBinary(b)
	for _, mode := range policyModes {
		for _, phase := range phases {
			name := phase.name + "/" + mode.name
			b.Run(name, func(b *testing.B) {
				env := sandbox(b, mode.config)
				payload := fixture(b, phase.fixture)
				// One warm run creates the database and the session, so the
				// measured runs see a steady state.
				runHook(b, bin, env, phase.hookType, payload)

				durations := make([]time.Duration, 0, b.N)
				b.ResetTimer()
				for range b.N {
					start := time.Now()
					runHook(b, bin, env, phase.hookType, payload)
					durations = append(durations, time.Since(start))
				}
				b.StopTimer()
				record(b, name, durations)
			})
		}
	}
}

func runHook(b *testing.B, bin string, env []string, hookType string, payload []byte) {
	cmd := exec.Command(bin, "_hook", "claude-code", hookType)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		b.Fatalf("gryph _hook %s: %v\n%s", hookType, err, stderr.String())
	}
}

func record(b *testing.B, name string, durations []time.Duration) {
	slices.Sort(durations)
	r := result{
		name: name,
		n:    len(durations),
		p50:  percentile(durations, 50),
		p95:  percentile(durations, 95),
		p99:  percentile(durations, 99),
	}
	b.ReportMetric(float64(r.p50.Nanoseconds()), "p50-ns")
	b.ReportMetric(float64(r.p95.Nanoseconds()), "p95-ns")
	b.ReportMetric(float64(r.p99.Nanoseconds()), "p99-ns")

	resultsMu.Lock()
	defer resultsMu.Unlock()
	results[name] = r
}

// percentile is the nearest-rank percentile of a sorted sample.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

func writeReport(path string) error {
	resultsMu.Lock()
	defer resultsMu.Unlock()
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Hook latency\n\n")
	fmt.Fprintf(&sb, "Measured through the real `gryph` binary, one process per hook, on %s/%s with %d CPUs, Go %s, at %s.\n\n",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.Version(), time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "| Benchmark | Runs | p50 | p95 | p99 |\n|---|---|---|---|---|\n")
	for _, name := range names {
		r := results[name]
		fmt.Fprintf(&sb, "| %s | %d | %s | %s | %s |\n", r.name, r.n, round(r.p50), round(r.p95), round(r.p99))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func round(d time.Duration) string {
	return d.Round(100 * time.Microsecond).String()
}
