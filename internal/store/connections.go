package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Connections manages workspace-scoped service connections
// (add-workspace-connections): the product objects that own a materialized
// workspace MCP server through the server's origin marker. All operations are
// workspace-scoped; a connection belonging to another workspace is
// indistinguishable from an unknown id (domain.ErrNotFound). Service is unique
// per workspace — one connection per service (design.md D6); violations return
// domain.ErrConnectionExists.
//
// The stored connection never carries the token: the secret lives as the
// materialized server's encrypted header row (design.md D4), so every read of
// a Connection is hint-free by construction — token hints ride the server row.
type Connections interface {
	// Create stores the connection, assigning ID and timestamps when empty.
	// A second connection for the same (workspace, service) returns
	// domain.ErrConnectionExists; an unknown workspace returns
	// domain.ErrNotFound.
	Create(ctx context.Context, c *domain.Connection) error
	Get(ctx context.Context, workspaceID, id string) (*domain.Connection, error)
	List(ctx context.Context, workspaceID string) ([]domain.Connection, error)
	// GetByService resolves the workspace's connection for a service;
	// domain.ErrNotFound when the service is not connected.
	GetByService(ctx context.Context, workspaceID, service string) (*domain.Connection, error)
	// Delete removes the connection and cascades atomically (design.md D8):
	// the linked workspace MCP server row (origin_connection_id) and every
	// agent's attachment reference to it die in the same transaction, leaving
	// the token ciphertext unrecoverable. Unknown or cross-workspace ids
	// return domain.ErrNotFound.
	Delete(ctx context.Context, workspaceID, id string) error
}
