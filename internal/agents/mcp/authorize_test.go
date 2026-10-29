package mcp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/stretchr/testify/require"
)

// authorizeDiscovery is the Discovery an authorize test hands to
// StartAuthorization — what a prior Discover would have validated against the
// fake authorization server.
func authorizeDiscovery(as *fakeAuthServer) Discovery {
	return Discovery{
		AuthServerMetadataURL: as.URL + "/.well-known/oauth-authorization-server",
		Scopes:                []string{"read", "write"},
		RegistrationEndpoint:  as.URL + "/register",
	}
}

// authorizeCtx bounds the synchronous part of the flows under test.
func authorizeCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 15*time.Second)
}

// awaitDone reads the flow's single Done value with a test-failure bound.
func awaitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("authorization Done did not resolve")
		return nil
	}
}

func TestStartAuthorizationEndToEnd(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	store := transport.NewMemoryTokenStore()
	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth:      OAuth{Tokens: store},
		Discovery:  authorizeDiscovery(as),
		ClientName: "buddies",
	})
	require.NoError(t, err)
	defer auth.Cancel()

	// Dynamic registration happened synchronously, for the caller to persist.
	require.Equal(t, "client-1", auth.ClientID)
	require.Equal(t, "secret-client-1", auth.ClientSecret)

	parsed, err := url.Parse(auth.URL)
	require.NoError(t, err)
	query := parsed.Query()
	require.Equal(t, "code", query.Get("response_type"))
	require.Equal(t, auth.ClientID, query.Get("client_id"))
	require.Equal(t, auth.RedirectURI, query.Get("redirect_uri"))
	require.Equal(t, "read write", query.Get("scope"))
	require.Equal(t, "S256", query.Get("code_challenge_method"))
	require.NotEmpty(t, query.Get("code_challenge"))
	state := query.Get("state")
	require.NotEmpty(t, state)

	// Simulate the authorization server redirecting back with the code.
	callback := auth.RedirectURI + "?" + url.Values{"code": {"c1"}, "state": {state}}.Encode()
	resp, err := http.Get(callback)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), "close this tab")

	// The exchange completed and saved the token into the request's store.
	require.NoError(t, awaitDone(t, auth.Done))
	_, open := <-auth.Done
	require.False(t, open) // Done closes after its single value

	token, err := store.GetToken(ctx)
	require.NoError(t, err)
	require.Equal(t, "at-good", token.AccessToken)
	require.Equal(t, "rt-good", token.RefreshToken)

	// The exchange carried the PKCE verifier and the registered redirect.
	forms := as.tokensSeen()
	require.Len(t, forms, 1)
	require.NotEmpty(t, forms[0].Get("code_verifier"))
	require.Equal(t, auth.RedirectURI, forms[0].Get("redirect_uri"))
}

func TestStartAuthorizationRejectsWrongState(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	store := transport.NewMemoryTokenStore()
	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth:     OAuth{Tokens: store},
		Discovery: authorizeDiscovery(as),
	})
	require.NoError(t, err)
	defer auth.Cancel()

	resp, err := http.Get(auth.RedirectURI + "?" + url.Values{"code": {"c1"}, "state": {"tampered"}}.Encode())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Error(t, awaitDone(t, auth.Done))
	// Nothing was saved.
	_, err = store.GetToken(ctx)
	require.ErrorIs(t, err, ErrNoToken)
}

func TestStartAuthorizationOAuthErrorRedirect(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth:     OAuth{Tokens: transport.NewMemoryTokenStore()},
		Discovery: authorizeDiscovery(as),
	})
	require.NoError(t, err)
	defer auth.Cancel()

	parsed, err := url.Parse(auth.URL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	resp, err := http.Get(auth.RedirectURI + "?" + url.Values{"error": {"access_denied"}, "state": {state}}.Encode())
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	err = awaitDone(t, auth.Done)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_denied")
}

func TestStartAuthorizationCancel(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth:     OAuth{Tokens: transport.NewMemoryTokenStore()},
		Discovery: authorizeDiscovery(as),
	})
	require.NoError(t, err)

	auth.Cancel()
	require.Error(t, awaitDone(t, auth.Done))
}

func TestStartAuthorizationReusesRegisteredClient(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth:     OAuth{ClientID: "client-static", Tokens: transport.NewMemoryTokenStore()},
		Discovery: authorizeDiscovery(as),
	})
	require.NoError(t, err)
	defer auth.Cancel()

	// A stored registration skips DCR entirely and goes straight to the
	// authorization URL.
	require.Equal(t, "client-static", auth.ClientID)
	require.Equal(t, 0, as.registrationsSeen())
	require.Contains(t, auth.URL, "client_id=client-static")
}

