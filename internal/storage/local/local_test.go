package local_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	_ "github.com/oniharnantyo/onclaw/internal/storage/local"
)

func TestLocalDriver_Integration(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// 1. Open via registry
	storeInst, err := storage.Open("local", storage.StorageConfig{
		DataDir: tempDir,
		BaseURL: "/api/v1/files/",
	})
	if err != nil {
		t.Fatalf("unexpected error opening local driver: %v", err)
	}

	content := []byte("fake png avatar data here")
	key := "avatars/sample-key-123"

	// 2. Open non-existent
	_, err = storeInst.Open(ctx, key)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing file, got %v", err)
	}

	// 3. Put
	if err := storeInst.Put(ctx, key, bytes.NewReader(content), int64(len(content)), "image/png"); err != nil {
		t.Fatalf("unexpected Put error: %v", err)
	}

	// Verify file and metadata exist on disk
	contentPath := filepath.Join(tempDir, "files", "avatars", "sample-key-123")
	metaPath := contentPath + ".meta"
	if _, err := os.Stat(contentPath); err != nil {
		t.Fatalf("expected content file to exist on disk: %v", err)
	}
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("expected meta file to exist on disk: %v", err)
	}

	// 4. Open and read
	f, err := storeInst.Open(ctx, key)
	if err != nil {
		t.Fatalf("unexpected Open error: %v", err)
	}
	defer f.Close()

	if f.ContentType() != "image/png" {
		t.Fatalf("expected image/png content type, got %q", f.ContentType())
	}
	if f.Size() != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), f.Size())
	}

	readBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	if !bytes.Equal(readBytes, content) {
		t.Fatalf("content mismatch")
	}

	// 5. Seek
	offset, err := f.Seek(5, io.SeekStart)
	if err != nil || offset != 5 {
		t.Fatalf("seek failed: %v, offset: %d", err, offset)
	}
	subBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read after seek failed: %v", err)
	}
	if string(subBytes) != string(content[5:]) {
		t.Fatalf("expected %q, got %q", string(content[5:]), string(subBytes))
	}

	// 6. URL
	url := storeInst.URL(key)
	if url != "/api/v1/files/"+key {
		t.Fatalf("unexpected capability URL: %q", url)
	}

	// 7. Delete
	if err := storeInst.Delete(ctx, key); err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}

	// Verify files deleted from disk
	if _, err := os.Stat(contentPath); !os.IsNotExist(err) {
		t.Fatal("expected content file to be deleted")
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Fatal("expected meta file to be deleted")
	}

	// Open after delete returns ErrNotFound
	_, err = storeInst.Open(ctx, key)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestLocalDriver_PathTraversalDefense(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	storeInst, err := storage.Open("local", storage.StorageConfig{
		DataDir: tempDir,
	})
	if err != nil {
		t.Fatalf("unexpected error opening local driver: %v", err)
	}

	traversalKeys := []string{
		"../escape",
		"../../etc/passwd",
		"/etc/passwd",
		"avatars/../../outside",
		"",
	}

	for _, key := range traversalKeys {
		t.Run("traversal_"+key, func(t *testing.T) {
			err := storeInst.Put(ctx, key, bytes.NewReader([]byte("hack")), 4, "text/plain")
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid for traversal key %q, got %v", key, err)
			}

			_, err = storeInst.Open(ctx, key)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid for traversal key %q on Open, got %v", key, err)
			}

			err = storeInst.Delete(ctx, key)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid for traversal key %q on Delete, got %v", key, err)
			}
		})
	}
}
