package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"
)

// discoveryCtx bounds every Discover/connect call in these tests.
func discoveryCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 15*time.Second)
}

func TestDiscoverPathScopedMetadata(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	// The Atlassian shape: the challenge names only the path-suffixed
	// document, and discovery follows it all the way to the authorization
	// server's endpoints.
	d, err := Discover(ctx, TransportStreamableHTTP, fx.mcpServer.URL+"/mcp", map[string]string{"X-Buddies-Test": "hdr-123"})
	require.NoError(t, err)
	require.Equal(t, fx.expectedDiscovery(), *d)

	// The probe is the documented shape: a POST initialize carrying the
	// connection's configured headers.
	method, accept, header := fx.guard.lastProbe()
	require.Equal(t, http.MethodPost, method)
	require.Contains(t, accept, "text/event-stream")
	require.Equal(t, "hdr-123", header.Get("X-Buddies-Test"))
}

func TestDiscoverOIDCOnlyAuthServer(t *testing.T) {
	fx := newOAuthFixture(t)
	fx.auth.oidcOnly = true // RFC 8414 path 404s; only openid-configuration is served
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	d, err := Discover(ctx, TransportStreamableHTTP, fx.mcpServer.URL+"/mcp", nil)
	require.NoError(t, err)
	require.Equal(t, fx.auth.URL, d.Issuer)
	require.Equal(t, fx.auth.URL+"/.well-known/openid-configuration", d.AuthServerMetadataURL)
	require.Equal(t, fx.auth.URL+"/token", d.TokenEndpoint)
}

func TestDiscoverChallengeWithoutResourceMetadata(t *testing.T) {
	fx := newOAuthFixture(t)
	fx.guard.bareChallenge = true
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := Discover(ctx, TransportStreamableHTTP, fx.mcpServer.URL+"/mcp", nil)
	require.ErrorIs(t, err, ErrNoOAuth)
}

func TestDiscoverServerWithoutOAuth(t *testing.T) {
	srv := httptest.NewServer(mcpserver.NewStreamableHTTPServer(newStubServer()))
	defer srv.Close()
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := Discover(ctx, TransportStreamableHTTP, srv.URL+"/mcp", nil)
	require.ErrorIs(t, err, ErrNoOAuth)
}

func TestDiscoverResourceWithoutAuthorizationServers(t *testing.T) {
	fx := newOAuthFixture(t)
	fx.guard.emptyServers = true
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := Discover(ctx, TransportStreamableHTTP, fx.mcpServer.URL+"/mcp", nil)
	require.ErrorIs(t, err, ErrNoOAuth)
}

func TestDiscoverBrokenAuthServerMetadataIsNotNoOAuth(t *testing.T) {
	fx := newOAuthFixture(t)
	fx.auth.metadata404 = true // both candidate metadata documents 404
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := Discover(ctx, TransportStreamableHTTP, fx.mcpServer.URL+"/mcp", nil)
	require.Error(t, err)
	// The server advertises OAuth but its metadata is broken: a failure, not
	// "no OAuth" (design D5 keeps such servers out of a plain error row).
	require.NotErrorIs(t, err, ErrNoOAuth)
}

func TestDiscoverSSERequestsGet(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	d, err := Discover(ctx, TransportSSE, fx.mcpServer.URL+"/mcp", nil)
	require.NoError(t, err)
	require.Equal(t, fx.auth.URL, d.Issuer)

	method, accept, _ := fx.guard.lastProbe()
	require.Equal(t, http.MethodGet, method)
	require.Equal(t, "text/event-stream", accept)
}

func TestDiscoverUnsupportedTransport(t *testing.T) {
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := Discover(ctx, TransportStdio, "http://127.0.0.1:1/mcp", nil)
	require.ErrorIs(t, err, ErrUnsupportedTransport)
}

func TestFetchAuthServerMetadata(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	md, err := FetchAuthServerMetadata(ctx, fx.auth.URL+"/.well-known/oauth-authorization-server")
	require.NoError(t, err)
	require.Equal(t, fx.auth.URL, md.Issuer)
	require.Equal(t, fx.auth.URL+"/token", md.TokenEndpoint)
	require.Equal(t, fx.auth.URL+"/revoke", md.RevocationEndpoint)

	// A document missing the endpoints buddies requires does not validate.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://as.test"}`))
	}))
	defer srv.Close()
	_, err = FetchAuthServerMetadata(ctx, srv.URL)
	require.Error(t, err)
}

func TestRevokeToken(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	require.NoError(t, RevokeToken(ctx, fx.auth.URL+"/revoke", "rt-1", "refresh_token", "client-1", ""))
	forms := fx.auth.revocationsSeen()
	require.Len(t, forms, 1)
	require.Equal(t, "rt-1", forms[0].Get("token"))
	require.Equal(t, "refresh_token", forms[0].Get("token_type_hint"))
	require.Equal(t, "client-1", forms[0].Get("client_id"))
	require.Empty(t, forms[0].Get("client_secret"))

	// Non-2xx is an error (callers treat it as best-effort anyway).
	require.Error(t, RevokeToken(ctx, fx.auth.URL+"/missing", "rt-1", "", "", ""))
}

func TestAuthRequiredErrorClassification(t *testing.T) {
	cause := errors.New("boom")
	err := &AuthRequiredError{Cause: cause}
	require.ErrorIs(t, err, ErrAuthRequired)
	require.ErrorIs(t, err, cause)
	require.Equal(t, "mcp server requires authorization: boom", err.Error())
}

func TestStatusAuthRequiredValue(t *testing.T) {
	// The store's status column and the frontend chip both key off the
	// literal; a rename here is a cross-layer contract change.
	require.Equal(t, "auth_required", StatusAuthRequired)
}

