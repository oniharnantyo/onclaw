package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestKVStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	ks := NewKVStore(db)

	// Test setting KV with empty key
	if err := ks.Set(ctx, "", "val"); err == nil {
		t.Error("expected error setting KV with empty key, got nil")
	}

	// Test setting KV
	if err := ks.Set(ctx, "default_provider", "test-prov"); err != nil {
		t.Fatalf("failed to Set preferences: %v", err)
	}

	// Test getting KV
	pref, err := ks.Get(ctx, "default_provider")
	if err != nil {
		t.Fatalf("failed to Get preferences: %v", err)
	}
	if pref != "test-prov" {
		t.Errorf("expected test-prov, got %s", pref)
	}

	// Test getting non-existent KV
	_, err = ks.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error getting nonexistent preference, got nil")
	}

	// Test updating KV (upsert)
	if err := ks.Set(ctx, "default_provider", "another-prov"); err != nil {
		t.Fatalf("failed to update preference: %v", err)
	}

	updatedPref, err := ks.Get(ctx, "default_provider")
	if err != nil {
		t.Fatalf("failed to get updated preference: %v", err)
	}
	if updatedPref != "another-prov" {
		t.Errorf("expected updated value another-prov, got %s", updatedPref)
	}

	// Test deleting KV
	if err := ks.Delete(ctx, "default_provider"); err != nil {
		t.Fatalf("failed to Delete preference: %v", err)
	}

	// Verify KV was deleted
	_, err = ks.Get(ctx, "default_provider")
	if err == nil {
		t.Error("expected error getting deleted preference, got nil")
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after deletion, got %v", err)
	}

	// Test deleting non-existent KV (should not error)
	if err := ks.Delete(ctx, "nonexistent"); err != nil {
		t.Errorf("Delete on non-existent key should not error, got: %v", err)
	}
}
