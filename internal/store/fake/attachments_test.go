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

// attachmentSeed builds a workspace and a user, returning the ids the
// attachment tests need (both are FK references on attachments rows).
func attachmentSeed(t *testing.T, ctx context.Context, s store.Store, slug string) (workspaceID, userID string) {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "WS " + slug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	user := &domain.User{Email: slug + "-owner@example.com", Name: "Owner"}
	if err := s.Users().Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return ws.ID, user.ID
}

// TestAttachmentStore_CreateAndByIDRoundTrip covers the upload-path shape:
// Create mints the id, sets CreatedAt itself, and ByID returns every stored
// field — including the non-serialized StorageKey and Backend.
func TestAttachmentStore_CreateAndByIDRoundTrip(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID := attachmentSeed(t, ctx, s, "att-roundtrip")

	before := time.Now().UTC()
	a := &domain.Attachment{
		WorkspaceID: wsID,
		StorageKey:  "att/9f86d081884c7d659a2feaa0c55ad015",
		Backend:     "local",
		Name:        "shot.png",
		MimeType:    "image/png",
		Size:        48120,
		Lane:        domain.AttachmentLaneInlineImage,
		CreatedBy:   userID,
	}
	if err := s.Attachments().Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == "" {
		t.Fatal("expected the store to mint an id")
	}
	if a.CreatedAt.Before(before) {
		t.Errorf("created_at = %v, want a store-set time at or after %v", a.CreatedAt, before)
	}

	got, err := s.Attachments().ByID(ctx, wsID, a.ID)
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if got.ID != a.ID || got.WorkspaceID != wsID {
		t.Errorf("scope mismatch: got id=%q ws=%q", got.ID, got.WorkspaceID)
	}
	if got.StorageKey != a.StorageKey {
		t.Errorf("storage_key = %q, want %q", got.StorageKey, a.StorageKey)
	}
	if got.Backend != "local" || got.Name != "shot.png" || got.MimeType != "image/png" {
		t.Errorf("blob metadata not preserved: %+v", got)
	}
	if got.Size != 48120 || got.Lane != domain.AttachmentLaneInlineImage || got.CreatedBy != userID {
		t.Errorf("row metadata not preserved: %+v", got)
	}
	if !got.CreatedAt.Equal(a.CreatedAt) {
		t.Errorf("created_at = %v, want the stored %v", got.CreatedAt, a.CreatedAt)
	}
}

// TestAttachmentStore_ByIDForeignWorkspaceNotFound covers the tenancy
// requirement: an id belonging to another workspace is indistinguishable
// from an unknown id — both domain.ErrNotFound.
func TestAttachmentStore_ByIDForeignWorkspaceNotFound(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID := attachmentSeed(t, ctx, s, "att-tenant-a")
	otherWs := &domain.Workspace{Slug: "att-tenant-b", Name: "WS B"}
	if err := s.Workspaces().Create(ctx, otherWs); err != nil {
		t.Fatalf("create workspace B: %v", err)
	}

	a := &domain.Attachment{WorkspaceID: wsID, StorageKey: "att/tenant-a-key", Backend: "local", Name: "f.png", MimeType: "image/png", Size: 1, Lane: domain.AttachmentLaneInlineImage, CreatedBy: userID}
	if err := s.Attachments().Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.Attachments().ByID(ctx, otherWs.ID, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign-workspace lookup: expected ErrNotFound, got %v", err)
	}
	if _, err := s.Attachments().ByID(ctx, wsID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown-id lookup: expected ErrNotFound, got %v", err)
	}
}

// TestAttachmentStore_ByStorageKeyHitAndMiss covers the global serving
// lookup: the capability key resolves without a workspace predicate; an
// unknown key returns domain.ErrNotFound.
func TestAttachmentStore_ByStorageKeyHitAndMiss(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID := attachmentSeed(t, ctx, s, "att-capkey")

	a := &domain.Attachment{WorkspaceID: wsID, StorageKey: "att/capability-bearer", Backend: "local", Name: "report.pdf", MimeType: "application/pdf", Size: 2048, Lane: domain.AttachmentLaneInlinePDF, CreatedBy: userID}
	if err := s.Attachments().Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.Attachments().ByStorageKey(ctx, "att/capability-bearer")
	if err != nil {
		t.Fatalf("by storage key: %v", err)
	}
	if got.ID != a.ID || got.WorkspaceID != wsID || got.StorageKey != a.StorageKey {
		t.Errorf("wrong attachment resolved: %+v", got)
	}

	if _, err := s.Attachments().ByStorageKey(ctx, "att/no-such-key"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown key: expected ErrNotFound, got %v", err)
	}
}

// TestAttachmentStore_CreateUnknownReferencesNotFound pins the FK parity
// with the schema: a create referencing an unknown workspace or creator
// fails with domain.ErrNotFound.
func TestAttachmentStore_CreateUnknownReferencesNotFound(t *testing.T) {
	s := fake.New()
	ctx := context.Background()
	wsID, userID := attachmentSeed(t, ctx, s, "att-fk")

	st := s.Attachments()
	mk := func(ws, by string) *domain.Attachment {
		return &domain.Attachment{WorkspaceID: ws, StorageKey: "att/k-" + ws + "-" + by, Backend: "local", Name: "f", MimeType: "text/plain", Size: 1, Lane: domain.AttachmentLaneInlineText, CreatedBy: by}
	}
	if err := st.Create(ctx, mk("00000000-0000-0000-0000-000000000000", userID)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown workspace: expected ErrNotFound, got %v", err)
	}
	if err := st.Create(ctx, mk(wsID, "00000000-0000-0000-0000-000000000000")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown creator: expected ErrNotFound, got %v", err)
	}
}
