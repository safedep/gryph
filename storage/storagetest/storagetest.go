// Package storagetest provides shared test helpers for code that exercises
// the storage layer. It lives outside _test.go so test packages in other
// directories (e.g. aarm/accumulator, cli) can reuse the setup.
package storagetest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/safedep/gryph/storage"
	"github.com/stretchr/testify/require"
)

// NewStore creates a fresh SQLite-backed storage.SQLiteStore in t.TempDir()
// and registers a t.Cleanup to close it. It fails the test on any setup
// error so callers can use it as a one-liner.
func NewStore(t *testing.T) *storage.SQLiteStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	require.NoError(t, store.Init(context.Background()))
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})
	return store
}
