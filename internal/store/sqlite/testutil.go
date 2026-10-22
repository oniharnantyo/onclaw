package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// newTestStore opens a migrated store against a fresh temporary database.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st.(*Store)
}

// openRaw opens a second raw connection pool to the same database file for
// schema-level assertions that bypass the store.
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn(path))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// tableExists reports whether a table exists in the database.
func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	require.NoError(t, err)
	return n > 0
}

// eventBatch builds len(ids) events sharing one kind and unique payloads.
func eventBatch(ids ...string) []store.SessionEvent {
	out := make([]store.SessionEvent, 0, len(ids))
	for _, id := range ids {
		out = append(out, store.SessionEvent{
			EventID: id,
			TurnID:  "turn-1",
			Kind:    "message",
			Payload: []byte(`{"text":"` + id + `"}`),
		})
	}
	return out
}
