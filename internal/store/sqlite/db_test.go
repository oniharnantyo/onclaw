package sqlite

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDbPath(t *testing.T) {
	// 1. Explicit path
	p, err := ResolveDbPath("/tmp/test.db")
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
	p, err = ResolveDbPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("/custom/home", ".onclaw", "onclaw.db")
	if p != expected {
		t.Errorf("expected %s, got %s", expected, p)
	}

	// 3. Empty db_path, empty HOME (causes UserHomeDir error)
	os.Setenv("HOME", "")
	_, err = ResolveDbPath("")
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
	db, err := Open(dbPath)
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

	_, err = Open(dbPath)
	if err == nil {
		t.Fatal("expected Open to fail for 0644 file, but it succeeded")
	}

	// 3. Restore to 0600, expect Open to succeed again
	if err := os.Chmod(dbPath, 0600); err != nil {
		t.Fatalf("Chmod failed: %v", err)
	}
	db2, err := Open(dbPath)
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
	_, err = Open(badPath)
	if err == nil {
		t.Fatal("expected error when parent directory creation is blocked by a file, but succeeded")
	}

	// 2. Open an empty folder as a database (causes open failure)
	dirPath := filepath.Join(tmpDir, "folder")
	if err := os.Mkdir(dirPath, 0700); err != nil {
		t.Fatalf("failed to create blocker directory: %v", err)
	}

	// stat succeeds, but it is a directory, not a 0600 file
	_, err = Open(dirPath)
	if err == nil {
		t.Fatal("expected error when trying to open a directory as a DB file, but succeeded")
	}
}
