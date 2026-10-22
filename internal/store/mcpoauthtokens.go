package store

import "context"

// MCPOAuthTokenStore persists OAuth tokens per MCP server (design D3): one
// row per server, keyed by its ID.
type MCPOAuthTokenStore interface {
	// Get returns the server's token; ErrNotFound when absent.
	Get(ctx context.Context, serverID string) (*MCPOAuthToken, error)
	// Save create-or-replaces the server's token, stamping UpdatedAt (UTC).
	Save(ctx context.Context, token *MCPOAuthToken) error
	// Delete removes the server's token; absent is not an error.
	Delete(ctx context.Context, serverID string) error
}
