//go:build perf

package perf

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The client benchmarks send the hook to the decision service, as a
// managed host does. The managed configuration that turns the client on
// lives in /etc, so they need root on Linux and run only when
// GRYPH_PERF_CLIENT is set. The benchmark writes the managed directory for
// its own duration and refuses a host that already has one.
const (
	clientEnv      = "GRYPH_PERF_CLIENT"
	budgetEnv      = "GRYPH_PERF_BUDGET"
	defaultBudget  = 1.10
	managedDir     = "/etc/safedep/gryph"
	clientModeName = "client"
	localModeName  = "policy-on"
	serviceTimeout = 10 * time.Second
)

func clientEnabled() bool {
	return os.Getenv(clientEnv) != "" && runtime.GOOS == "linux" && os.Geteuid() == 0
}

// service is one decision service started for the client benchmarks.
type service struct {
	bin     string
	dir     string
	pidFile string
}

// startService writes the managed configuration, starts the service and
// waits for its handshake. The managed policy carries the same user rule
// as the local benchmarks, so both modes evaluate the same rules.
func startService(b *testing.B, bin string) *service {
	if _, err := os.Stat(managedDir); err == nil {
		b.Fatalf("%s exists: the client benchmarks need a host without a managed Gryph", managedDir)
	}
	dir, err := os.MkdirTemp("/tmp", "gryph-perf-")
	if err != nil {
		b.Fatal(err)
	}
	socket := filepath.Join(dir, "hook.sock")
	stateDir := filepath.Join(dir, "state")
	spoolDir := filepath.Join(dir, "spool")
	managed := fmt.Sprintf("policy:\n  enabled: true\n  fail_mode: closed\nsupervisor:\n  enabled: true\n  socket: %s\n  spool_dir: %s\n", socket, spoolDir)
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		b.Fatal(err)
	}
	s := &service{bin: bin, dir: dir, pidFile: filepath.Join(stateDir, "supervisor.pid")}
	b.Cleanup(s.stop)
	if err := os.WriteFile(filepath.Join(managedDir, "config.yml"), []byte(managed), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "policy.yaml"), []byte(userPolicy), 0o644); err != nil {
		b.Fatal(err)
	}
	run := exec.Command(bin, "supervisor", "run", "--socket", socket, "--state-dir", stateDir, "--spool-dir", spoolDir, "--allow-root")
	run.Stderr = os.Stderr
	if err := run.Start(); err != nil {
		b.Fatalf("start the service: %v", err)
	}
	go func() { _ = run.Wait() }()
	ready := exec.Command(bin, "supervisor", "ready", "--socket", socket, "--timeout", serviceTimeout.String())
	ready.Stderr = os.Stderr
	if err := ready.Run(); err != nil {
		b.Fatalf("the service does not answer: %v", err)
	}
	return s
}

// stop ends the service and removes the managed directory, so the host is
// as it was before the benchmark.
func (s *service) stop() {
	stop := exec.Command(s.bin, "supervisor", "stop", "--pid-file", s.pidFile)
	stop.Stderr = os.Stderr
	if err := stop.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "stop the service:", err)
	}
	_ = os.RemoveAll(managedDir)
	_ = os.Remove(filepath.Dir(managedDir))
	_ = os.RemoveAll(s.dir)
}

// checkBudget compares the client mode with the local policy-on mode of
// the same run. The p99 of every phase through the client stays within
// the local p99 times the budget. It prints both percentiles, so the
// report of a failure names the phase.
func checkBudget() error {
	resultsMu.Lock()
	defer resultsMu.Unlock()
	budget := defaultBudget
	if v := os.Getenv(budgetEnv); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", budgetEnv, err)
		}
		budget = f
	}
	var names []string
	for name := range results {
		if strings.HasSuffix(name, "/"+clientModeName) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	var over []string
	fmt.Fprintf(os.Stderr, "latency guard: client p99 within %.0f%% of local policy-on p99\n", budget*100)
	for _, name := range names {
		phase := strings.TrimSuffix(name, "/"+clientModeName)
		client := results[name]
		local, ok := results[phase+"/"+localModeName]
		if !ok {
			return fmt.Errorf("latency guard: no %s/%s result to compare with", phase, localModeName)
		}
		limit := time.Duration(float64(local.p99) * budget)
		verdict := "ok"
		if client.p99 > limit {
			verdict = "over"
			over = append(over, phase)
		}
		fmt.Fprintf(os.Stderr, "  %-14s client p95 %s p99 %s | local p95 %s p99 %s | limit %s | %s\n",
			phase, round(client.p95), round(client.p99), round(local.p95), round(local.p99), round(limit), verdict)
	}
	if len(over) > 0 {
		return fmt.Errorf("latency guard: the client p99 is over the budget for %s", strings.Join(over, ", "))
	}
	return nil
}
