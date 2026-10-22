package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestMCPServers_AuthRoundTrip covers the auth column (design D4): an
// OAuth-mode row survives create/read/update cycles exactly, and update
// persists auth changes.
func TestMCPServers_AuthRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	server := &store.MCPServer{
		Name:      "atlassian",
		Transport: "http",
		URL:       "https://mcp.example.com/mcp",
		Auth: store.MCPServerAuth{
			Mode:                  store.MCPAuthModeOAuth,
			ClientID:              "c1",
			Issuer:                "https://as.example",
			ResourceMetadataURL:   "https://mcp.example.com/.well-known/oauth-protected-resource/mcp",
			AuthServerMetadataURL: "https://as.example/.well-known/oauth-authorization-server",
			RedirectURI:           "http://127.0.0.1:29173/callback",
			Scopes:                []string{"read", "offline_access"},
		},
	}
	require.NoError(t, s.MCPServers().Create(ctx, server))

	got, err := s.MCPServers().ByID(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, server.Auth, got.Auth)
	require.True(t, got.Auth.IsOAuth())

	list, err := s.MCPServers().List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, server.Auth, list[0].Auth)

	got.Auth = store.MCPServerAuth{Mode: store.MCPAuthModeNone}
	require.NoError(t, s.MCPServers().Update(ctx, got))
	updated, err := s.MCPServers().ByID(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, store.MCPServerAuth{Mode: store.MCPAuthModeNone}, updated.Auth)
	require.False(t, updated.Auth.IsOAuth())
}

// TestMCPServers_ZeroAuthReadsAsNone covers the store side of the migration
// default: a row created with the zero MCPServerAuth ('{}') reads back as the
// zero value, i.e. mode none.
func TestMCPServers_ZeroAuthReadsAsNone(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	server := &store.MCPServer{Name: "filesystem", Transport: "stdio", Command: "npx"}
	require.NoError(t, s.MCPServers().Create(ctx, server))

	got, err := s.MCPServers().ByID(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, store.MCPServerAuth{}, got.Auth)
	require.False(t, got.Auth.IsOAuth())
}

// TestMCPServers_LegacyAuthRowReadsAsNone covers the migration's NOT NULL
// DEFAULT '{}': a pre-auth row (written without the auth column) reads as
// the zero MCPServerAuth through the store.
func TestMCPServers_LegacyAuthRowReadsAsNone(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	s := st.(*Store)

	db := openRaw(t, path)
	_, err = db.Exec(`INSERT INTO mcp_servers (id, name, created_at, updated_at)
VALUES ('srv-legacy', 'legacy', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`)
	require.NoError(t, err)

	got, err := s.MCPServers().ByID(ctx, "srv-legacy")
	require.NoError(t, err)
	require.Equal(t, store.MCPServerAuth{}, got.Auth)
	require.False(t, got.Auth.IsOAuth())
}
