package store

import (
	"context"
	"time"
)

// Document is one knowledge-corpus document's catalog row (add-knowledge-
// corpus design D2/D6). The bytes live on disk under the document's scope
// subtree of the knowledge directory; this row is the catalog the surfaces
// list and the store's scope indexes are built from. Scope is "global" or
// "agent" and Source is "user" or "agent" — the raw string values of the
// knowledge package's constants (the store layer does not import it). AgentID
// and AgentSlug are empty on global rows.
type Document struct {
	ID          string
	Filename    string
	MIME        string
	Size        int64
	Scope       string // "global" | "agent"
	AgentID     string // "" when global
	AgentSlug   string // "" when global
	Description string
	Source      string // "user" | "agent"
	CreatedAt   time.Time
}

// DocumentStore manages the knowledge document catalog: one row per stored
// corpus document, keyed by a store-assigned id. Filename uniqueness is
// scoped — (scope, agent-slug-when-agent, filename) is unique — and enforced
// both here (UNIQUE constraint) and by the knowledge storage layer's
// DocumentByName precheck.
type DocumentStore interface {
	// Put inserts one document's catalog row, assigning ID and stamping
	// CreatedAt when empty. A duplicate (scope, agent slug, filename) is
	// rejected with ErrDuplicate; an unknown agent_id with the driver's
	// foreign-key error wrapped.
	PutDocument(ctx context.Context, d Document) (Document, error)
	// List returns every document row, sorted by filename. The result is
	// never nil.
	ListDocuments(ctx context.Context) ([]Document, error)
	// Get returns one document row; ErrNotFound when absent.
	GetDocument(ctx context.Context, id string) (Document, error)
	// DocumentByName resolves one scope's row by filename; found is false
	// when the scope holds no such filename.
	DocumentByName(ctx context.Context, scope, agentSlug, filename string) (Document, bool, error)
	// UpdateDocumentScope moves one row between scopes (the storage layer
	// moves the file alongside); ErrNotFound when absent.
	UpdateDocumentScope(ctx context.Context, id, scope, agentID, agentSlug string) error
	// UpdateDocumentDescription rewrites one row's description; ErrNotFound
	// when absent.
	UpdateDocumentDescription(ctx context.Context, id, description string) error
	// Delete removes one document's row; ErrNotFound when absent.
	DeleteDocument(ctx context.Context, id string) error
}
