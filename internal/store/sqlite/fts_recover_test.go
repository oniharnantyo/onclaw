package sqlite_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

// corruptMemoryFts reproduces the "phantom" FTS5 state the production database
// was found in: the memory_documents_fts virtual table is still registered in
// sqlite_master, but its shadow tables (notably _config, which must hold a
// version row) have been emptied — e.g. by a manual table truncation. Every
// access then fails with "invalid fts5 file format (found 0, expected 4 or 5)".
//
// The shadow tables are read-only under SQLite's defensive mode, so they are
// emptied through writable_schema on a dedicated connection, mirroring how an
// admin-level truncation would leave the database.
func corruptMemoryFts(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire conn to corrupt fts: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "PRAGMA writable_schema=ON"); err != nil {
		t.Fatalf("enable writable_schema: %v", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin corrupt tx: %v", err)
	}
	for _, q := range []string{
		`DELETE FROM memory_documents_fts_data`,
		`DELETE FROM memory_documents_fts_idx`,
		`DELETE FROM memory_documents_fts_docsize`,
		`DELETE FROM memory_documents_fts_config`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			tx.Rollback()
			t.Fatalf("empty shadow table: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit corrupt tx: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA writable_schema=RESET"); err != nil {
		t.Fatalf("reset writable_schema: %v", err)
	}
}

func TestMigrate_RecoversCorruptMemoryFts(t *testing.T) {
	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Seed documents that must remain searchable after the index is rebuilt.
	seeds := []string{"the quick brown fox", "lazy dog naps", "fts rebuild resilience"}
	for _, content := range seeds {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO memory_documents (agent, scope, kind, content, source, embedding_model, created_at)
			 VALUES ('a', 'global', 'note', ?, 't', '', '2026-01-01T00:00:00Z')`,
			content,
		); err != nil {
			t.Fatalf("seed document: %v", err)
		}
	}

	// Reproduce the corruption, then prove the bare backfill bricks on it.
	corruptMemoryFts(t, db)
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO memory_documents_fts(rowid, content) SELECT id, content FROM memory_documents`); err == nil {
		t.Fatal("expected the bare backfill to fail on the corrupt phantom, got nil")
	}

	// Migrate must heal the index instead of returning an error.
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("Migrate should recover the corrupt FTS, got: %v", err)
	}

	// A document indexed after recovery reaches the FTS via its trigger.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_documents (agent, scope, kind, content, source, embedding_model, created_at)
		 VALUES ('a', 'global', 'note', 'newly inserted token', 't', '', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert via base table after recovery: %v", err)
	}
	assertFtsMatch := func(term string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRowContext(ctx,
			`SELECT count(*) FROM memory_documents d JOIN memory_documents_fts f ON d.id = f.rowid WHERE f.content MATCH ?`,
			term,
		).Scan(&got); err != nil {
			t.Fatalf("MATCH %q after recovery: %v", term, err)
		}
		if got != want {
			t.Errorf("MATCH %q after recovery: want %d, got %d", term, want, got)
		}
	}
	assertFtsMatch("token", 1) // newly inserted, exercises the trigger

	// The seeded documents were backfilled into the rebuilt index and remain searchable.
	for _, term := range []string{"fox", "dog", "resilience"} {
		assertFtsMatch(term, 1)
	}

	// The version row that FTS5 writes at creation time is present again.
	var configRows int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM memory_documents_fts_config").Scan(&configRows); err != nil {
		t.Fatalf("read fts config: %v", err)
	}
	if configRows == 0 {
		t.Error("expected memory_documents_fts_config to hold its version row after recovery")
	}

	// The unrelated FTS index is untouched by recovery.
	var convConfigRows int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversation_messages_fts_config").Scan(&convConfigRows); err != nil {
		t.Fatalf("read conversation fts config: %v", err)
	}
	if convConfigRows == 0 {
		t.Error("conversation_messages_fts_config should still hold its version row")
	}
}

// TestMigrate_HealthyFtsFastPath confirms a healthy database never enters the
// recovery path: the bare backfill succeeds and rebuildMemoryFts stays dormant.
func TestMigrate_HealthyFtsFastPath(t *testing.T) {
	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO memory_documents (agent, scope, kind, content, source, embedding_model, created_at)
		 VALUES ('a', 'global', 'note', 'healthy fast path content', 't', '', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("Migrate on healthy db failed: %v", err)
	}

	var got int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM memory_documents d JOIN memory_documents_fts f ON d.id = f.rowid WHERE f.content MATCH 'healthy'`,
	).Scan(&got); err != nil {
		t.Fatalf("MATCH on healthy db: %v", err)
	}
	if got != 1 {
		t.Errorf("MATCH 'healthy': want 1, got %d", got)
	}
}