package fanotify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatcher needs CAP_SYS_ADMIN and an account to run the opener as.
// Without them it skips.
func TestWatcher(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}
	if _, err := exec.LookPath("runuser"); err != nil {
		t.Skip("needs runuser")
	}
	// The opener runs as nobody, so the directory must be reachable by
	// every account: t.TempDir sits under a 0700 parent.
	dir, err := os.MkdirTemp("/tmp", "gryph-fanotify-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	require.NoError(t, os.Chmod(dir, 0o777))
	file := filepath.Join(dir, "policy.yaml")
	require.NoError(t, os.WriteFile(file, []byte("version: 1\n"), 0o666))
	// The umask narrows the mode of a new file. Every account must be able
	// to write by the access checks, so only the watcher stops the write.
	require.NoError(t, os.Chmod(file, 0o666))

	w, err := Open(Options{Files: []string{file}, Dirs: []string{dir}})
	require.NoError(t, err)
	defer func() { _ = w.Close() }()

	var (
		mu      sync.Mutex
		reqs    []Request
		changes []Change
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Serve(ctx, func(r Request) bool {
			mu.Lock()
			reqs = append(reqs, r)
			mu.Unlock()
			return Decide(nil, r)
		}, func(c Change) {
			mu.Lock()
			changes = append(changes, c)
			mu.Unlock()
		})
	}()

	run := func(user, script string) error {
		cmd := exec.Command("runuser", "-u", user, "--", "sh", "-c", script)
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	assert.NoError(t, run("nobody", "cat "+file+" >/dev/null"), "a read passes")
	assert.Error(t, run("nobody", "echo x >> "+file), "a write is denied")
	assert.Error(t, run("nobody", ": > "+filepath.Join(dir, "new.yaml")), "a create in the directory is denied")
	assert.NoError(t, exec.Command("sh", "-c", "echo y >> "+file).Run(), "root writes")
	require.NoError(t, os.Rename(file, file+".bak"))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		moved := false
		for _, c := range changes {
			moved = moved || c.Op == "moved"
		}
		mu.Unlock()
		if moved {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	require.NoError(t, <-done)

	mu.Lock()
	defer mu.Unlock()
	var denied, allowed int
	for _, r := range reqs {
		if Decide(nil, r) {
			allowed++
		} else {
			denied++
		}
	}
	assert.GreaterOrEqual(t, denied, 2)
	assert.GreaterOrEqual(t, allowed, 2)
	var ops []string
	for _, c := range changes {
		ops = append(ops, c.Op)
	}
	assert.Contains(t, ops, "modify")
	assert.Contains(t, ops, "moved")
}
