package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpen_MigratesFreshV0Database covers task 3.2: opening a v0 (no schema)
// database applies the embedded migrations and produces a working store; a
// second Open on the migrated database is a no-op that succeeds.
func TestOpen_MigratesFreshV0Database(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/v0.db"

	st, err := Open(ctx, path)
	require.NoError(t, err, "a fresh v0 database must migrate on open")
	require.NotNil(t, st)
	require.NoError(t, st.Close())

	db := openRaw(t, path)

	for _, table := range []string{
		"schema_migrations", "agents", "sessions", "providers", "mcp_servers",
		"mcp_oauth_tokens", "schedules", "hooks", "gateway_settings", "session_events", "checkpoints",
		"skills", "tool_states", "agent_hooks", "hook_executions", "tool_settings",
		"attachments", "documents", "document_presets", "provider_models",
	} {
		require.True(t, tableExists(t, db, table), "table %s must exist after migration", table)
	}

	var version int
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 15, version, "the latest embedded schema version must be recorded as applied")

	// Second open: idempotent, still fully applied.
	st2, err := Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, st2.Close())
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 15, version, "re-opening must not re-apply migrations")
}

// TestOpen_EnablesWALAndBusyTimeout covers design D2's concurrency
// prerequisites: journal mode is WAL and every pooled connection carries the
// busy timeout.
func TestOpen_EnablesWALAndBusyTimeout(t *testing.T) {
	s := newTestStore(t)

	var journalMode string
	require.NoError(t, s.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode))
	require.Equal(t, "wal", journalMode)

	var busyTimeout int
	require.NoError(t, s.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout))
	require.Equal(t, busyTimeoutMS, busyTimeout)
}

// TestOpen_FailingMigrationPreventsStartup covers task 3.2's failure gate: a
// deliberately broken migration aborts with a version-attributed error, its
// partial work is rolled back (no table, no version recorded), the open
// fails so no store is handed to services, and the database remains clean
// enough that a corrected migration run still succeeds.
func TestOpen_FailingMigrationPreventsStartup(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/broken.db"

	// The full embedded schema followed by a hypothetical broken future
	// migration: its first statement succeeds, the second is invalid SQL —
	// both must roll back together (one transaction per migration).
	broken := append(mustEmbeddedMigrations(t), migration{
		Version: 16,
		Name:    "000016_broken",
		SQL:     `CREATE TABLE never_committed (id TEXT); THIS IS NOT VALID SQL;`,
	})
	_, err := openWith(ctx, path, broken)
	require.Error(t, err, "a failing migration must block the open (startup)")
	require.Contains(t, err.Error(), "000016_broken", "the error must name the failing migration")

	db := openRaw(t, path)
	require.True(t, tableExists(t, db, "agents"),
		"earlier migrations commit independently and must survive")
	require.False(t, tableExists(t, db, "never_committed"),
		"the failed migration's own work must roll back")

	var version int
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 15, version,
		"the embedded migrations commit; the failed migration's version must not be recorded")

	// A corrected run — the embedded list only — opens cleanly against the
	// recovered database.
	st, err := openWith(ctx, path, mustEmbeddedMigrations(t))
	require.NoError(t, err)
	require.NoError(t, st.Close())
	require.True(t, tableExists(t, db, "agents"))
}

// TestEmbeddedMigrations_OrderedAndParsable guards the loader: every
// embedded file parses to a strictly ascending version list.
func TestEmbeddedMigrations_OrderedAndParsable(t *testing.T) {
	migs := mustEmbeddedMigrations(t)
	require.NotEmpty(t, migs)

	for i, m := range migs {
		require.Positive(t, m.Version)
		require.NotEmpty(t, m.Name)
		require.NotEmpty(t, m.SQL)
		if i > 0 {
			require.Greater(t, m.Version, migs[i-1].Version, "migrations must be version-ordered")
		}
	}
	require.Equal(t, 1, migs[0].Version, "schema v1 must be the first migration")
}

// mustEmbeddedMigrations loads the embedded migration list for tests.
func mustEmbeddedMigrations(t *testing.T) []migration {
	t.Helper()
	migs, err := embeddedMigrations()
	require.NoError(t, err)
	return migs
}
