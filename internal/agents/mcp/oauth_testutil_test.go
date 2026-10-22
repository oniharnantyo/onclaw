package mcp

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

// fakeAuthServer is an httptest authorization server for the OAuth tests: it
// serves RFC 8414 metadata (optionally only under the OIDC discovery path),
// hands out a fresh client id per registration, grants authorization-code
// tokens, rejects every refresh token with invalid_grant unless refreshOK
// opts in (the expired-credentials classification relies on the default), and
// records the token and revocation forms it received.
type fakeAuthServer struct {
	*httptest.Server

	mu            sync.Mutex
	registrations int
	tokenForms    []url.Values
	revokeForms   []url.Values

	oidcOnly    bool // serve metadata only at /.well-known/openid-configuration
	metadata404 bool // 404 every metadata document (broken-advertisement case)
	refreshOK   bool // answer grant_type=refresh_token with a fresh token set (successful headless refresh)
}

func newFakeAuthServer(t *testing.T) *fakeAuthServer {
	t.Helper()
	fake := &fakeAuthServer{}
	fake.Server = httptest.NewServer(fake)
	t.Cleanup(fake.Close)
	return fake
}

func (f *fakeAuthServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		if f.oidcOnly || f.metadata404 {
			http.NotFound(w, r)
			return
		}
		f.writeMetadata(w)
	case "/.well-known/openid-configuration":
		if f.metadata404 {
			http.NotFound(w, r)
			return
		}
		f.writeMetadata(w)
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
		form := cloneForm(r.PostForm)
		f.mu.Lock()
		f.tokenForms = append(f.tokenForms, form)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if form.Get("grant_type") != "authorization_code" {
			if form.Get("grant_type") == "refresh_token" && f.refreshOK {
				// An honored refresh hands out a fresh token set — new access
				// and refresh tokens — so the headless-refresh test can
				// connect; the guarded MCP endpoint only accepts Bearer good.
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "good",
					"token_type":    "Bearer",
					"refresh_token": "rt-refreshed",
					"expires_in":    60,
				})
				return
			}
			// Every refresh is rejected by default: the expired-token test
			// needs a refresh that cannot rescue the stored token.
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
		f.revokeForms = append(f.revokeForms, cloneForm(r.PostForm))
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAuthServer) writeMetadata(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                 f.URL,
		"authorization_endpoint": f.URL + "/authorize",
		"token_endpoint":         f.URL + "/token",
		"registration_endpoint":  f.URL + "/register",
		"revocation_endpoint":    f.URL + "/revoke",
		"scopes_supported":       []string{"read", "write"},
	})
}

func (f *fakeAuthServer) registrationsSeen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registrations
}

func (f *fakeAuthServer) tokensSeen() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tokenForms)
}

func (f *fakeAuthServer) revocationsSeen() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.revokeForms)
}

// bearerMCP is the OAuth-protected MCP endpoint: it 401s every request
// without `Authorization: Bearer good`, using the Atlassian-shaped challenge —
// a resource_metadata URL at a path-suffixed well-known location whose
// origin-level sibling does not exist — and serves the in-process mcp-go
// streamable server otherwise.
type bearerMCP struct {
	inner http.Handler

	mu sync.Mutex
	// metadataURL is set once the fixture's server URL is known.
	metadataURL string
	// bareChallenge drops resource_metadata from the challenge (the plain
	// non-OAuth 401 case).
	bareChallenge bool
	// emptyServers makes the protected-resource document list no
	// authorization servers (the malformed-advertisement case).
	emptyServers bool
	// last method/Accept/headers of the most recent unauthenticated probe,
	// for pinning the Discover request shape.
	lastMethod string
	lastAccept string
	lastHeader http.Header
}

func (b *bearerMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer good" {
		b.mu.Lock()
		b.lastMethod = r.Method
		b.lastAccept = r.Header.Get("Accept")
		b.lastHeader = r.Header.Clone()
		metadataURL, bare := b.metadataURL, b.bareChallenge
		b.mu.Unlock()
		if bare {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		} else {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer error="invalid_token", resource_metadata=%q`, metadataURL))
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
		return
	}
	b.inner.ServeHTTP(w, r)
}

func (b *bearerMCP) resourceMetadataURL() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.metadataURL
}

func (b *bearerMCP) lastProbe() (method, accept string, header http.Header) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastMethod, b.lastAccept, b.lastHeader
}

// oauthFixture wires the verified Atlassian-shaped deployment: a
// bearer-protected streamable MCP endpoint whose 401 names a path-suffixed
// protected-resource document (the origin-level path 404s), pointing at a
// separate fake authorization server.
type oauthFixture struct {
	mcpServer *httptest.Server
	auth      *fakeAuthServer
	guard     *bearerMCP
}

func newOAuthFixture(t *testing.T) *oauthFixture {
	t.Helper()
	fx := &oauthFixture{
		auth:  newFakeAuthServer(t),
		guard: &bearerMCP{inner: mcpserver.NewStreamableHTTPServer(newStubServer())},
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", fx.guard)
	mux.HandleFunc("/.well-known/oauth-protected-resource/v2/mcp", func(w http.ResponseWriter, _ *http.Request) {
		servers := []string{fx.auth.URL}
		if fx.guard.emptyServers {
			servers = nil
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              fx.mcpServer.URL + "/mcp",
			"authorization_servers": servers,
			"scopes_supported":      []string{"read", "write", "offline_access"},
		})
	})
	// The origin-level /.well-known/oauth-protected-resource intentionally
	// does not exist — /mcp and the path-suffixed document are the only
	// routes (Atlassian's shape).
	fx.mcpServer = httptest.NewServer(mux)
	t.Cleanup(fx.mcpServer.Close)
	fx.guard.mu.Lock()
	fx.guard.metadataURL = fx.mcpServer.URL + "/.well-known/oauth-protected-resource/v2/mcp"
	fx.guard.mu.Unlock()
	return fx
}

// expectedDiscovery is the Discovery the fixture's advertisement must yield.
func (fx *oauthFixture) expectedDiscovery() Discovery {
	return Discovery{
		ResourceMetadataURL:   fx.guard.resourceMetadataURL(),
		Issuer:                fx.auth.URL,
		AuthServerMetadataURL: fx.auth.URL + "/.well-known/oauth-authorization-server",
		Scopes:                []string{"read", "write", "offline_access"},
		AuthorizationEndpoint: fx.auth.URL + "/authorize",
		TokenEndpoint:         fx.auth.URL + "/token",
		RegistrationEndpoint:  fx.auth.URL + "/register",
		RevocationEndpoint:    fx.auth.URL + "/revoke",
	}
}

// cloneForm snapshots a parsed form (url.Values.Clone needs go1.27).
func cloneForm(form url.Values) url.Values {
	out := make(url.Values, len(form))
	for name, values := range form {
		out[name] = slices.Clone(values)
	}
	return out
}

// freeCallbackURI binds 127.0.0.1:0 to learn a free port, releases it, and
// returns a loopback callback URI on that port — tests never touch the fixed
// DefaultRedirectURI port.
func freeCallbackURI(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return fmt.Sprintf("http://127.0.0.1:%d/callback", port)
}
