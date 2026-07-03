package sqlite_test

import (
	"context"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"testing"
)

func TestSecretStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	ss := sqlite.NewSecretStore(db)

	// Test setting secret with empty key
	if err := ss.SetSecret(ctx, "", "val"); err == nil {
		t.Error("expected error setting secret with empty key, got nil")
	}

	// Test setting secret
	if err := ss.SetSecret(ctx, "provider:test-prov", "enc-val-123"); err != nil {
		t.Fatalf("failed to SetSecret: %v", err)
	}

	// Test getting secret
	sec, err := ss.GetSecret(ctx, "provider:test-prov")
	if err != nil {
		t.Fatalf("failed to GetSecret: %v", err)
	}
	if sec != "enc-val-123" {
		t.Errorf("expected enc-val-123, got %s", sec)
	}

	// Test getting non-existent secret (should return empty string, no error)
	secNon, err := ss.GetSecret(ctx, "provider:nonexistent")
	if err != nil {
		t.Fatalf("GetSecret on nonexistent key failed: %v", err)
	}
	if secNon != "" {
		t.Errorf("expected empty string for nonexistent secret, got %s", secNon)
	}

	// Test updating secret (upsert)
	if err := ss.SetSecret(ctx, "provider:test-prov", "enc-val-456"); err != nil {
		t.Fatalf("failed to update Secret: %v", err)
	}

	updatedSec, err := ss.GetSecret(ctx, "provider:test-prov")
	if err != nil {
		t.Fatalf("failed to get updated Secret: %v", err)
	}
	if updatedSec != "enc-val-456" {
		t.Errorf("expected updated value enc-val-456, got %s", updatedSec)
	}

	// Test deleting secret
	if err := ss.DeleteSecret(ctx, "provider:test-prov"); err != nil {
		t.Fatalf("failed to DeleteSecret: %v", err)
	}

	// Verify secret was deleted
	deletedSec, err := ss.GetSecret(ctx, "provider:test-prov")
	if err != nil {
		t.Fatalf("GetSecret after delete failed: %v", err)
	}
	if deletedSec != "" {
		t.Errorf("expected empty string after deletion, got %s", deletedSec)
	}
}
