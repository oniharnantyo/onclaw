package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

func TestFileCoreStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-core-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	cs := memory.NewFileCoreStore(50) // limit to 50 characters

	// 1. Read nonexistent file should return empty string, no error
	val, err := cs.ReadCore(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to read nonexistent file: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string, got %q", val)
	}

	// 2. Add operation
	val, err = cs.WriteCore(ctx, tmpDir, "add", "", "line 1")
	if err != nil {
		t.Fatalf("failed to add: %v", err)
	}
	if val != "line 1" {
		t.Errorf("unexpected content: %q", val)
	}

	// Read check
	val, err = cs.ReadCore(ctx, tmpDir)
	if err != nil || val != "line 1" {
		t.Errorf("read failed or mismatch: err=%v, val=%q", err, val)
	}

	// Append line 2
	val, err = cs.WriteCore(ctx, tmpDir, "add", "", "line 2")
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if val != "line 1\nline 2" {
		t.Errorf("unexpected content after append: %q", val)
	}

	// 3. Replace operation
	val, err = cs.WriteCore(ctx, tmpDir, "replace", "line 2", "line two")
	if err != nil {
		t.Fatalf("failed to replace: %v", err)
	}
	if val != "line 1\nline two" {
		t.Errorf("unexpected content after replace: %q", val)
	}

	// Replace non-existent should fail
	_, err = cs.WriteCore(ctx, tmpDir, "replace", "missing", "new")
	if err == nil {
		t.Error("expected error replacing missing target, got nil")
	}

	// Replace duplicate should fail
	_, _ = cs.WriteCore(ctx, tmpDir, "add", "", "\nline 1") // Add duplicate line 1
	_, err = cs.WriteCore(ctx, tmpDir, "replace", "line 1", "new")
	if err == nil {
		t.Error("expected error replacing duplicate target, got nil")
	}

	// Clean up duplicate by overwriting the file manually or using the store
	path := filepath.Join(tmpDir, "MEMORY.md")
	_ = os.WriteFile(path, []byte("line 1\nline two"), 0644)

	// 4. Remove operation
	val, err = cs.WriteCore(ctx, tmpDir, "remove", "\nline two", "")
	if err != nil {
		t.Fatalf("failed to remove: %v", err)
	}
	if val != "line 1" {
		t.Errorf("unexpected content after remove: %q", val)
	}

	// 5. Character cap overflow test
	_, err = cs.WriteCore(ctx, tmpDir, "add", "", " this text is definitely way too long for a 50 character limit")
	if err == nil {
		t.Error("expected cap overflow error, got nil")
	}
}
