package cli

import (
	"testing"

	"github.com/safedep/gryph/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApp_DisplayedDatabasePath_HonorsConfigOverride is a regression test
// for the bug where status, doctor, install, and uninstall displayed the
// resolved default database path (from config.ResolvePaths) instead of the
// path configured via storage.path in config.yaml.
//
// The actual DB operations (InitStore) already used Config.GetDatabasePath()
// correctly, so the data landed at the overridden location — but the user-
// visible messages and log entries pointed at the default path, misleading
// operators about where their data actually lives.
//
// The fix replaces app.Paths.DatabaseFile with app.Config.GetDatabasePath()
// at every display / operational callsite. This test asserts the invariant:
// once storage.path is set, the config-aware path differs from the resolved
// default, and the two MUST NOT be used interchangeably.
func TestApp_DisplayedDatabasePath_HonorsConfigOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.Path = "/tmp/gryph-test-override.db"

	app, err := NewApp(cfg)
	require.NoError(t, err)
	defer func() {
		_ = app.Close()
	}()

	// The config-aware path is what status, doctor, install, and uninstall
	// must display and operate on.
	assert.Equal(t, "/tmp/gryph-test-override.db", app.Config.GetDatabasePath())

	// The Paths.DatabaseFile is still the resolved platform default — that
	// is correct for callers that explicitly want the default, but it is
	// NOT what the user-facing commands should show when an override is
	// configured. Guard against a regression that silently swaps the two.
	assert.NotEqual(t, "/tmp/gryph-test-override.db", app.Paths.DatabaseFile,
		"Paths.DatabaseFile must remain the resolved default; user-facing "+
			"callsites must use Config.GetDatabasePath() instead")
}

// TestApp_DisplayedDatabasePath_DefaultsAlignWhenNoOverride documents that
// the two paths agree when storage.path is not set, so the fix is a no-op
// for the common case.
func TestApp_DisplayedDatabasePath_DefaultsAlignWhenNoOverride(t *testing.T) {
	cfg := config.Default()
	// Storage.Path intentionally left empty.

	app, err := NewApp(cfg)
	require.NoError(t, err)
	defer func() {
		_ = app.Close()
	}()

	assert.Equal(t, app.Paths.DatabaseFile, app.Config.GetDatabasePath())
}
