package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// The MCP OAuth flow tests (tasks.md 5.2–5.4, design D2/D4/D5/D6/D8/D9): one
// in-process httptest deployment — an OAuth-protected streamable MCP endpoint
// (the Atlassian shape: a path-suffixed protected-resource document behind
// the 401's challenge), the authorization server's metadata, dynamic
// registration, authorization-code tokens, and revocation — driven through
// the settings service with a fake store, a recording opener, and a
// recording emitter.

// oauthFlowFixture is that deployment. The MCP endpoint answers the full
// streamable handshake once the bearer token matches, so the happy path's
// post-authorization probe connects for real.
type oauthFlowFixture struct {
	*httptest.Server

	// inner is the protected MCP endpoint, created once: the streamable
	// transport is stateful (sessions), so a per-request instance would
	// break the handshake's second leg.
	inner http.Handler

	// order, when set, records a "dial" per MCP-endpoint request so tests
	// can assert ordering against the service's invalidation log.
	order *eventLog

	mu                 sync.Mutex
	registrations      int
	tokenForms         []url.Values
	revokeForms        []url.Values
	noRevocationAdvert bool // metadata omits revocation_endpoint
	revocationFails    bool // /revoke answers 500
}

func newOAuthFlowFixture(t *testing.T) *oauthFlowFixture {
	t.Helper()
	fx := &oauthFlowFixture{inner: newFlowStubHandler()}
	fx.Server = httptest.NewServer(fx)
	t.Cleanup(fx.Close)
	return fx
}

