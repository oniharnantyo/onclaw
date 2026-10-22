package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
)

// mcpTokenStore adapts one server's row of the store's OAuth token table onto
// the MCP client's TokenStore (design D3). The row's opaque Token column is
// the JSON encoding of mcp.Token; nothing-stored maps onto mcp.ErrNoToken so
// mcp-go's transports treat it as the no-token state, not an operational
// failure.
type mcpTokenStore struct {
	serverID string
	tokens   store.MCPOAuthTokenStore
}

// NewMCPTokenStore adapts one server's row of tokens onto the MCP client's
// TokenStore (design D3): the mcp package accepts data, not the store, so
// this adapter is the seam's other half — injected into the ref hydration
// (MCPServerRef) and the settings service's authorization flow alike.
func NewMCPTokenStore(serverID string, tokens store.MCPOAuthTokenStore) mcp.TokenStore {
	return mcpTokenStore{serverID: serverID, tokens: tokens}
}

// GetToken returns the server's stored token. Per mcp-go's TokenStore
// contract it honors ctx first, reports nothing-stored as mcp.ErrNoToken, and
// wraps every other failure.
func (t mcpTokenStore) GetToken(ctx context.Context) (*mcp.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	row, err := t.tokens.Get(ctx, t.serverID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, mcp.ErrNoToken
		}
		return nil, fmt.Errorf("get mcp oauth token %s: %w", t.serverID, err)
	}
	var token mcp.Token
	if err := json.Unmarshal(row.Token, &token); err != nil {
		return nil, fmt.Errorf("decode mcp oauth token %s: %w", t.serverID, err)
	}
	return &token, nil
}

// SaveToken stores the token as the row's JSON encoding, creating or
// replacing the server's token row.
func (t mcpTokenStore) SaveToken(ctx context.Context, token *mcp.Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("encode mcp oauth token %s: %w", t.serverID, err)
	}
	if err := t.tokens.Save(ctx, &store.MCPOAuthToken{ServerID: t.serverID, Token: encoded}); err != nil {
		return fmt.Errorf("save mcp oauth token %s: %w", t.serverID, err)
	}
	return nil
}
