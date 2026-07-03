package sqlite_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

// setupTestDB creates a temporary SQLite database for testing.
// Returns the database connection and a cleanup function.
func setupTestDB(t *testing.T) (*sql.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "onclaw-store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "onclaw.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Open failed: %v", err)
	}

	if err := sqlite.Migrate(db); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Migrate failed: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}
