package sqlite

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestProfileStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	ps := NewProfileStore(db)

	// Test adding invalid profile (empty name)
	invalidP := &store.Profile{Name: ""}
	if err := ps.AddProfile(ctx, invalidP); err == nil {
		t.Error("expected error adding empty profile, got nil")
	}

	// Test adding valid profile
	p := &store.Profile{
		Name:         "test-prov",
		ProviderType: "openai",
		APIBase:      "https://api.openai.com/v1",
		Settings:     `{"reasoning_effort": "high"}`,
		Enabled:      1,
	}

	if err := ps.AddProfile(ctx, p); err != nil {
		t.Fatalf("failed to AddProfile: %v", err)
	}

	// Test getting profile
	gotP, err := ps.GetProfile(ctx, p.Name)
	if err != nil {
		t.Fatalf("failed to GetProfile: %v", err)
	}

	// Verify profile fields match
	if gotP.Name != p.Name ||
		gotP.ProviderType != p.ProviderType ||
		gotP.APIBase != p.APIBase ||
		gotP.Settings != p.Settings ||
		gotP.Enabled != p.Enabled {
		t.Errorf("profile fields mismatch. got: %+v, want: %+v", gotP, p)
	}

	// Verify timestamps were set
	if gotP.CreatedAt == "" || gotP.UpdatedAt == "" {
		t.Error("expected CreatedAt and UpdatedAt to be set, got empty strings")
	}
	if gotP.CreatedAt != gotP.UpdatedAt {
		t.Logf("Note: CreatedAt (%s) != UpdatedAt (%s) for new profile", gotP.CreatedAt, gotP.UpdatedAt)
	}

	// Test getting non-existent profile
	_, err = ps.GetProfile(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error getting nonexistent profile, got nil")
	}

	// Test listing profiles
	list, err := ps.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("failed to ListProfiles: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected list length 1, got %d", len(list))
	}
	if list[0].Name != p.Name {
		t.Errorf("expected profile name %s, got %s", p.Name, list[0].Name)
	}

	// Test that adding duplicate profile fails
	if err := ps.AddProfile(ctx, p); err == nil {
		t.Error("expected error when adding duplicate profile, got nil")
	}

	// Test removing profile
	if err := ps.RemoveProfile(ctx, p.Name); err != nil {
		t.Fatalf("failed to RemoveProfile: %v", err)
	}

	// Verify profile was removed
	_, err = ps.GetProfile(ctx, p.Name)
	if err == nil {
		t.Error("expected error getting removed profile, got nil")
	}

	// Test removing non-existent profile
	if err := ps.RemoveProfile(ctx, "nonexistent"); err == nil {
		t.Error("expected RemoveProfile to return error for nonexistent profile")
	}
}

func TestMigrateIdempotency(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Run migration second time - should not error
	if err := Migrate(db); err != nil {
		t.Errorf("second migration failed: %v", err)
	}

	// Verify WAL mode is still enabled
	var journalMode string
	err := db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode=wal, got %s", journalMode)
	}
}

func TestMigrateClosedDB(t *testing.T) {
	db, cleanup := setupTestDB(t)
	cleanup() // Close and cleanup

	// Running migrate on closed DB should fail
	if err := Migrate(db); err == nil {
		t.Error("expected Migrate to fail on closed database, got nil")
	}
}