func (f *oauthFlowFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/mcp":
		if f.order != nil {
			f.order.append("dial")
		}
		if r.Header.Get("Authorization") != "Bearer at-good" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer error="invalid_token", resource_metadata=%q`,
				f.URL+"/.well-known/oauth-protected-resource/mcp"))
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
			return
		}
		f.inner.ServeHTTP(w, r)
	case "/.well-known/oauth-protected-resource/mcp":
		f.writeJSON(w, map[string]any{
			"resource":              f.URL + "/mcp",
			"authorization_servers": []string{f.URL},
			"scopes_supported":      []string{"read", "write", "offline_access"},
		})
	case "/.well-known/oauth-authorization-server":
		metadata := map[string]any{
			"issuer":                 f.URL,
			"authorization_endpoint": f.URL + "/authorize",
			"token_endpoint":         f.URL + "/token",
			"registration_endpoint":  f.URL + "/register",
			"scopes_supported":       []string{"read", "write"},
		}
		if !f.noRevocationAdvert {
			metadata["revocation_endpoint"] = f.URL + "/revoke"
		}
		f.writeJSON(w, metadata)
	case "/register":
		f.mu.Lock()
		f.registrations++
		id := fmt.Sprintf("client-%d", f.registrations)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"client_id": id, "client_secret": "secret-" + id})
	case "/token":
		_ = r.ParseForm()
		f.mu.Lock()
		f.tokenForms = append(f.tokenForms, cloneFormValues(r.PostForm))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("grant_type") != "authorization_code" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-good",
			"token_type":    "Bearer",
			"refresh_token": "rt-good",
			"expires_in":    3600,
		})
	case "/revoke":
		_ = r.ParseForm()
		f.mu.Lock()
		f.revokeForms = append(f.revokeForms, cloneFormValues(r.PostForm))
		f.mu.Unlock()
		if f.revocationFails {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func (f *oauthFlowFixture) writeJSON(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *oauthFlowFixture) registrationsSeen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registrations
}

func (f *oauthFlowFixture) revocationsSeen() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.revokeForms)
}

// newFlowStubHandler builds the fixture's protected MCP endpoint: a
// streamable server exposing one tool, the shape the post-authorization
// probe discovers.
func newFlowStubHandler() http.Handler {
	s := mcpserver.NewMCPServer("buddies-flow-mock", "1.0.0")
	s.AddTool(gomcp.NewTool("test_tool"), func(_ context.Context, _ gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		return gomcp.NewToolResultText("tool result"), nil
	})
	return mcpserver.NewStreamableHTTPServer(s)
}

// cloneFormValues snapshots a parsed form (url.Values.Clone needs go1.27).
func cloneFormValues(form url.Values) url.Values {
	out := make(url.Values, len(form))
	for name, values := range form {
		out[name] = slices.Clone(values)
	}
	return out
}

// freeCallbackURI binds 127.0.0.1:0 to learn a free port, releases it, and
// returns a loopback callback URI on that port — tests never share the fixed
// DefaultRedirectURI port.
func freeCallbackURI(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return fmt.Sprintf("http://127.0.0.1:%d/callback", port)
}

// eventLog records tagged events under one mutex so tests can assert the
// order between the service's invalidation and the fixture's dial.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) append(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// oauthHTTPServerRow builds an enabled streamable HTTP server row for the
// fixture's protected endpoint, auth mode none (static headers only).
func oauthHTTPServerRow(name, url string) store.MCPServer {
	return store.MCPServer{
		Name:      name,
		Transport: mcp.TransportStreamableHTTP,
		URL:       url,
		Enabled:   true,
	}
}

// awaitOAuthCompleted polls the emitter until a new OAuth completion event
// arrives (the flow's goroutine is asynchronous), with a test-failure bound.
// seen is the event count already observed — only events beyond it count.
func awaitOAuthCompleted(t *testing.T, emitter *recordingEmitter, seen int) MCPOAuthCompletedEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		events := emitter.recorded()
		for _, event := range events[seen:] {
			if event.Name != EventMCPOAuthCompleted {
				continue
			}
			require.Len(t, event.Data, 1)
			payload, ok := event.Data[0].(MCPOAuthCompletedEvent)
			require.True(t, ok, "completion payload must be an MCPOAuthCompletedEvent")
			return payload
		}
		if time.Now().After(deadline) {
			t.Fatal("mcp:oauth:completed never arrived")
			return MCPOAuthCompletedEvent{}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// finishAuthorization simulates the authorization server redirecting the
// browser back with the code, reading the state off the latest recorded URL.
func finishAuthorization(t *testing.T, opener *recordingOpener, redirectURI string) {
	t.Helper()
	require.NotEmpty(t, opener.urls)
	parsed, err := url.Parse(opener.urls[len(opener.urls)-1])
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)
	callback := redirectURI + "?" + url.Values{"code": {"c1"}, "state": {state}}.Encode()
	resp, err := http.Get(callback)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

// ---------------------------------------------------------------------------
// AuthorizeMCPServer (design D2/D4/D8).
// ---------------------------------------------------------------------------

// TestAuthorizeMCPServerHappyPath is the end-to-end acceptance path (spec
// scenario "First authorization connects the server"): bounded discovery,
// dynamic registration persisted before the browser opens, the code exchange
// saving tokens, the out-of-band re-probe connecting for real, and the
// completion event announced only after the row reflects it.
func TestAuthorizeMCPServerHappyPath(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	emitter := &recordingEmitter{}
	opener := &recordingOpener{}
	cache := &recordingMCPCache{}
	svc := NewSettingsService(fs, WithEmitter(emitter), WithURLOpener(opener), WithMCPInvalidate(cache.invalidate))
	fx := newOAuthFlowFixture(t)

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)
	created.Auth.RedirectURI = freeCallbackURI(t)
	require.NoError(t, fs.MCPServers().Update(ctx, &created))

	require.NoError(t, svc.AuthorizeMCPServer(ctx, created.ID))

	// The browser URL carries the registered client and the PKCE challenge.
	require.Len(t, opener.urls, 1)
	parsed, err := url.Parse(opener.urls[0])
	require.NoError(t, err)
	require.Contains(t, opener.urls[0], "client_id=client-1")
	require.Equal(t, "code", parsed.Query().Get("response_type"))
	require.Equal(t, "S256", parsed.Query().Get("code_challenge_method"))

	// The registration was persisted before the browser opened (design D4);
	// the status fields are untouched until the flow completes.
	stored, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, store.MCPAuthModeOAuth, stored.Auth.Mode)
	require.Equal(t, "client-1", stored.Auth.ClientID)
	require.Equal(t, "secret-client-1", stored.Auth.ClientSecret)
	require.Equal(t, fx.URL, stored.Auth.Issuer)
	require.Equal(t, fx.URL+"/.well-known/oauth-authorization-server", stored.Auth.AuthServerMetadataURL)
	require.Equal(t, fx.URL+"/.well-known/oauth-protected-resource/mcp", stored.Auth.ResourceMetadataURL)
	require.Equal(t, []string{"read", "write", "offline_access"}, stored.Auth.Scopes)
	require.Empty(t, stored.Status)
	require.Equal(t, 0, stored.ToolCount)

	// Simulate the browser round-trip and await the out-of-band completion.
	finishAuthorization(t, opener, stored.Auth.RedirectURI)
	completed := awaitOAuthCompleted(t, emitter, len(emitter.recorded()))
	require.Equal(t, created.ID, completed.ServerID)
	require.True(t, completed.OK)
	require.Empty(t, completed.Error)

	// The exchanged tokens were persisted through the adapter.
	row, err := fs.MCPOAuthTokens().Get(ctx, created.ID)
	require.NoError(t, err)
	var saved mcp.Token
	require.NoError(t, json.Unmarshal(row.Token, &saved))
	require.Equal(t, "at-good", saved.AccessToken)
	require.Equal(t, "rt-good", saved.RefreshToken)

	// The completion re-probed with the fresh tokens: connected, one tool.
	stored, err = fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, stored.StatusError, "the post-authorization probe failed")
	require.Equal(t, mcp.StatusConnected, stored.Status)
	require.Equal(t, 1, stored.ToolCount)

	// Only the re-probe invalidated the cache — the flow's start never did.
	require.Equal(t, []string{created.ID}, cache.invalidated())
}

// TestAuthorizeMCPServerNoBrowser pins the headless-host path (design D8):
// the flow is cancelled, the row is auth_required with the desktop-app
// message, nothing was saved, no event fired, and the in-flight slot is
// free for a retry.
func TestAuthorizeMCPServerNoBrowser(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	emitter := &recordingEmitter{}
	svc := NewSettingsService(fs, WithEmitter(emitter), WithURLOpener(NoBrowser{}))
	fx := newOAuthFlowFixture(t)

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)
	created.Auth.RedirectURI = freeCallbackURI(t)
	require.NoError(t, fs.MCPServers().Update(ctx, &created))

	err = svc.AuthorizeMCPServer(ctx, created.ID)
	require.ErrorIs(t, err, ErrNoBrowser)

	stored, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, mcp.StatusAuthRequired, stored.Status)
	require.Equal(t, ErrNoBrowser.Error(), stored.StatusError)
	require.Equal(t, 0, stored.ToolCount)
	// The registration persisted before the failed open survives (design D4).
	require.Equal(t, store.MCPAuthModeOAuth, stored.Auth.Mode)
	require.NotEmpty(t, stored.Auth.ClientID)

	require.Empty(t, emitter.recorded())
	_, err = fs.MCPOAuthTokens().Get(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	// The slot was released: the retry fails on the browser again, not on
	// the in-flight guard.
	err = svc.AuthorizeMCPServer(ctx, created.ID)
	require.ErrorIs(t, err, ErrNoBrowser)
}

// TestAuthorizeMCPServerSecondCallInProgress pins the one-in-flight guard:
// after the synchronous part returns the browser round-trip is still
// pending, so a second authorization of the same server is rejected.
func TestAuthorizeMCPServerSecondCallInProgress(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	emitter := &recordingEmitter{}
	opener := &recordingOpener{}
	svc := NewSettingsService(fs, WithEmitter(emitter), WithURLOpener(opener))
	fx := newOAuthFlowFixture(t)

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)
	created.Auth.RedirectURI = freeCallbackURI(t)
	require.NoError(t, fs.MCPServers().Update(ctx, &created))

	require.NoError(t, svc.AuthorizeMCPServer(ctx, created.ID))
	err = svc.AuthorizeMCPServer(ctx, created.ID)
	require.ErrorContains(t, err, "authorization already in progress")

	// Finish the first flow so its goroutine does not outlive the test.
	stored, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	finishAuthorization(t, opener, stored.Auth.RedirectURI)
	require.True(t, awaitOAuthCompleted(t, emitter, len(emitter.recorded())).OK)
}

// TestAuthorizeMCPServerIssuerChangeClearsRegistration pins the registration
// reuse rule (design D4): a stored registration minted by another
// authorization server is cleared and the flow re-registers.
func TestAuthorizeMCPServerIssuerChangeClearsRegistration(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	emitter := &recordingEmitter{}
	opener := &recordingOpener{}
	svc := NewSettingsService(fs, WithEmitter(emitter), WithURLOpener(opener))
	fx := newOAuthFlowFixture(t)

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)
	created.Auth = store.MCPServerAuth{
		Mode:         store.MCPAuthModeOAuth,
		Issuer:       "https://other.example",
		ClientID:     "old-client",
		ClientSecret: "old-secret",
	}
	require.NoError(t, fs.MCPServers().Update(ctx, &created))

	require.NoError(t, svc.AuthorizeMCPServer(ctx, created.ID))

	stored, err := fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, fx.URL, stored.Auth.Issuer, "the discovered issuer replaced the stale one")
	require.NotEqual(t, "old-client", stored.Auth.ClientID, "the foreign registration was cleared")
	require.NotEmpty(t, stored.Auth.ClientID)
	require.Contains(t, opener.urls[0], "client_id="+stored.Auth.ClientID)
	require.Equal(t, 1, fx.registrationsSeen())

	// Complete the first flow (against the default callback it registered)
	// so its goroutine does not outlive the test.
	finishAuthorization(t, opener, stored.Auth.RedirectURI)
	require.True(t, awaitOAuthCompleted(t, emitter, len(emitter.recorded())).OK)

	// The released slot takes a second flow, reusing the fresh registration.
	stored.Auth.RedirectURI = freeCallbackURI(t)
	require.NoError(t, fs.MCPServers().Update(ctx, stored))
	require.NoError(t, svc.AuthorizeMCPServer(ctx, created.ID))
	require.Equal(t, 1, fx.registrationsSeen(), "the fresh registration is reused, not re-registered")
	stored, err = fs.MCPServers().ByID(ctx, created.ID)
	require.NoError(t, err)
	finishAuthorization(t, opener, stored.Auth.RedirectURI)
	require.True(t, awaitOAuthCompleted(t, emitter, len(emitter.recorded())).OK)
}

// TestAuthorizeMCPServerStdioUnsupported pins the transport gate: stdio has
// no OAuth (design D1's scope).
func TestAuthorizeMCPServerStdioUnsupported(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	created, err := svc.CreateMCPServer(ctx, store.MCPServer{
		Name: "local", Transport: mcp.TransportStdio, Command: "/bin/true", Enabled: true,
	})
	require.NoError(t, err)

	err = svc.AuthorizeMCPServer(ctx, created.ID)
	require.ErrorContains(t, err, `transport "stdio" does not support oauth`)
}

// TestAuthorizeMCPServerNoOAuthAdvertised pins the discovery gate: a server
// that does not advertise OAuth protection (no 401 here) is an error, and
// the in-flight slot is released for a later attempt.
func TestAuthorizeMCPServerNoOAuthAdvertised(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer plain.Close()

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("plain", plain.URL+"/mcp"))
	require.NoError(t, err)

	err = svc.AuthorizeMCPServer(ctx, created.ID)
	require.ErrorContains(t, err, "does not advertise oauth")

	// The guard was released: a retry fails on discovery again, not on the
	// in-flight guard.
	err = svc.AuthorizeMCPServer(ctx, created.ID)
	require.ErrorContains(t, err, "does not advertise oauth")
}

// ---------------------------------------------------------------------------
// DisconnectMCPServer (design D9).
// ---------------------------------------------------------------------------

// seedOAuthSession stores an OAuth-mode row carrying a stored token set, the
// shape Disconnect finds after a completed authorization.
func seedOAuthSession(t *testing.T, fs *fake.Store, svc *SettingsService, fx *oauthFlowFixture) store.MCPServer {
	t.Helper()
	created, err := svc.CreateMCPServer(context.Background(), oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)
	created.Auth = store.MCPServerAuth{
		Mode:                  store.MCPAuthModeOAuth,
		ClientID:              "client-1",
		ClientSecret:          "secret-client-1",
		Issuer:                fx.URL,
		AuthServerMetadataURL: fx.URL + "/.well-known/oauth-authorization-server",
	}
	require.NoError(t, fs.MCPServers().Update(context.Background(), &created))
	encoded, err := json.Marshal(mcp.Token{AccessToken: "at-old", TokenType: "Bearer", RefreshToken: "rt-old"})
	require.NoError(t, err)
	require.NoError(t, fs.MCPOAuthTokens().Save(context.Background(),
		&store.MCPOAuthToken{ServerID: created.ID, Token: encoded}))
	return created
}

// assertDisconnected pins the disconnect outcome shared by every revocation
// variant: tokens deleted, cache invalidated, row auth_required with the
// disconnect message, registration kept.
func assertDisconnected(t *testing.T, fs *fake.Store, cache *recordingMCPCache, serverID string, disconnected store.MCPServer) {
	t.Helper()
	_, err := fs.MCPOAuthTokens().Get(context.Background(), serverID)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Equal(t, mcp.StatusAuthRequired, disconnected.Status)
	require.Equal(t, MCPDisconnectedMessage, disconnected.StatusError)
	require.Equal(t, 0, disconnected.ToolCount)
	require.Equal(t, "client-1", disconnected.Auth.ClientID, "the registration survives the disconnect")
	require.Equal(t, store.MCPAuthModeOAuth, disconnected.Auth.Mode)
	require.Equal(t, []string{serverID}, cache.invalidated())
}

func TestDisconnectMCPServerRevokesRefreshToken(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	cache := &recordingMCPCache{}
	svc := NewSettingsService(fs, WithMCPInvalidate(cache.invalidate))
	fx := newOAuthFlowFixture(t)
	created := seedOAuthSession(t, fs, svc, fx)

	disconnected, err := svc.DisconnectMCPServer(ctx, created.ID)
	require.NoError(t, err)

	// The refresh token was revoked, with its hint and the client id.
	revokes := fx.revocationsSeen()
	require.Len(t, revokes, 1)
	require.Equal(t, "rt-old", revokes[0].Get("token"))
	require.Equal(t, "refresh_token", revokes[0].Get("token_type_hint"))
	require.Equal(t, "client-1", revokes[0].Get("client_id"))
	require.Empty(t, revokes[0].Get("access_token_hint"))
	assertDisconnected(t, fs, cache, created.ID, disconnected)
}

func TestDisconnectMCPServerWithoutRevocationEndpoint(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	cache := &recordingMCPCache{}
	svc := NewSettingsService(fs, WithMCPInvalidate(cache.invalidate))
	fx := newOAuthFlowFixture(t)
	fx.noRevocationAdvert = true
	created := seedOAuthSession(t, fs, svc, fx)

	disconnected, err := svc.DisconnectMCPServer(ctx, created.ID)
	require.NoError(t, err)

	require.Empty(t, fx.revocationsSeen(), "nothing to revoke against: no revoke call")
	assertDisconnected(t, fs, cache, created.ID, disconnected)
}

func TestDisconnectMCPServerFailedRevocationIsBestEffort(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	cache := &recordingMCPCache{}
	svc := NewSettingsService(fs, WithMCPInvalidate(cache.invalidate))
	fx := newOAuthFlowFixture(t)
	fx.revocationFails = true
	created := seedOAuthSession(t, fs, svc, fx)

	disconnected, err := svc.DisconnectMCPServer(ctx, created.ID)
	require.NoError(t, err, "a failed revocation must not block a disconnect")
	require.Len(t, fx.revocationsSeen(), 1)
	assertDisconnected(t, fs, cache, created.ID, disconnected)
}

func TestDisconnectMCPServerWithoutStoredToken(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	cache := &recordingMCPCache{}
	svc := NewSettingsService(fs, WithMCPInvalidate(cache.invalidate))
	fx := newOAuthFlowFixture(t)

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("atlassian", fx.URL+"/mcp"))
	require.NoError(t, err)
	created.Auth = store.MCPServerAuth{
		Mode:                  store.MCPAuthModeOAuth,
		ClientID:              "client-1",
		AuthServerMetadataURL: fx.URL + "/.well-known/oauth-authorization-server",
	}
	require.NoError(t, fs.MCPServers().Update(ctx, &created))

	disconnected, err := svc.DisconnectMCPServer(ctx, created.ID)
	require.NoError(t, err, "an absent token row is nothing to revoke, not a failure")
	require.Empty(t, fx.revocationsSeen())
	require.Equal(t, mcp.StatusAuthRequired, disconnected.Status)
	require.Equal(t, MCPDisconnectedMessage, disconnected.StatusError)
}

func TestDisconnectMCPServerNotOAuthMode(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	created, err := svc.CreateMCPServer(ctx, oauthHTTPServerRow("plain", "https://mcp.example/mcp"))
	require.NoError(t, err)

	_, err = svc.DisconnectMCPServer(ctx, created.ID)
	require.ErrorContains(t, err, "not oauth")
}
