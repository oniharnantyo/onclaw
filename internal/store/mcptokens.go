package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// MCPTokens manages the MCP servers' stored OAuth token sets
// (add-mcp-oauth-client design.md D4): one row per oauth-mode server, keyed
// by scope kind (workspace | agent) + server id, holding the encrypted
// access/refresh envelopes, the expiry, the granted scopes, and the issuing
// authorization server. The rows are deliberately separate from the
// user-visible header/env rows so the UI's secret-row model (hint/last-4,
// manual edit) stays meaningful while refresh mutates the tokens invisibly;
// reads are presence-only and the envelope contents never serialize to
// clients.
//
// Every method is workspace-scoped (the tenant partition); a row belonging
// to another workspace is indistinguishable from an unknown id
// (domain.ErrNotFound). agentID selects the scope kind: empty means a
// workspace-scoped server, non-empty means an agent-private server of that
// agent. A server row's deletion cascades its token row (workspace deletes,
// agent deletes, and connection disconnects included); a server id without a
// token row is simply absent (domain.ErrNotFound).
type MCPTokens interface {
	// Replace create-or-replaces the server row's single token set: the
	// reauthorization and refresh write paths ride it, and both replace the
	// whole row in one write (created_at is fixed at birth, updated_at
	// advances). An unknown or cross-workspace workspace-scope server, or an
	// agent-scope server outside the workspace/agent pair, returns
	// domain.ErrNotFound.
	Replace(ctx context.Context, t *domain.MCPToken) error
	// Get returns the server row's token set; domain.ErrNotFound when the
	// server has none.
	Get(ctx context.Context, workspaceID, agentID, serverID string) (*domain.MCPToken, error)
	// GetForUpdate returns the token set under a row lock so a
	// read-refresh-write sequence inside store.WithTx cannot interleave with
	// a concurrent reauthorization. Outside a transaction the lock ends with
	// the statement (the callers refresh on-dial and accept the race); the
	// fake holds its global lock across WithTx, so it is a plain Get there.
	GetForUpdate(ctx context.Context, workspaceID, agentID, serverID string) (*domain.MCPToken, error)
	// Delete removes the server row's token set. Absence is
	// domain.ErrNotFound. The cascades make this an explicit-teardown
	// primitive, not the only deletion path.
	Delete(ctx context.Context, workspaceID, agentID, serverID string) error
}
