package storage_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
)

type dummyStorage struct{}

func (d *dummyStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return nil
}
func (d *dummyStorage) Open(ctx context.Context, key string) (storage.File, error) {
	return nil, domain.ErrNotFound
}
func (d *dummyStorage) Delete(ctx context.Context, key string) error {
	return nil
}
func (d *dummyStorage) URL(key string) string {
	return "/files/" + key
}

func TestStorageRegistry(t *testing.T) {
	storage.Register("test-dummy-storage", func(cfg storage.StorageConfig) (storage.Storage, error) {
		if cfg.DataDir == "error" {
			return nil, errors.New("cannot initialize")
		}
		return &dummyStorage{}, nil
	})

	// Duplicate panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate storage driver registration")
		}
	}()
	storage.Register("test-dummy-storage", func(cfg storage.StorageConfig) (storage.Storage, error) {
		return nil, nil
	})
}

func TestStorageOpen(t *testing.T) {
	// Unknown driver
	_, err := storage.Open("unknown-driver", storage.StorageConfig{})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown driver, got %v", err)
	}

	// Successful open
	s, err := storage.Open("test-dummy-storage", storage.StorageConfig{DataDir: "/tmp"})
	if err != nil || s == nil {
		t.Fatalf("expected successful open, got err: %v, storage: %v", err, s)
	}

	// Error open
	_, err = storage.Open("test-dummy-storage", storage.StorageConfig{DataDir: "error"})
	if err == nil {
		t.Fatal("expected error from open, got nil")
	}
}

func TestNewKey(t *testing.T) {
	key1, err := storage.NewKey()
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}
	key2, err := storage.NewKey()
	if err != nil {
		t.Fatalf("unexpected error generating key: %v", err)
	}

	// 128-bit hex key is 32 hex chars
	if len(key1) != 32 {
		t.Fatalf("expected 32-character hex key, got %d chars: %q", len(key1), key1)
	}
	if key1 == key2 {
		t.Fatalf("expected unique keys, got identical keys: %q", key1)
	}
}
