package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// AttachmentStore manages workspace-scoped chat attachment records. The blob
// bytes live in a storage-port driver; rows carry the capability key and the
// backend that holds the blob so reads survive backend switches (D16).
type AttachmentStore interface {
	// Create inserts one attachment row. CreatedAt is set by the store.
	Create(ctx context.Context, a *domain.Attachment) error
	// ByID resolves an attachment scoped to its workspace. An id belonging to
	// another workspace returns domain.ErrNotFound — foreign and unknown are
	// indistinguishable (tenancy requirement).
	ByID(ctx context.Context, workspaceID, id string) (*domain.Attachment, error)
	// ByStorageKey resolves an attachment by its capability key. This lookup
	// is deliberately GLOBAL and unauthenticated: capability keys are 128-bit
	// random bearer tokens (same posture as avatar files) and are the wire
	// token for serving. Unknown keys return domain.ErrNotFound.
	ByStorageKey(ctx context.Context, key string) (*domain.Attachment, error)
}
