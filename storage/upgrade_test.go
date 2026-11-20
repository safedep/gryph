package storage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openV090Database writes the audit database of Gryph v0.9.0 to a new file
// and returns its path.
func openV090Database(t *testing.T) string {
	t.Helper()
	ddl, err := os.ReadFile(filepath.Join("testdata", "v0.9.0.sql"))
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "audit.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(string(ddl))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return path
}

func migrationPlan(t *testing.T, s *SQLiteStore) string {
	t.Helper()
	var plan bytes.Buffer
	require.NoError(t, s.client.Schema.WriteTo(context.Background(), &plan, migrateOptions...))
	return plan.String()
}

func hasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n))
	return n > 0
}

// The first hook after an upgrade runs the migration. A table copy holds
// the write lock for many seconds on a large database, so the upgrade from
// v0.9.0 must add each new column in place.
func TestUpgradeFromV090AddsColumnsInPlace(t *testing.T) {
	s, err := NewSQLiteStore(openV090Database(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	plan := migrationPlan(t, s)
	assert.NotContains(t, plan, "new_", "the migration copies a table")
	assert.NotContains(t, plan, "DROP TABLE")
	assert.Contains(t, plan, "ADD COLUMN")

	require.NoError(t, s.Init(context.Background()))

	assert.Empty(t, migrationPlan(t, s), "a migrated database has a migration left")
	assert.True(t, hasColumn(t, s.DB(), "audit_events", "conversation_context"),
		"the migration rebuilt audit_events and dropped a column")
	assert.True(t, hasColumn(t, s.DB(), "sessions", "event_count"))

	var events, sessions int
	require.NoError(t, s.DB().QueryRow(`SELECT count(*) FROM audit_events`).Scan(&events))
	require.NoError(t, s.DB().QueryRow(`SELECT count(*) FROM sessions`).Scan(&sessions))
	assert.Equal(t, 1, events)
	assert.Equal(t, 1, sessions)
}

// Ent turns foreign keys off on a pooled connection before the migration
// transaction, and on again when it ends. A BEGIN that times out behind
// another hook leaves that connection off, and ent then refuses every
// retry on it. The retry must open new connections.
func TestCreateSchemaRetriesAfterForeignKeysOff(t *testing.T) {
	ctx := context.Background()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "audit.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Init(ctx))

	s.db.SetMaxIdleConns(0)
	s.db.SetMaxIdleConns(1)
	conn, err := s.db.Conn(ctx)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "PRAGMA foreign_keys = off")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	require.NoError(t, s.createSchema(ctx))
}

// Two hooks that upgrade one database plan the same new column. The loser
// fails with "duplicate column name", and its retry finds nothing to do.
func TestRetryableSchemaError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{`table "sessions" already exists`, true},
		{`add column "imported" to table: "sessions": SQL logic error: duplicate column name: imported (1)`, true},
		{"database is locked (5) (SQLITE_BUSY)", true},
		{`sqlite: foreign_keys pragma is off: missing "_fk=1" in the connection string`, true},
		{"no such table: sessions", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, retryableSchemaError(errors.New(tc.msg)), tc.msg)
	}
}
