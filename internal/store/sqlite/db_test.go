package sqlite_test

import (
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDbPath(t *testing.T) {
	// 1. Explicit path
	p, err := sqlite.ResolveDbPath("/tmp/test.db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != "/tmp/test.db" {
		t.Errorf("expected /tmp/test.db, got %s", p)
	}

	// Save original env vars
	origHome := os.Getenv("HOME")
	defer func() {
		os.Setenv("HOME", origHome)
	}()

	// 2. Empty db_path, uses HOME/.onclaw/onclaw.db
	os.Setenv("HOME", "/custom/home")
	p, err = sqlite.ResolveDbPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("/custom/home", ".onclaw", "onclaw.db")
	if p != expected {
		t.Errorf("expected %s, got %s", expected, p)
	}

	// 3. Empty db_path, empty HOME (causes UserHomeDir error)
	os.Setenv("HOME", "")
	_, err = sqlite.ResolveDbPath("")
	if err == nil {
		t.Error("expected error resolving db path with empty HOME, got nil")
	}
}

func TestOpenAndPermissions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "onclaw.db")

	// 1. Open new db file (should create with 0600)
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed on non-existent file: %v", err)
	}
	defer db.Close()

	fi, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %04o", fi.Mode().Perm())
	}

	// 2. Close db, change permissions to 0644, expect Open to fail
	db.Close()
	if err := os.Chmod(dbPath, 0644); err != nil {
		t.Fatalf("Chmod failed: %v", err)
	}

	_, err = sqlite.Open(dbPath)
	if err == nil {
		t.Fatal("expected Open to fail for 0644 file, but it succeeded")
	}

	// 3. Restore to 0600, expect Open to succeed again
	if err := os.Chmod(dbPath, 0600); err != nil {
		t.Fatalf("Chmod failed: %v", err)
	}
	db2, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed on restored 0600 file: %v", err)
	}
	db2.Close()
}

func TestOpenErrors(t *testing.T) {
	// 1. Invalid path (parent directory cannot be created under a file)
	tmpDir, err := os.MkdirTemp("", "onclaw-store-err-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("plain text"), 0600); err != nil {
		t.Fatalf("failed to write blocker file: %v", err)
	}

	// This path attempts to create a directory under a regular file
	badPath := filepath.Join(filePath, "database.db")
	_, err = sqlite.Open(badPath)
	if err == nil {
		t.Fatal("expected error when parent directory creation is blocked by a file, but succeeded")
	}

	// 2. Open an empty folder as a database (causes open failure)
	dirPath := filepath.Join(tmpDir, "folder")
	if err := os.Mkdir(dirPath, 0700); err != nil {
		t.Fatalf("failed to create blocker directory: %v", err)
	}

	// stat succeeds, but it is a directory, not a 0600 file
	_, err = sqlite.Open(dirPath)
	if err == nil {
		t.Fatal("expected error when trying to open a directory as a DB file, but succeeded")
	}
}

func TestMigrate_RenamesSystemPromptToDescription(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Manually set up a legacy agents table with system_prompt column
	_, err := db.Exec("DROP TABLE IF EXISTS agents;")
	if err != nil {
		t.Fatalf("failed to drop agents table: %v", err)
	}

	createLegacyQuery := `CREATE TABLE agents (
		name TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		model TEXT NOT NULL DEFAULT '',
		model_metadata TEXT NOT NULL DEFAULT '{}',
		reasoning_effort TEXT NOT NULL DEFAULT '',
		reasoning_budget_tokens INTEGER NOT NULL DEFAULT 0,
		system_prompt TEXT NOT NULL DEFAULT '',
		workspace TEXT NOT NULL DEFAULT '',
		tools TEXT NOT NULL DEFAULT '',
		max_iterations INTEGER NOT NULL DEFAULT 0,
		max_context_tokens INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`
	if _, err := db.Exec(createLegacyQuery); err != nil {
		t.Fatalf("failed to create legacy agents table: %v", err)
	}

	insertLegacyQuery := `INSERT INTO agents (name, provider, system_prompt, created_at, updated_at)
		VALUES ('legacy-agent', 'openai-prov', 'You are a legacy prompt', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');`
	if _, err := db.Exec(insertLegacyQuery); err != nil {
		t.Fatalf("failed to insert legacy agent: %v", err)
	}

	// 2. Run Migrate
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// 3. Verify column is renamed to description and text is preserved
	var name, desc string
	err = db.QueryRow("SELECT name, description FROM agents WHERE name = 'legacy-agent'").Scan(&name, &desc)
	if err != nil {
		t.Fatalf("failed to query migrated agent: %v", err)
	}

	if desc != "You are a legacy prompt" {
		t.Errorf("expected migrated description 'You are a legacy prompt', got %q", desc)
	}
}
