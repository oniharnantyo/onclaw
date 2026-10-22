package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// The MCP OAuth adapter tests (tasks.md 5.1, design D3): the token-store
// adapter round-trips mcp.Token through one server's store row, maps
// nothing-stored onto mcp.ErrNoToken, honors context cancellation, and
// MCPServerRef hydrates the connection's OAuth settings only for OAuth-mode
// rows.

// seedMCPServerRow stores one server row in the fake store and returns it
// with its assigned ID (the token rows key on it).
func seedMCPServerRow(t *testing.T, fs *fake.Store, auth store.MCPServerAuth) store.MCPServer {
	t.Helper()
	server := store.MCPServer{Name: "github", Transport: "http", URL: "https://mcp.example/mcp", Auth: auth}
	require.NoError(t, fs.MCPServers().Create(t.Context(), &server))
	return server
}

func TestMCPTokenStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	server := seedMCPServerRow(t, fs, store.MCPServerAuth{Mode: store.MCPAuthModeOAuth})
	tokens := NewMCPTokenStore(server.ID, fs.MCPOAuthTokens())

	want := &mcp.Token{
		AccessToken:  "at-1",
		TokenType:    "Bearer",
		RefreshToken: "rt-1",
		ExpiresIn:    3600,
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}
	require.NoError(t, tokens.SaveToken(ctx, want))

	got, err := tokens.GetToken(ctx)
	require.NoError(t, err)
	require.Equal(t, want, got)

	// Save is an upsert: the second token replaces the first.
	require.NoError(t, tokens.SaveToken(ctx, &mcp.Token{AccessToken: "at-2", TokenType: "Bearer"}))
	got, err = tokens.GetToken(ctx)
	require.NoError(t, err)
	require.Equal(t, "at-2", got.AccessToken)
}

func TestMCPTokenStoreAbsentIsNoToken(t *testing.T) {
	fs := fake.New()
	server := seedMCPServerRow(t, fs, store.MCPServerAuth{})
	tokens := NewMCPTokenStore(server.ID, fs.MCPOAuthTokens())

	_, err := tokens.GetToken(t.Context())
	require.ErrorIs(t, err, mcp.ErrNoToken)

	// Delete is idempotent, so a disconnected server reads as no-token too.
	require.NoError(t, tokens.SaveToken(t.Context(), &mcp.Token{AccessToken: "at-1"}))
	require.NoError(t, fs.MCPOAuthTokens().Delete(t.Context(), server.ID))
	_, err = tokens.GetToken(t.Context())
	require.ErrorIs(t, err, mcp.ErrNoToken)
}

func TestMCPTokenStoreHonorsContext(t *testing.T) {
	fs := fake.New()
	server := seedMCPServerRow(t, fs, store.MCPServerAuth{})
	tokens := NewMCPTokenStore(server.ID, fs.MCPOAuthTokens())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tokens.GetToken(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, tokens.SaveToken(ctx, &mcp.Token{AccessToken: "at-1"}), context.Canceled)
}

func TestMCPServerRefHydratesOAuth(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	server := seedMCPServerRow(t, fs, store.MCPServerAuth{
		Mode:                  store.MCPAuthModeOAuth,
		ClientID:              "client-1",
		ClientSecret:          "secret-1",
		RedirectURI:           "http://127.0.0.1:29173/callback",
		Scopes:                []string{"read", "offline_access"},
		AuthServerMetadataURL: "https://as.example/.well-known/oauth-authorization-server",
	})

	ref := MCPServerRef(server, fs.MCPOAuthTokens())
	require.True(t, ref.Conn.OAuth.Enabled)
	require.Equal(t, "client-1", ref.Conn.OAuth.ClientID)
	require.Equal(t, "secret-1", ref.Conn.OAuth.ClientSecret)
	require.Equal(t, "http://127.0.0.1:29173/callback", ref.Conn.OAuth.RedirectURI)
	require.Equal(t, []string{"read", "offline_access"}, ref.Conn.OAuth.Scopes)
	require.Equal(t, "https://as.example/.well-known/oauth-authorization-server", ref.Conn.OAuth.AuthServerMetadataURL)

	// The token store is wired to this server's row: nothing stored yet.
	_, err := ref.Conn.OAuth.Tokens.GetToken(ctx)
	require.ErrorIs(t, err, mcp.ErrNoToken)

	// The hydrated scopes are a clone: mutating the row afterwards must not
	// reach the ref.
	server.Auth.Scopes[0] = "mutated"
	require.Equal(t, []string{"read", "offline_access"}, ref.Conn.OAuth.Scopes)
}

func TestMCPServerRefNoneModeKeepsStaticHeaders(t *testing.T) {
	fs := fake.New()
	server := seedMCPServerRow(t, fs, store.MCPServerAuth{})

	ref := MCPServerRef(server, fs.MCPOAuthTokens())
	require.False(t, ref.Conn.OAuth.Enabled)
	require.Nil(t, ref.Conn.OAuth.Tokens)
}
