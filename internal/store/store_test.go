package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type dummyStore struct {
	store.Store
}

func TestRegistry(t *testing.T) {
	// Register dummy driver
	store.Register("test-dummy", func(ctx context.Context, cfg store.DSNConfig) (store.Store, error) {
		if cfg.DSN == "invalid" {
			return nil, errors.New("cannot connect")
		}
		return &dummyStore{}, nil
	})

	// Re-registering duplicate name should panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate registration, got nil")
		}
	}()
	store.Register("test-dummy", func(ctx context.Context, cfg store.DSNConfig) (store.Store, error) {
		return nil, nil
	})
}

func TestOpen(t *testing.T) {
	ctx := context.Background()

	// Unknown driver
	_, err := store.Open(ctx, "nonexistent", store.DSNConfig{DSN: "foo"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown driver, got %v", err)
	}

	// Successful open
	s, err := store.Open(ctx, "test-dummy", store.DSNConfig{DSN: "ok"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s == nil {
		t.Fatal("expected store instance, got nil")
	}

	// Error open
	_, err = store.Open(ctx, "test-dummy", store.DSNConfig{DSN: "invalid"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
