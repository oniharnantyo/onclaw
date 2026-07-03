package secrets_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/secrets"
)

func TestGetOrCreateKeyfileKEK(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "secrets-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	keyfilePath := filepath.Join(tempDir, "master.key")

	// 1. Should create a new keyfile since it doesn't exist
	kek1, err := secrets.GetOrCreateKeyfileKEK(keyfilePath)
	if err != nil {
		t.Fatalf("failed to get/create keyfile KEK: %v", err)
	}

	if len(kek1) != 32 {
		t.Errorf("expected 32 bytes KEK, got %d", len(kek1))
	}

	info, err := os.Stat(keyfilePath)
	if err != nil {
		t.Fatalf("expected keyfile to exist: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected permissions 0600, got %04o", perm)
	}

	// 2. Should read the existing keyfile
	kek2, err := secrets.GetOrCreateKeyfileKEK(keyfilePath)
	if err != nil {
		t.Fatalf("failed to get existing keyfile KEK: %v", err)
	}

	if !bytes.Equal(kek1, kek2) {
		t.Error("expected kek2 to match kek1")
	}

	// 3. If permissions are wider, refuse to operate
	if err := os.Chmod(keyfilePath, 0644); err != nil {
		t.Fatalf("failed to chmod keyfile: %v", err)
	}

	_, err = secrets.GetOrCreateKeyfileKEK(keyfilePath)
	if err == nil {
		t.Error("expected error due to wider permissions (0644), but it succeeded")
	}
}

func TestResolveKeyfilePath(t *testing.T) {
	dbPath := "/path/to/db/data.db"
	expected := "/path/to/db/master.key"
	got := secrets.ResolveKeyfilePath(dbPath)
	if got != expected {
		t.Errorf("expected ResolveKeyfilePath to return %q, got %q", expected, got)
	}
}

func TestKeyfileErrors(t *testing.T) {
	// 1. GetOrCreateKeyfileKEK directory creation failure
	tmpDir, err := os.MkdirTemp("", "secrets-err-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("plain text"), 0600); err != nil {
		t.Fatalf("failed to write blocker file: %v", err)
	}

	// parent dir creation is blocked by filePath
	blockedKeyPath := filepath.Join(filePath, "master.key")
	_, err = secrets.GetOrCreateKeyfileKEK(blockedKeyPath)
	if err == nil {
		t.Error("expected GetOrCreateKeyfileKEK to fail when parent dir creation is blocked, got nil")
	}

	// 2. GetOrCreateKeyfileKEK keyfile invalid length
	invalidKeyPath := filepath.Join(tmpDir, "invalid.key")
	if err := os.WriteFile(invalidKeyPath, []byte("too-short"), 0600); err != nil {
		t.Fatalf("failed to write invalid keyfile: %v", err)
	}
	_, err = secrets.GetOrCreateKeyfileKEK(invalidKeyPath)
	if err == nil {
		t.Error("expected GetOrCreateKeyfileKEK to fail when keyfile is not 32 bytes, got nil")
	}
}
