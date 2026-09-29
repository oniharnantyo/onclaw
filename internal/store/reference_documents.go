package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// ReferenceDocumentStore manages the workspace reference-document registry
// (add-reference-documents): the as-is blob's capability metadata, the tiered
// visibility scope, and the agent/channel attach lists. Every method is
// workspace-scoped — no query runs without a workspace scope — except
// GetByStorageKey, which is deliberately GLOBAL: capability keys are random
// bearer tokens and are the wire token for serving (the
// AttachmentStore.ByStorageKey posture). A foreign id is unknown:
// domain.ErrNotFound, indistinguishably.
type ReferenceDocumentStore interface {
	// Create inserts one document row, assigning ID when empty, and persists
	// the agent/channel joins from the struct. CreatedAt/UpdatedAt are set by
	// the store (invariant 4).
	Create(ctx context.Context, doc *domain.ReferenceDocument) error
	// Get resolves a document scoped to its workspace. An id belonging to
	// another workspace returns domain.ErrNotFound — foreign and unknown are
	// indistinguishable (tenancy requirement).
	Get(ctx context.Context, workspaceID, id string) (*domain.ReferenceDocument, error)
	// GetByStorageKey resolves a document by its capability key. This lookup
	// is deliberately GLOBAL and unauthenticated: the capability key is the
	// bearer token for serving. Unknown keys return domain.ErrNotFound.
	GetByStorageKey(ctx context.Context, storageKey string) (*domain.ReferenceDocument, error)
	// List returns the workspace's documents, newest first.
	List(ctx context.Context, workspaceID string) ([]domain.ReferenceDocument, error)
	// ListByChannel returns the workspace's documents whose channel join
	// contains channelID plus the promoted (scope='workspace') documents.
	ListByChannel(ctx context.Context, workspaceID, channelID string) ([]domain.ReferenceDocument, error)
	// ListByAgent returns the workspace's documents whose agent join
	// contains agentID plus the promoted (scope='workspace') documents.
	ListByAgent(ctx context.Context, workspaceID, agentID string) ([]domain.ReferenceDocument, error)
	// UpdateMeta rewrites the editable bibliographic fields (name,
	// description). An absent or foreign document returns domain.ErrNotFound.
	UpdateMeta(ctx context.Context, workspaceID, id, name, description string) error
	// SetScope flips the visibility tier (promote to workspace / demote to
	// attached); the attach lists are left as stored. Absent or foreign
	// documents return domain.ErrNotFound.
	SetScope(ctx context.Context, workspaceID, id, scope string) error
	// SetAgents replaces the agent join set (set-complete: the stored rows
	// become exactly agentIDs). Every agent must exist in the workspace —
	// unknown or foreign-workspace agents return domain.ErrNotFound.
	SetAgents(ctx context.Context, workspaceID, id string, agentIDs []string) error
	// SetChannels replaces the channel join set (set-complete). Every
	// channel must exist in the workspace — unknown or foreign-workspace
	// channels return domain.ErrNotFound.
	SetChannels(ctx context.Context, workspaceID, id string, channelIDs []string) error
	// ReplaceBlob swaps the blob identity after a re-upload: mime, size,
	// storage key, backend, page count, and the derived index status are
	// rewritten; scope and the attach lists are left as stored. The caller
	// rebuilds the section index separately (ReplaceForDocument). The new
	// storage key must not collide (domain.ErrConflict). Absent or foreign
	// documents return domain.ErrNotFound.
	ReplaceBlob(ctx context.Context, workspaceID, id string, mimeType string, sizeBytes int64, storageKey, backend string, pageCount int, indexStatus string) error
	// Delete removes the document; its join rows and sections die with it
	// (ON DELETE CASCADE). The caller deletes the blob bytes. Absent or
	// foreign documents return domain.ErrNotFound.
	Delete(ctx context.Context, workspaceID, id string) error
}

// DocumentSectionStore manages the deterministic section index behind
// document.search: one row per sectioner output slice, full-text indexed.
// All methods are workspace-scoped.
type DocumentSectionStore interface {
	// ReplaceForDocument replaces the document's entire section set in one
	// step (delete-then-insert): the stored rows become exactly sections.
	// IDs are assigned when empty. An absent or foreign document returns
	// domain.ErrNotFound. The re-upload rebuild path rides this.
	ReplaceForDocument(ctx context.Context, workspaceID, documentID string, sections []domain.DocumentSection) error
	// DeleteForDocument drops the document's sections. Absent documents
	// leave nothing to drop — no error (the caller typically just deleted
	// the document, whose rows already cascaded).
	DeleteForDocument(ctx context.Context, workspaceID, documentID string) error
	// Search runs the full-text query over the workspace's sections of the
	// given documents, best match first (relevance, then ordinal).
	// documentIDs is the run's visibility filter — an empty slice returns an
	// empty result and MUST never degrade to an unfiltered query (D7:
	// visibility is enforced at query time, never by post-filtering). An
	// empty query is invalid input. limit <= 0 means no limit (the
	// LoadSessionEventsParams convention).
	Search(ctx context.Context, workspaceID string, documentIDs []string, query string, limit int) ([]domain.DocumentSectionHit, error)
	// ListForDocument returns the document's sections in ordinal order —
	// the compose-side read the manifest's table-of-contents projection
	// (add-reference-documents D6) is built from. Workspace-scoped: an absent
	// or foreign document returns domain.ErrNotFound, indistinguishably.
	ListForDocument(ctx context.Context, workspaceID, documentID string) ([]domain.DocumentSection, error)
}
