//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// attachmentSeed builds a workspace and the uploading user an attachment row
// references, returning the ids the attachment tests need.
func attachmentSeed(t *testing.T, ctx context.Context, s store.Store, slug string) (workspaceID, userID string) {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	user := &domain.User{Email: slug + "-uploader@example.com", Name: "Uploader"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return ws.ID, user.ID
}

// attachmentFixture returns an attachment pointing at the given workspace
// and creator, ready for Create.
func attachmentFixture(workspaceID, createdBy, storageKey string) *domain.Attachment {
	return &domain.Attachment{
		WorkspaceID: workspaceID,
		StorageKey:  storageKey,
		Backend:     "local",
		Name:        "diagram.png",
		MimeType:    "image/png",
		Size:        123456,
		Lane:        domain.AttachmentLaneInlineImage,
		CreatedBy:   createdBy,
	}
}

// TestIntegration_AttachmentStore_CreateAndByID covers the create → scoped
// lookup round-trip: every domain field survives, the id and CreatedAt are
// store-managed, and the row is reachable through its capability key.
func TestIntegration_AttachmentStore_CreateAndByID(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, userID := attachmentSeed(t, ctx, s, "att-roundtrip")

	att := attachmentFixture(wsID, userID, "att/roundtrip-key")
	if err := s.Attachments().Create(ctx, att); err != nil {
		t.Fatalf("create: %v", err)
	}
	if att.ID == "" {
		t.Fatal("expected id to be assigned by the store")
	}
	if att.CreatedAt.IsZero() {
		t.Fatal("expected created_at to be set by the store")
	}

	got, err := s.Attachments().ByID(ctx, wsID, att.ID)
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if got.ID != att.ID ||
		got.WorkspaceID != wsID ||
		got.StorageKey != "att/roundtrip-key" ||
		got.Backend != "local" ||
		got.Name != "diagram.png" ||
		got.MimeType != "image/png" ||
		got.Size != 123456 ||
		got.Lane != domain.AttachmentLaneInlineImage ||
		got.CreatedBy != userID {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, att)
	}
	if !got.CreatedAt.Equal(att.CreatedAt) {
		t.Errorf("created_at = %v, want the stored %v", got.CreatedAt, att.CreatedAt)
	}

	byKey, err := s.Attachments().ByStorageKey(ctx, "att/roundtrip-key")
	if err != nil {
		t.Fatalf("by storage key: %v", err)
	}
	if byKey.ID != att.ID || byKey.StorageKey != "att/roundtrip-key" {
		t.Errorf("by-storage-key row mismatch: got %+v", byKey)
	}
}

// TestIntegration_AttachmentStore_ByIDForeignWorkspaceNotFound covers the
// tenancy requirement: an attachment id from another workspace is
// indistinguishable from an unknown id (domain.ErrNotFound — no existence
// leak).
func TestIntegration_AttachmentStore_ByIDForeignWorkspaceNotFound(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, userID := attachmentSeed(t, ctx, s, "att-foreign")

	other := &domain.Workspace{Slug: "att-foreign-other", Name: "Other WS"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}

	att := attachmentFixture(wsID, userID, "att/foreign-key")
	if err := s.Attachments().Create(ctx, att); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.Attachments().ByID(ctx, other.ID, att.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace lookup: expected ErrNotFound, got %v", err)
	}
	if _, err := s.Attachments().ByID(ctx, wsID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown-id lookup: expected ErrNotFound, got %v", err)
	}

	// The failed attempts must not have disturbed the row.
	got, err := s.Attachments().ByID(ctx, wsID, att.ID)
	if err != nil || got.ID != att.ID {
		t.Errorf("owning-workspace lookup should still succeed, got %+v, err %v", got, err)
	}
}

// TestIntegration_AttachmentStore_ByStorageKeyMissNotFound covers the
// capability-key serving path: unknown keys return domain.ErrNotFound.
func TestIntegration_AttachmentStore_ByStorageKeyMissNotFound(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, userID := attachmentSeed(t, ctx, s, "att-key-miss")

	att := attachmentFixture(wsID, userID, "att/known-key")
	if err := s.Attachments().Create(ctx, att); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.Attachments().ByStorageKey(ctx, "att/never-stored"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown storage key: expected ErrNotFound, got %v", err)
	}
}

// TestIntegration_AttachmentStore_CreateUnknownReferencesNotFound pins the
// FK behavior: unknown workspace or creator references surface as
// domain.ErrNotFound through the store's error translation.
func TestIntegration_AttachmentStore_CreateUnknownReferencesNotFound(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	wsID, userID := attachmentSeed(t, ctx, s, "att-fk")

	unknown := "00000000-0000-0000-0000-000000000000"
	if err := s.Attachments().Create(ctx, attachmentFixture(unknown, userID, "att/bad-ws")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown workspace: expected ErrNotFound, got %v", err)
	}
	if err := s.Attachments().Create(ctx, attachmentFixture(wsID, unknown, "att/bad-user")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown creator: expected ErrNotFound, got %v", err)
	}
}
