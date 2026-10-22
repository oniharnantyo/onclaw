package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestMCPOAuthTokens_AbsentGetIsNotFound covers the absent read: a server
// with no stored token maps to the store sentinel.
func TestMCPOAuthTokens_AbsentGetIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	server := seedMCPServer(t, ctx, s)
	_, err := s.MCPOAuthTokens().Get(ctx, server.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// seedMCPServer creates one parent mcp_servers row, satisfying the token
// table's foreign key.
func seedMCPServer(t *testing.T, ctx context.Context, s *Store) *store.MCPServer {
	t.Helper()
	server := &store.MCPServer{Name: "test-server", Transport: "http", URL: "https://mcp.example.com/mcp"}
	require.NoError(t, s.MCPServers().Create(ctx, server))
	return server
}

// TestMCPOAuthTokens_SaveGetDeleteRoundTrip covers the token lifecycle: Save
// stamps UpdatedAt and round-trips the bytes, a second Save overwrites, and
// Delete is idempotent.
func TestMCPOAuthTokens_SaveGetDeleteRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tokens := s.MCPOAuthTokens()
	server := seedMCPServer(t, ctx, s)

	in := &store.MCPOAuthToken{ServerID: server.ID, Token: []byte(`{"access_token":"a1"}`)}
	require.NoError(t, tokens.Save(ctx, in))
	require.False(t, in.UpdatedAt.IsZero(), "Save must stamp UpdatedAt")

	got, err := tokens.Get(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, server.ID, got.ServerID)
	require.Equal(t, []byte(`{"access_token":"a1"}`), got.Token)
	require.Equal(t, in.UpdatedAt, got.UpdatedAt)

	require.NoError(t, tokens.Save(ctx, &store.MCPOAuthToken{ServerID: server.ID, Token: []byte(`{"access_token":"a2"}`)}))
	got, err = tokens.Get(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, []byte(`{"access_token":"a2"}`), got.Token)

	require.NoError(t, tokens.Delete(ctx, server.ID))
	_, err = tokens.Get(ctx, server.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.NoError(t, tokens.Delete(ctx, server.ID), "deleting an absent token is not an error")
}

// TestMCPOAuthTokens_CascadeOnServerDelete covers migration 000012's foreign
// key: deleting the mcp_servers row removes the server's token with it.
func TestMCPOAuthTokens_CascadeOnServerDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	server := &store.MCPServer{Name: "atlassian", Transport: "http", URL: "https://mcp.example.com/mcp"}
	require.NoError(t, s.MCPServers().Create(ctx, server))
	require.NoError(t, s.MCPOAuthTokens().Save(ctx, &store.MCPOAuthToken{
		ServerID: server.ID,
		Token:    []byte(`{"access_token":"a1"}`),
	}))

	require.NoError(t, s.MCPServers().Delete(ctx, server.ID))

	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM mcp_oauth_tokens WHERE server_id = ?`, server.ID).Scan(&n))
	require.Equal(t, 0, n, "deleting the server must cascade its token away")
	_, err := s.MCPOAuthTokens().Get(ctx, server.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestMCPOAuthTokens_ConcurrentSavesLeaveOneRow covers design D3's per-server
// mutex: concurrent saves of distinct payloads for one server are race-free
// and leave exactly one row holding one of the saved payloads.
func TestMCPOAuthTokens_ConcurrentSavesLeaveOneRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	tokens := s.MCPOAuthTokens()
	server := seedMCPServer(t, ctx, s)
	serverID := server.ID

	const writers = 16
	payloads := make([]string, 0, writers)
	for i := range writers {
		payloads = append(payloads, fmt.Sprintf(`{"access_token":"a%d"}`, i))
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = tokens.Save(ctx, &store.MCPOAuthToken{ServerID: serverID, Token: []byte(payloads[i])})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}

	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM mcp_oauth_tokens WHERE server_id = ?`, serverID).Scan(&n))
	require.Equal(t, 1, n, "concurrent saves must collapse to one row per server")

	got, err := tokens.Get(ctx, serverID)
	require.NoError(t, err)
	require.Contains(t, payloads, string(got.Token), "the surviving row must hold one of the saved payloads")
}

// TestMCPOAuthTokens_VisibleThroughTx covers the tx-scoped token store: a
// save inside WithTx commits with the transaction and shares the parent
// store's per-server locks.
func TestMCPOAuthTokens_VisibleThroughTx(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	server := &store.MCPServer{Name: "atlassian", Transport: "http", URL: "https://mcp.example.com/mcp"}
	require.NoError(t, s.MCPServers().Create(ctx, server))

	err := s.WithTx(ctx, func(tx store.Store) error {
		return tx.MCPOAuthTokens().Save(ctx, &store.MCPOAuthToken{ServerID: server.ID, Token: []byte(`{"access_token":"a1"}`)})
	})
	require.NoError(t, err)

	got, err := s.MCPOAuthTokens().Get(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, []byte(`{"access_token":"a1"}`), got.Token)
}
