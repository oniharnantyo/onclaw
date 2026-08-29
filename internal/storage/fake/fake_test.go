package fake_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage/fake"
)

func TestFakeStorage_PutOpenDelete(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	content := []byte("image binary data here")
	key := "avatars/random-uuid"

	// 1. Open non-existent
	_, err := s.Open(ctx, key)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent key, got %v", err)
	}

	// 2. Put
	if err := s.Put(ctx, key, bytes.NewReader(content), int64(len(content)), "image/png"); err != nil {
		t.Fatalf("unexpected Put error: %v", err)
	}

	// 3. Open and verify
	file, err := s.Open(ctx, key)
	if err != nil {
		t.Fatalf("unexpected Open error: %v", err)
	}
	defer file.Close()

	if file.ContentType() != "image/png" {
		t.Fatalf("expected contentType image/png, got %q", file.ContentType())
	}
	if file.Size() != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), file.Size())
	}

	readBytes, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	if !bytes.Equal(readBytes, content) {
		t.Fatalf("read content does not match put content")
	}

	// 4. Seek test
	offset, err := file.Seek(6, io.SeekStart)
	if err != nil || offset != 6 {
		t.Fatalf("seek error: %v, offset: %d", err, offset)
	}
	subBytes, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read after seek error: %v", err)
	}
	if string(subBytes) != string(content[6:]) {
		t.Fatalf("expected %q, got %q", string(content[6:]), string(subBytes))
	}

	// 5. URL
	url := s.URL(key)
	if url != "/api/v1/files/"+key {
		t.Fatalf("unexpected URL: %q", url)
	}

	// 6. Delete
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}
	_, err = s.Open(ctx, key)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}
