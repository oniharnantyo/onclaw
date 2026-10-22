package fake

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type mcpOAuthTokenStore struct{ s *Store }

// Get returns the server's token; ErrNotFound when absent.
func (t mcpOAuthTokenStore) Get(_ context.Context, serverID string) (*store.MCPOAuthToken, error) {
	t.s.mu.Lock()
	defer t.s.mu.Unlock()

	token, ok := t.s.mcpOAuthTokens[serverID]
	if !ok {
		return nil, fmt.Errorf("mcp oauth token %s: %w", serverID, store.ErrNotFound)
	}
	token.Token = cloneBytes(token.Token)
	return &token, nil
}

// Save create-or-replaces the server's token, stamping UpdatedAt (UTC) when
// the caller left it zero.
func (t mcpOAuthTokenStore) Save(_ context.Context, token *store.MCPOAuthToken) error {
	t.s.mu.Lock()
	defer t.s.mu.Unlock()

	if token.UpdatedAt.IsZero() {
		token.UpdatedAt = nowUTC()
	}
	t.s.mcpOAuthTokens[token.ServerID] = store.MCPOAuthToken{
		ServerID:  token.ServerID,
		Token:     cloneBytes(token.Token),
		UpdatedAt: token.UpdatedAt,
	}
	return nil
}

// Delete removes the server's token; absent is not an error.
func (t mcpOAuthTokenStore) Delete(_ context.Context, serverID string) error {
	t.s.mu.Lock()
	defer t.s.mu.Unlock()

	delete(t.s.mcpOAuthTokens, serverID)
	return nil
}