// ---------------------------------------------------------------------------
// Dial + classification (design D5): connect maps an OAuth-protected server's
// failures onto ErrAuthRequired, and leaves everything else alone.
// ---------------------------------------------------------------------------

// oauthConn is an OAuth-enabled streamable HTTP connection to the fixture.
func oauthConn(fx *oauthFixture, store TokenStore) Connection {
	return Connection{
		Transport: TransportStreamableHTTP,
		URL:       fx.mcpServer.URL + "/mcp",
		OAuth: OAuth{
			Enabled:               true,
			ClientID:              "client-static",
			Tokens:                store,
			AuthServerMetadataURL: fx.auth.URL + "/.well-known/oauth-authorization-server",
		},
	}
}

func TestConnectOAuthWithoutTokenStoreFailsFast(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := connect(ctx, Connection{Transport: TransportStreamableHTTP, URL: fx.mcpServer.URL + "/mcp", OAuth: OAuth{Enabled: true}})
	require.Error(t, err)
	// A wiring bug, not a server state: never auth_required.
	require.NotErrorIs(t, err, ErrAuthRequired)
}

func TestConnectOAuthValidTokenListsTools(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	store := transport.NewMemoryTokenStore()
	require.NoError(t, store.SaveToken(ctx, &Token{AccessToken: "good", TokenType: "Bearer"}))

	ready, err := connect(ctx, oauthConn(fx, store))
	require.NoError(t, err)
	defer func() { _ = ready.client.Close() }()
	require.Contains(t, listedToolNames(t, ctx, ready), "test_tool")
}

func TestConnectOAuthEmptyStoreIsAuthRequired(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := connect(ctx, oauthConn(fx, transport.NewMemoryTokenStore()))
	var authReq *AuthRequiredError
	require.ErrorAs(t, err, &authReq)
	require.ErrorIs(t, err, ErrAuthRequired)
	// The OAuth transport reported the need itself: no discovery to carry.
	require.Nil(t, authReq.Discovery)
}

func TestConnectOAuthExpiredTokenRefreshRejected(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	store := transport.NewMemoryTokenStore()
	require.NoError(t, store.SaveToken(ctx, &Token{
		AccessToken:  "stale",
		TokenType:    "Bearer",
		RefreshToken: "rt-stale",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}))

	_, err := connect(ctx, oauthConn(fx, store))
	var authReq *AuthRequiredError
	require.ErrorAs(t, err, &authReq)
	require.ErrorIs(t, err, ErrAuthRequired)
	require.Nil(t, authReq.Discovery)

	// The transport tried the refresh first and the authorization server
	// rejected it.
	forms := fx.auth.tokensSeen()
	require.NotEmpty(t, forms)
	require.Equal(t, "refresh_token", forms[0].Get("grant_type"))
}

func TestConnectOAuthExpiredTokenRefreshReconnects(t *testing.T) {
	fx := newOAuthFixture(t)
	fx.auth.refreshOK = true // the authorization server honors refresh grants
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	store := transport.NewMemoryTokenStore()
	require.NoError(t, store.SaveToken(ctx, &Token{
		AccessToken:  "stale",
		TokenType:    "Bearer",
		RefreshToken: "rt-stale",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}))

	// The expired stored token refreshes headlessly: no browser, no
	// authorization flow — the connection just comes up.
	ready, err := connect(ctx, oauthConn(fx, store))
	require.NoError(t, err)
	defer func() { _ = ready.client.Close() }()
	require.Contains(t, listedToolNames(t, ctx, ready), "test_tool")

	// The refresh grant reached the authorization server carrying the stored
	// refresh token.
	forms := fx.auth.tokensSeen()
	require.NotEmpty(t, forms)
	require.Equal(t, "refresh_token", forms[0].Get("grant_type"))
	require.Equal(t, "rt-stale", forms[0].Get("refresh_token"))

	// The refreshed token set is persisted: new access token, rotated
	// refresh token, and a fresh expiry.
	saved, err := store.GetToken(ctx)
	require.NoError(t, err)
	require.Equal(t, "good", saved.AccessToken)
	require.Equal(t, "rt-refreshed", saved.RefreshToken)
	require.True(t, saved.ExpiresAt.After(time.Now()))
}

func TestConnectNoneModeWithAdvertisementIsAuthRequired(t *testing.T) {
	fx := newOAuthFixture(t)
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := connect(ctx, Connection{Transport: TransportStreamableHTTP, URL: fx.mcpServer.URL + "/mcp"})
	var authReq *AuthRequiredError
	require.ErrorAs(t, err, &authReq)
	require.ErrorIs(t, err, ErrAuthRequired)
	// None mode learned the whole advertisement from the unauthenticated 401.
	require.NotNil(t, authReq.Discovery)
	require.Equal(t, fx.expectedDiscovery(), *authReq.Discovery)
	// The original dial failure is preserved as the cause.
	require.ErrorContains(t, err, "status 401")
}

func TestConnectNoneModePlain401StaysError(t *testing.T) {
	fx := newOAuthFixture(t)
	fx.guard.bareChallenge = true
	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := connect(ctx, Connection{Transport: TransportStreamableHTTP, URL: fx.mcpServer.URL + "/mcp"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAuthRequired)
	var authReq *AuthRequiredError
	require.NotErrorAs(t, err, &authReq)
}

func TestConnectNoneModeServerDownStaysError(t *testing.T) {
	// Reserve a port, then close the listener: nothing is listening there.
	srv := httptest.NewServer(http.NewServeMux())
	url := srv.URL
	srv.Close()

	ctx, cancel := discoveryCtx(t)
	defer cancel()

	_, err := connect(ctx, Connection{Transport: TransportStreamableHTTP, URL: url + "/mcp"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAuthRequired)
}
