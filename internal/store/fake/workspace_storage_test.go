package fake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

// storageConfigSeed builds a workspace for the workspace-storage tests.
func storageConfigSeed(t *testing.T, ctx context.Context, s store.Store, slug string) string {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return ws.ID
}

// TestWorkspaceStorageStore_GetAbsentNotFound pins the D16 default: absence
// of a row means the instance-default local storage applies — Get returns
// domain.ErrNotFound and callers fall back.
func TestWorkspaceStorageStore_GetAbsentNotFound(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID := storageConfigSeed(t, ctx, s, "wsstore-absent")

	if _, err := s.WorkspaceStorage().Get(ctx, wsID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for unconfigured workspace, got %v", err)
	}
}

// TestWorkspaceStorageStore_UpsertGetRoundTrip covers the sealed-secret
// contract: the config persists wholesale — including the opaque sealed
// envelope in SecretAccessKey and the UsePathStyle flag — and UpdatedAt is
// set by the store.
func TestWorkspaceStorageStore_UpsertGetRoundTrip(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID := storageConfigSeed(t, ctx, s, "wsstore-roundtrip")

	before := time.Now().UTC()
	cfg := &domain.WorkspaceStorageConfig{
		WorkspaceID:     wsID,
		Driver:          "s3",
		Endpoint:        "https://s3.us-east-1.amazonaws.com",
		Region:          "us-east-1",
		Bucket:          "acme-attachments",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "sealed:v1:opaque-envelope-bytes",
		UsePathStyle:    true,
	}
	if err := s.WorkspaceStorage().Upsert(ctx, cfg); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if cfg.UpdatedAt.Before(before) {
		t.Errorf("updated_at = %v, want a store-set time at or after %v", cfg.UpdatedAt, before)
	}

	got, err := s.WorkspaceStorage().Get(ctx, wsID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorkspaceID != wsID || got.Driver != "s3" {
		t.Errorf("scope/driver mismatch: %+v", got)
	}
	if got.Endpoint != cfg.Endpoint || got.Region != cfg.Region || got.Bucket != cfg.Bucket || got.AccessKeyID != cfg.AccessKeyID {
		t.Errorf("s3 fields not preserved: %+v", got)
	}
	if got.SecretAccessKey != cfg.SecretAccessKey {
		t.Errorf("sealed secret = %q, want the exact stored envelope %q", got.SecretAccessKey, cfg.SecretAccessKey)
	}
	if !got.UsePathStyle {
		t.Errorf("use_path_style = false, want true")
	}
	if !got.UpdatedAt.Equal(cfg.UpdatedAt) {
		t.Errorf("updated_at = %v, want the stored %v", got.UpdatedAt, cfg.UpdatedAt)
	}
}

// TestWorkspaceStorageStore_UpsertReplaces covers PUT semantics: a second
// upsert REPLACES the configuration wholesale — nothing of the previous
// config survives, including the secret.
func TestWorkspaceStorageStore_UpsertReplaces(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID := storageConfigSeed(t, ctx, s, "wsstore-replace")

	first := &domain.WorkspaceStorageConfig{
		WorkspaceID:     wsID,
		Driver:          "s3",
		Endpoint:        "https://s3.us-east-1.amazonaws.com",
		Region:          "us-east-1",
		Bucket:          "acme-attachments",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "sealed:v1:first",
		UsePathStyle:    true,
	}
	if err := s.WorkspaceStorage().Upsert(ctx, first); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	second := &domain.WorkspaceStorageConfig{
		WorkspaceID: wsID,
		Driver:      "local",
	}
	if err := s.WorkspaceStorage().Upsert(ctx, second); err != nil {
		t.Fatalf("upsert second: %v", err)
	}

	got, err := s.WorkspaceStorage().Get(ctx, wsID)
	if err != nil {
		t.Fatalf("get after replace: %v", err)
	}
	if got.Driver != "local" {
		t.Errorf("driver = %q, want local after replacement", got.Driver)
	}
	if got.Endpoint != "" || got.Region != "" || got.Bucket != "" || got.AccessKeyID != "" {
		t.Errorf("stale s3 fields survived replacement: %+v", got)
	}
	if got.SecretAccessKey != "" {
		t.Errorf("stale sealed secret survived replacement: %q", got.SecretAccessKey)
	}
	if got.UsePathStyle {
		t.Errorf("use_path_style = true, want false after replacement")
	}
	if !got.UpdatedAt.After(first.UpdatedAt) {
		t.Errorf("updated_at = %v, want a bump past %v", got.UpdatedAt, first.UpdatedAt)
	}
}

// TestWorkspaceStorageStore_UpsertUnknownWorkspaceNotFound pins FK parity:
// a config for an unknown workspace is rejected with domain.ErrNotFound.
func TestWorkspaceStorageStore_UpsertUnknownWorkspaceNotFound(t *testing.T) {
	s := fake.New()
	ctx := context.Background()

	cfg := &domain.WorkspaceStorageConfig{WorkspaceID: "00000000-0000-0000-0000-000000000000", Driver: "local"}
	if err := s.WorkspaceStorage().Upsert(ctx, cfg); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound for unknown workspace, got %v", err)
	}
}
