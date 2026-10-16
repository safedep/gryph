//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The keys go into the state directory, so the install refuses a state
// directory whose chain a user controls, or that another account owns.
// The checks need root, which owns the chain and can give a directory away.
func TestEnsureStateDir_RefusesAUserChain(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the chain check needs a root-owned chain")
	}
	base, err := os.MkdirTemp("/tmp", "gryph-state-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	require.NoError(t, os.Chmod(base, 0o755))

	require.NoError(t, ensureStateDir(filepath.Join(base, "root-owned", "state")))

	planted := filepath.Join(base, "planted")
	require.NoError(t, os.Mkdir(planted, 0o755))
	require.NoError(t, os.Chown(planted, 4242, 4242))
	assert.ErrorContains(t, ensureStateDir(filepath.Join(planted, "state")), "not owned by root")
}

func TestEnsureSocketDir_RefusesAUserChain(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the chain check needs a root-owned chain")
	}
	base, err := os.MkdirTemp("/tmp", "gryph-socket-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	require.NoError(t, os.Chmod(base, 0o755))

	require.NoError(t, ensureSocketDir(filepath.Join(base, "run", "hook.sock")))

	planted := filepath.Join(base, "planted")
	require.NoError(t, os.Mkdir(planted, 0o777))
	require.NoError(t, os.Chmod(planted, 0o777))
	assert.ErrorContains(t, ensureSocketDir(filepath.Join(planted, "hook.sock")), "writable by group or other")
}

func TestHandStateToServiceAccount_RefusesAnotherOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("giving a directory to another account needs root")
	}
	state := t.TempDir()
	require.NoError(t, os.Chown(state, 4242, 4242))
	cfg := config.Default()
	cfg.Supervisor.StateDir = state
	cfg.Supervisor.ServerIdentity = "gryph-no-such-account"
	_, err := handStateToServiceAccount(cfg)
	assert.ErrorContains(t, err, "belongs to uid 4242")
}

func TestCheckStateDir_RefusesASymlink(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.Symlink(t.TempDir(), state))
	cfg := config.Default()
	cfg.Supervisor.StateDir = state
	_, err := checkStateDir(cfg)
	assert.ErrorContains(t, err, "is not a directory")
}

func TestInstallMachineKeys_WritesNoKeyForAnotherOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("giving a directory to another account needs root")
	}
	base, err := os.MkdirTemp("/tmp", "gryph-keys-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	require.NoError(t, os.Chmod(base, 0o755))
	state := filepath.Join(base, "state")
	require.NoError(t, os.Mkdir(state, 0o750))
	require.NoError(t, os.Chown(state, 4242, 4242))
	cfg := config.Default()
	cfg.Supervisor.StateDir = state
	cfg.Supervisor.ServerIdentity = "gryph-no-such-account"

	report := installMachineKeys(cfg)
	assert.Contains(t, report.Error, "belongs to uid 4242")
	assert.NoDirExists(t, cfg.Supervisor.KeyDir())
}
