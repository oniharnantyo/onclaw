package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
)

func TestBrowseFS(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "onclaw-test-browsefs")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test subdirectories
	subDirs := []string{"dirB", "dirA", ".hiddenDir"}
	for _, sub := range subDirs {
		err := os.Mkdir(filepath.Join(tempDir, sub), 0755)
		if err != nil {
			t.Fatalf("failed to create sub dir %s: %v", sub, err)
		}
	}

	// Create a test file (should be ignored since we only list directories)
	err = os.WriteFile(filepath.Join(tempDir, "fileA.txt"), []byte("content"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	svc := &service.Service{}
	ctx := context.Background()

	res, err := svc.BrowseFS(ctx, tempDir)
	if err != nil {
		t.Fatalf("BrowseFS failed: %v", err)
	}

	absTempDir, _ := filepath.Abs(tempDir)
	if res.CurrentPath != absTempDir {
		t.Errorf("expected CurrentPath %s, got %s", absTempDir, res.CurrentPath)
	}

	// Should exclude hidden directories and files, listing only dirA and dirB in sorted order
	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res.Entries))
	}

	if res.Entries[0].Name != "dirA" || !res.Entries[0].IsDir {
		t.Errorf("expected first entry to be dirA, got %+v", res.Entries[0])
	}

	if res.Entries[1].Name != "dirB" || !res.Entries[1].IsDir {
		t.Errorf("expected second entry to be dirB, got %+v", res.Entries[1])
	}
}