func TestStartAuthorizationPortTakenFallsBackAndReregisters(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	// Occupy the configured callback port and ask for exactly it.
	squatter, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer squatter.Close()
	squatted := squatter.Addr().(*net.TCPAddr).Port

	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth: OAuth{
			ClientID:    "client-stale",
			RedirectURI: fmt.Sprintf("http://127.0.0.1:%d/callback", squatted),
			Tokens:      transport.NewMemoryTokenStore(),
		},
		Discovery: authorizeDiscovery(as),
	})
	require.NoError(t, err)
	defer auth.Cancel()

	// The fallback bound an ephemeral port and re-registered against it —
	// the stale registration's redirect no longer matches (design D4).
	parsed, err := url.Parse(auth.RedirectURI)
	require.NoError(t, err)
	require.NotEqual(t, strconv.Itoa(squatted), parsed.Port())
	require.NotEqual(t, "client-stale", auth.ClientID)
	require.True(t, strings.HasPrefix(auth.ClientID, "client-"))
	require.Equal(t, 1, as.registrationsSeen())
	require.Contains(t, auth.URL, url.QueryEscape(auth.RedirectURI))
}

func TestStartAuthorizationPreRegisteredClientSurvivesPortRebind(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	// Occupy the configured callback port and ask for exactly it.
	squatter, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer squatter.Close()
	squatted := squatter.Addr().(*net.TCPAddr).Port

	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth: OAuth{
			ClientID:      "google-console-client",
			ClientSecret:  "google-console-secret",
			RedirectURI:   fmt.Sprintf("http://127.0.0.1:%d/callback", squatted),
			PreRegistered: true,
			Tokens:        transport.NewMemoryTokenStore(),
		},
		Discovery: authorizeDiscovery(as),
	})
	require.NoError(t, err)
	defer auth.Cancel()

	// A pre-registered confidential client is fixed (design D3's
	// oauth_confidential): the rewritten redirect is used as-is, the console
	// client is never discarded, and no dynamic registration runs.
	parsed, err := url.Parse(auth.RedirectURI)
	require.NoError(t, err)
	require.NotEqual(t, strconv.Itoa(squatted), parsed.Port())
	require.Equal(t, "google-console-client", auth.ClientID)
	require.Equal(t, "google-console-secret", auth.ClientSecret)
	require.Equal(t, 0, as.registrationsSeen())
	require.Contains(t, auth.URL, "client_id=google-console-client")
}

func TestStartAuthorizationExtraAuthParams(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth: OAuth{
			Tokens: transport.NewMemoryTokenStore(),
			// Google's refresh-token requirement (design D3). The scope key
			// collides with mcp-go's own scope parameter on purpose: the
			// flow's own parameter must win.
			ExtraAuthParams: map[string]string{
				"access_type": "offline",
				"prompt":      "consent",
				"scope":       "overridden",
			},
		},
		Discovery: authorizeDiscovery(as),
	})
	require.NoError(t, err)
	defer auth.Cancel()

	parsed, err := url.Parse(auth.URL)
	require.NoError(t, err)
	query := parsed.Query()
	require.Equal(t, "offline", query.Get("access_type"))
	require.Equal(t, "consent", query.Get("prompt"))
	// The URL's own parameters win over the extras.
	require.Equal(t, "read write", query.Get("scope"))
}

func TestStartAuthorizationTimeout(t *testing.T) {
	as := newFakeAuthServer(t)

	prev := authorizationTimeout
	authorizationTimeout = 50 * time.Millisecond
	defer func() { authorizationTimeout = prev }()

	ctx, cancel := authorizeCtx(t)
	defer cancel()

	store := transport.NewMemoryTokenStore()
	auth, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth:     OAuth{Tokens: store},
		Discovery: authorizeDiscovery(as),
	})
	require.NoError(t, err)
	defer auth.Cancel() // no-op after the timeout fired

	// The timeout wraps context.DeadlineExceeded and nothing was saved.
	err = awaitDone(t, auth.Done)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = store.GetToken(ctx)
	require.ErrorIs(t, err, ErrNoToken)
}

func TestStartAuthorizationRequiresTokenStore(t *testing.T) {
	_, err := StartAuthorization(context.Background(), AuthorizeRequest{})
	require.Error(t, err)
}

func TestStartAuthorizationRequiresRegistrationEndpoint(t *testing.T) {
	as := newFakeAuthServer(t)
	ctx, cancel := authorizeCtx(t)
	defer cancel()

	// Fresh client but no registration endpoint advertised: the flow cannot
	// start.
	_, err := StartAuthorization(ctx, AuthorizeRequest{
		OAuth:     OAuth{Tokens: transport.NewMemoryTokenStore()},
		Discovery: Discovery{AuthServerMetadataURL: as.URL + "/.well-known/oauth-authorization-server"},
	})
	require.Error(t, err)
}
