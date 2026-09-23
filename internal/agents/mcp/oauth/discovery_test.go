package oauth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/mcp/oauth"
)

// stubRoute is one recorded response in a discovery fixture.
type stubRoute struct {
	status  int
	wwwAuth string
	body    string
}

// discoveryFixture serves a fixed set of recorded-style routes over httptest
// and counts every request path, so tests can assert exactly which discovery
// URLs were probed and that token endpoints were never touched. Route bodies
// are built per request from the request's own host, letting fixtures embed
// absolute URLs before the test server's address exists.
type discoveryFixture struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newDiscoveryFixture(t *testing.T, routes func(base string) map[string]stubRoute) *discoveryFixture {
	t.Helper()
	f := &discoveryFixture{hits: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		f.record(r.URL.Path)
		route, ok := routes(base)[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if route.wwwAuth != "" {
			w.Header().Set("WWW-Authenticate", route.wwwAuth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(route.status)
		_, _ = w.Write([]byte(route.body))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *discoveryFixture) record(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits[path]++
}

func (f *discoveryFixture) hitCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

// tokenEndpoints are registered on every fixture so an accidental token
// request shows up as a counted hit; discovery must never touch them (task
// 3.3: no token requests are attempted by discovery).
var tokenEndpoints = []string{"/oauth/token", "/oauth2/token", "/token"}

// withTokenEndpoints registers the never-to-be-called token routes into a
// fixture's route map.
func withTokenEndpoints(routes map[string]stubRoute) map[string]stubRoute {
	for _, path := range tokenEndpoints {
		routes[path] = stubRoute{status: http.StatusOK, body: `{"access_token":"discovery-must-never-fetch-this"}`}
	}
	return routes
}

func (f *discoveryFixture) assertNoTokenCalls(t *testing.T) {
	t.Helper()
	for _, path := range tokenEndpoints {
		if n := f.hitCount(path); n != 0 {
			t.Errorf("token endpoint %s was called %d times; discovery must never request tokens", path, n)
		}
	}
}

func fixtureDiscovery(f *discoveryFixture, opts ...oauth.Option) *oauth.Discovery {
	return oauth.NewDiscovery(append([]oauth.Option{oauth.WithHTTPClient(f.srv.Client())}, opts...)...)
}

// gitlabRoutes is the GitLab-style recorded fixture: the MCP endpoint answers
// 401 with a WWW-Authenticate challenge pointing at a path-inserted
// protected-resource metadata document, whose authorization server publishes
// its RFC 8414 metadata at the root well-known with DCR and PKCE S256.
func gitlabRoutes(base string) map[string]stubRoute {
	prm := fmt.Sprintf(`{"resource":%q,"authorization_servers":[%q],"scopes_supported":["mcp","api"],"bearer_methods_supported":["header"]}`,
		base+"/api/v4/mcp", base)
	as := fmt.Sprintf(`{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q,"scopes_supported":["api","mcp"],"code_challenge_methods_supported":["S256"],"response_types_supported":["code"],"grant_types_supported":["authorization_code","refresh_token"]}`,
		base, base+"/oauth/authorize", base+"/oauth/token", base+"/oauth/register")
	return withTokenEndpoints(map[string]stubRoute{
		"/api/v4/mcp": {
			status:  http.StatusUnauthorized,
			wwwAuth: `Bearer realm="gitlab", resource_metadata="` + base + `/.well-known/oauth-protected-resource/api/v4/mcp"`,
		},
		"/.well-known/oauth-protected-resource/api/v4/mcp": {status: http.StatusOK, body: prm},
		"/.well-known/oauth-protected-resource":            {status: http.StatusNotFound},
		"/.well-known/oauth-authorization-server":          {status: http.StatusOK, body: as},
	})
}

// notionRoutes is the Notion-style recorded fixture: no WWW-Authenticate
// challenge at all, a root protected-resource metadata document, and an
// authorization server advertising DCR, PKCE S256, and public clients
// (token_endpoint_auth_methods_supported includes none).
func notionRoutes(base string, mcpStatus int) map[string]stubRoute {
	prm := fmt.Sprintf(`{"resource":%q,"authorization_servers":[%q],"scopes_supported":["default"]}`,
		base+"/mcp", base)
	as := fmt.Sprintf(`{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"registration_endpoint":%q,"scopes_supported":["default"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none","client_secret_post"],"grant_types_supported":["authorization_code","refresh_token"]}`,
		base, base+"/oauth/authorize", base+"/oauth/token", base+"/oauth/register")
	return withTokenEndpoints(map[string]stubRoute{
		"/mcp":                                             {status: mcpStatus},
		"/.well-known/oauth-protected-resource/mcp":        {status: http.StatusNotFound},
		"/.well-known/oauth-protected-resource":            {status: http.StatusOK, body: prm},
		"/.well-known/oauth-authorization-server":          {status: http.StatusOK, body: as},
	})
}

func TestDiscovery_GitLabChallengeWithPathInsertedPRM(t *testing.T) {
	f := newDiscoveryFixture(t, gitlabRoutes)
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/api/v4/mcp"

	meta, err := d.Discover(context.Background(), serverURL)
	if err != nil {
		t.Fatalf("expected discovery to succeed, got %v", err)
	}
	if meta.ServerURL != serverURL {
		t.Errorf("expected server url %q, got %q", serverURL, meta.ServerURL)
	}
	if meta.ResourceMetadataURL != f.srv.URL+"/.well-known/oauth-protected-resource/api/v4/mcp" {
		t.Errorf("expected the challenge's path-inserted PRM url, got %q", meta.ResourceMetadataURL)
	}
	if meta.AuthorizationServerURL != f.srv.URL {
		t.Errorf("expected authorization server %q, got %q", f.srv.URL, meta.AuthorizationServerURL)
	}
	as := meta.AuthorizationServer
	if as == nil {
		t.Fatal("expected authorization-server metadata to be resolved")
	}
	if as.TokenEndpoint != f.srv.URL+"/oauth/token" {
		t.Errorf("expected token endpoint %q, got %q", f.srv.URL+"/oauth/token", as.TokenEndpoint)
	}
	if as.RegistrationEndpoint != f.srv.URL+"/oauth/register" {
		t.Errorf("expected registration endpoint %q, got %q", f.srv.URL+"/oauth/register", as.RegistrationEndpoint)
	}
	if !as.SupportsPKCES256() {
		t.Error("expected the authorization server to advertise PKCE S256")
	}
	if meta.Resource.Resource != serverURL {
		t.Errorf("expected PRM resource %q, got %q", serverURL, meta.Resource.Resource)
	}
	if got := f.hitCount("/api/v4/mcp"); got != 1 {
		t.Errorf("expected exactly one challenge probe, got %d", got)
	}
	f.assertNoTokenCalls(t)
}

func TestDiscovery_NotionStyleNoChallenge(t *testing.T) {
	for name, status := range map[string]int{
		"plain 401 without challenge": http.StatusUnauthorized,
		"unprotected 200":             http.StatusOK,
	} {
		t.Run(name, func(t *testing.T) {
			f := newDiscoveryFixture(t, func(base string) map[string]stubRoute {
				return notionRoutes(base, status)
			})
			d := fixtureDiscovery(f)
			serverURL := f.srv.URL + "/mcp"

			meta, err := d.Discover(context.Background(), serverURL)
			if err != nil {
				t.Fatalf("expected discovery to succeed from the well-known fallback, got %v", err)
			}
			if meta.ResourceMetadataURL != f.srv.URL+"/.well-known/oauth-protected-resource" {
				t.Errorf("expected the root PRM url, got %q", meta.ResourceMetadataURL)
			}
			as := meta.AuthorizationServer
			if as == nil {
				t.Fatal("expected authorization-server metadata to be resolved")
			}
			if as.RegistrationEndpoint != f.srv.URL+"/oauth/register" {
				t.Errorf("expected DCR registration endpoint %q, got %q", f.srv.URL+"/oauth/register", as.RegistrationEndpoint)
			}
			if !as.SupportsPKCES256() {
				t.Error("expected PKCE S256 support")
			}
			if !containsString(as.TokenEndpointAuthMethodsSupported, "none") {
				t.Errorf("expected token_endpoint_auth_methods_supported to include none, got %v", as.TokenEndpointAuthMethodsSupported)
			}
			if as.SupportsDeviceFlow() {
				t.Error("fixture advertises no device endpoint; expected SupportsDeviceFlow false")
			}
			f.assertNoTokenCalls(t)
		})
	}
}

func TestDiscovery_PathInsertedWellKnownWins(t *testing.T) {
	f := newDiscoveryFixture(t, func(base string) map[string]stubRoute {
		inserted := fmt.Sprintf(`{"resource":%q,"authorization_servers":[%q]}`, base+"/api/v4/mcp", base)
		root := fmt.Sprintf(`{"resource":%q,"authorization_servers":[%q]}`, base+"/api/v4/mcp", base+"/shadow-as")
		as := fmt.Sprintf(`{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q}`,
			base, base+"/oauth/authorize", base+"/oauth/token")
		return withTokenEndpoints(map[string]stubRoute{
			"/api/v4/mcp":                                      {status: http.StatusUnauthorized},
			"/.well-known/oauth-protected-resource/api/v4/mcp": {status: http.StatusOK, body: inserted},
			"/.well-known/oauth-protected-resource":            {status: http.StatusOK, body: root},
			"/.well-known/oauth-authorization-server":          {status: http.StatusOK, body: as},
		})
	})
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/api/v4/mcp"

	meta, err := d.Discover(context.Background(), serverURL)
	if err != nil {
		t.Fatalf("expected discovery to succeed, got %v", err)
	}
	if meta.AuthorizationServerURL != f.srv.URL {
		t.Errorf("expected the path-inserted PRM's authorization server %q, got %q", f.srv.URL, meta.AuthorizationServerURL)
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource"); got != 0 {
		t.Errorf("expected the root PRM never to be fetched once the path-inserted form hit, got %d fetches", got)
	}
	f.assertNoTokenCalls(t)
}

func TestDiscovery_NoMetadataServerFailsTyped(t *testing.T) {
	f := newDiscoveryFixture(t, func(base string) map[string]stubRoute {
		return withTokenEndpoints(map[string]stubRoute{
			"/mcp": {status: http.StatusUnauthorized},
		})
	})
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/mcp"

	meta, err := d.Discover(context.Background(), serverURL)
	if err == nil {
		t.Fatalf("expected a typed discovery failure, got metadata %+v", meta)
	}
	if !errors.Is(err, oauth.ErrDiscoveryFailed) {
		t.Errorf("expected the error to chain ErrDiscoveryFailed, got %v", err)
	}
	var de *oauth.DiscoveryError
	if !errors.As(err, &de) {
		t.Fatalf("expected a *oauth.DiscoveryError, got %T", err)
	}
	if de.Kind != oauth.FailureNoResourceMetadata {
		t.Errorf("expected kind FailureNoResourceMetadata, got %v", de.Kind)
	}
	if de.ServerURL != serverURL {
		t.Errorf("expected the error to name the server url, got %q", de.ServerURL)
	}
	guidance := de.Guidance()
	if !strings.Contains(guidance, "bring-your-own") || !strings.Contains(guidance, "auth mode none") {
		t.Errorf("expected guidance pointing at BYO and static modes, got %q", guidance)
	}
	f.assertNoTokenCalls(t)
}

func TestDiscovery_PRMWithoutAuthorizationServer(t *testing.T) {
	cases := []struct {
		name string
		prm  func(base string) string
	}{
		{
			name: "no authorization_servers field",
			prm: func(base string) string {
				return fmt.Sprintf(`{"resource":%q}`, base+"/mcp")
			},
		},
		{
			name: "only malformed authorization_servers entries",
			prm: func(base string) string {
				return fmt.Sprintf(`{"resource":%q,"authorization_servers":["not-a-url",""]}`, base+"/mcp")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDiscoveryFixture(t, func(base string) map[string]stubRoute {
				return withTokenEndpoints(map[string]stubRoute{
					"/mcp":                                      {status: http.StatusUnauthorized},
					"/.well-known/oauth-protected-resource/mcp": {status: http.StatusNotFound},
					"/.well-known/oauth-protected-resource":     {status: http.StatusOK, body: tc.prm(base)},
				})
			})
			d := fixtureDiscovery(f)

			_, err := d.Discover(context.Background(), f.srv.URL+"/mcp")
			var de *oauth.DiscoveryError
			if !errors.As(err, &de) {
				t.Fatalf("expected a *oauth.DiscoveryError, got %v", err)
			}
			if de.Kind != oauth.FailureNoAuthorizationServer {
				t.Errorf("expected kind FailureNoAuthorizationServer, got %v", de.Kind)
			}
			if de.Guidance() == "" {
				t.Error("expected non-empty guidance")
			}
			f.assertNoTokenCalls(t)
		})
	}
}

func TestDiscovery_InvalidAuthorizationServerMetadata(t *testing.T) {
	cases := []struct {
		name    string
		asBody  func(base string) string
		asStatus int
	}{
		{
			name: "issuer mismatch",
			asBody: func(base string) string {
				return fmt.Sprintf(`{"issuer":"https://elsewhere.example","authorization_endpoint":%q,"token_endpoint":%q}`,
					base+"/oauth/authorize", base+"/oauth/token")
			},
			asStatus: http.StatusOK,
		},
		{
			name: "missing token endpoint",
			asBody: func(base string) string {
				return fmt.Sprintf(`{"issuer":%q,"authorization_endpoint":%q}`, base, base+"/oauth/authorize")
			},
			asStatus: http.StatusOK,
		},
		{
			name:    "non-JSON body",
			asBody:  func(base string) string { return "<html>not json</html>" },
			asStatus: http.StatusOK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDiscoveryFixture(t, func(base string) map[string]stubRoute {
				prm := fmt.Sprintf(`{"resource":%q,"authorization_servers":[%q]}`, base+"/mcp", base)
				return withTokenEndpoints(map[string]stubRoute{
					"/mcp":                                      {status: http.StatusUnauthorized},
					"/.well-known/oauth-protected-resource/mcp": {status: http.StatusNotFound},
					"/.well-known/oauth-protected-resource":     {status: http.StatusOK, body: prm},
					"/.well-known/oauth-authorization-server":   {status: tc.asStatus, body: tc.asBody(base)},
				})
			})
			d := fixtureDiscovery(f)

			_, err := d.Discover(context.Background(), f.srv.URL+"/mcp")
			var de *oauth.DiscoveryError
			if !errors.As(err, &de) {
				t.Fatalf("expected a *oauth.DiscoveryError, got %v", err)
			}
			if de.Kind != oauth.FailureInvalidAuthorizationServerMetadata {
				t.Errorf("expected kind FailureInvalidAuthorizationServerMetadata, got %v", de.Kind)
			}
			if de.Guidance() == "" {
				t.Error("expected non-empty guidance")
			}
			f.assertNoTokenCalls(t)
		})
	}
}

func TestDiscovery_RejectsNonHTTPServerURL(t *testing.T) {
	d := oauth.NewDiscovery()

	_, err := d.Discover(context.Background(), "stdio://not-an-http-server")
	if !errors.Is(err, oauth.ErrDiscoveryFailed) {
		t.Fatalf("expected ErrDiscoveryFailed, got %v", err)
	}
	var de *oauth.DiscoveryError
	if !errors.As(err, &de) || de.Kind != oauth.FailureNoResourceMetadata {
		t.Errorf("expected kind FailureNoResourceMetadata, got %v", err)
	}
}

func TestDiscovery_WithChallengeSkipsProbe(t *testing.T) {
	f := newDiscoveryFixture(t, gitlabRoutes)
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/api/v4/mcp"
	challenge := `Bearer realm="gitlab", resource_metadata="` + f.srv.URL + `/.well-known/oauth-protected-resource/api/v4/mcp"`

	meta, err := d.Discover(context.Background(), serverURL, oauth.WithChallenge(challenge))
	if err != nil {
		t.Fatalf("expected discovery seeded by the live 401 challenge to succeed, got %v", err)
	}
	if meta.ResourceMetadataURL != f.srv.URL+"/.well-known/oauth-protected-resource/api/v4/mcp" {
		t.Errorf("expected the challenge's PRM url, got %q", meta.ResourceMetadataURL)
	}
	if got := f.hitCount("/api/v4/mcp"); got != 0 {
		t.Errorf("expected the challenge to skip the probe request, got %d probes", got)
	}
	f.assertNoTokenCalls(t)
}

func TestDiscovery_CacheHitAvoidsRefetch(t *testing.T) {
	f := newDiscoveryFixture(t, gitlabRoutes)
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/api/v4/mcp"

	first, err := d.Discover(context.Background(), serverURL)
	if err != nil {
		t.Fatalf("first discover: %v", err)
	}
	second, err := d.Discover(context.Background(), serverURL)
	if err != nil {
		t.Fatalf("second discover: %v", err)
	}
	if first != second {
		t.Error("expected the cached pointer to be handed back")
	}
	if got := f.hitCount("/api/v4/mcp"); got != 1 {
		t.Errorf("expected the cache to skip the probe on the second discover, got %d probes", got)
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource/api/v4/mcp"); got != 1 {
		t.Errorf("expected the cache to skip the PRM fetch, got %d fetches", got)
	}
}

func TestDiscovery_CacheKeyNormalizesTrailingSlash(t *testing.T) {
	f := newDiscoveryFixture(t, gitlabRoutes)
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/api/v4/mcp"

	if _, err := d.Discover(context.Background(), serverURL); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	if _, err := d.Discover(context.Background(), serverURL+"/"); err != nil {
		t.Fatalf("trailing-slash discover: %v", err)
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource/api/v4/mcp"); got != 1 {
		t.Errorf("expected both spellings to share one cache entry, got %d fetches", got)
	}
}

func TestDiscovery_ForceRefreshBypassesCache(t *testing.T) {
	f := newDiscoveryFixture(t, gitlabRoutes)
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/api/v4/mcp"

	if _, err := d.Discover(context.Background(), serverURL); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	refreshed, err := d.Discover(context.Background(), serverURL, oauth.ForceRefresh())
	if err != nil {
		t.Fatalf("forced discover: %v", err)
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource/api/v4/mcp"); got != 2 {
		t.Errorf("expected the forced refresh to re-run the chain, got %d PRM fetches", got)
	}
	if refreshed.AuthorizationServer.TokenEndpoint != f.srv.URL+"/oauth/token" {
		t.Errorf("expected a fully resolved fresh result, got %+v", refreshed.AuthorizationServer)
	}
}

func TestDiscovery_InvalidateForcesRefetch(t *testing.T) {
	f := newDiscoveryFixture(t, gitlabRoutes)
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/api/v4/mcp"

	if _, err := d.Discover(context.Background(), serverURL); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	d.Invalidate(serverURL)
	if _, err := d.Discover(context.Background(), serverURL); err != nil {
		t.Fatalf("post-invalidation discover: %v", err)
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource/api/v4/mcp"); got != 2 {
		t.Errorf("expected the invalidated entry to refetch, got %d PRM fetches", got)
	}
	// Invalidating an absent (or differently spelled) key is a no-op that
	// still matches the normalized entry.
	d.Invalidate(serverURL + "/")
	if _, err := d.Discover(context.Background(), serverURL); err != nil {
		t.Fatalf("third discover: %v", err)
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource/api/v4/mcp"); got != 3 {
		t.Errorf("expected the trailing-slash invalidation to drop the shared entry, got %d fetches", got)
	}
}

func TestDiscovery_CacheTTLExpiry(t *testing.T) {
	f := newDiscoveryFixture(t, gitlabRoutes)
	d := fixtureDiscovery(f, oauth.WithCacheTTL(250*time.Millisecond))
	serverURL := f.srv.URL + "/api/v4/mcp"

	if _, err := d.Discover(context.Background(), serverURL); err != nil {
		t.Fatalf("first discover: %v", err)
	}
	if _, err := d.Discover(context.Background(), serverURL); err != nil {
		t.Fatalf("within-TTL discover: %v", err)
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource/api/v4/mcp"); got != 1 {
		t.Fatalf("expected the second discover to hit the cache, got %d fetches", got)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := d.Discover(context.Background(), serverURL); err != nil {
		t.Fatalf("post-TTL discover: %v", err)
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource/api/v4/mcp"); got != 2 {
		t.Errorf("expected the expired entry to refetch, got %d fetches", got)
	}
}

func TestDiscovery_ConcurrentDiscover(t *testing.T) {
	f := newDiscoveryFixture(t, gitlabRoutes)
	d := fixtureDiscovery(f)
	serverURL := f.srv.URL + "/api/v4/mcp"

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = d.Discover(context.Background(), serverURL)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent discover %d: %v", i, err)
		}
	}
	if got := f.hitCount("/.well-known/oauth-protected-resource/api/v4/mcp"); got < 1 {
		t.Errorf("expected at least one PRM fetch, got %d", got)
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
