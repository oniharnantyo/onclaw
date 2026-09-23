package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Connections manages workspace-scoped service connections
// (add-workspace-connections): the product objects that own a materialized
// workspace MCP server through the server's origin marker. All operations are
// workspace-scoped; a connection belonging to another workspace is
// indistinguishable from an unknown id (domain.ErrNotFound). Uniqueness is per
// (workspace, service, origin) (add-recipe-base-url tasks.md 2.1): Origin is
// the resolved base-URL origin a parametrized recipe connected against — empty
// for non-parametrized recipes, where the composite key therefore collapses to
// the original one-connection-per-service rule; the same service on a
// different origin may coexist, the same (service, origin) twice returns
// domain.ErrConnectionExists.
//
// The stored connection never carries the access token: the secret lives as
// the materialized server's encrypted header row (design.md D4), so every read
// of a Connection is hint-free by construction — token hints ride the server
// row. The OAuth token lifecycle (add-connection-oauth design.md D1) DOES live
// here: the refresh-token ciphertext envelope, the access-token expiry, the
// granted scopes, and the status (including expired). Origin is deliberately
// outside every update path — it is set at Create and immutable afterwards
// (changing origins is disconnect and reconnect).
type Connections interface {
	// Create stores the connection, assigning ID and timestamps when empty.
	// An empty Status is stored as connected; the OAuth lifecycle fields
	// (RefreshCiphertext, ExpiresAt, GrantedScopes) persist as given — the
	// OAuth callback activation path creates the connection with its token
	// set in one write. Origin persists as given (empty for non-parametrized
	// recipes). A second connection for the same (workspace, service, origin)
	// returns domain.ErrConnectionExists; an unknown workspace returns
	// domain.ErrNotFound.
	Create(ctx context.Context, c *domain.Connection) error
	Get(ctx context.Context, workspaceID, id string) (*domain.Connection, error)
	List(ctx context.Context, workspaceID string) ([]domain.Connection, error)
	// GetByService resolves the workspace's connection for a service;
	// domain.ErrNotFound when the service is not connected. For a service the
	// recipe parametrizes by origin (several connections, add-recipe-base-url
	// tasks.md 2.1) the lookup is inherently ambiguous and yields a
	// deterministic row (earliest created); origin-scoped callers compare
	// List results instead.
	GetByService(ctx context.Context, workspaceID, service string) (*domain.Connection, error)
	// UpdateTokenLifecycle persists the OAuth token-lifecycle fields of an
	// existing connection — RefreshCiphertext, ExpiresAt, GrantedScopes, and
	// Status — and bumps UpdatedAt. It is the refresh write-through
	// (refresh-on-resolution, reauthorization) and the expired transition's
	// only write path; every other column is untouched. The workspace scope
	// rides c.WorkspaceID; unknown or cross-workspace connections return
	// domain.ErrNotFound.
	UpdateTokenLifecycle(ctx context.Context, c *domain.Connection) error
	// Delete removes the connection and cascades atomically (design.md D8):
	// the linked workspace MCP server row (origin_connection_id) and every
	// agent's attachment reference to it die in the same transaction, leaving
	// the token ciphertext unrecoverable. Unknown or cross-workspace ids
	// return domain.ErrNotFound.
	Delete(ctx context.Context, workspaceID, id string) error
}
