package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// errProbeFailure is the programmed probe outcome for the failure-path tests.
var errProbeFailure = errors.New("token rejected by upstream")

// -----------------------------------------------------------------------------
// OAuth HTTP surface (add-connection-oauth tasks 5.2): the guards
// (integrations.write on connect-for-oauth and reauthorize,
// admin.integrations.write + master tenant on the app registry), the public
// callback's validation errors and redirect shape, the connect dispatch
// envelope (`{"authorize_url"}`), and the gallery's availability flip —
// fake-based HTTP tests with the token endpoint intercepted by a stub
// transport and the probe injected through the router options.
// -----------------------------------------------------------------------------

const oauthTestBaseURL = "https://onclaw.example.com"

// stubTokenEndpoint is the http.RoundTripper the router-level tests inject:
// it answers the recipe's real token URL with canned JSON, recording forms.
type stubTokenEndpoint struct {
	mu       sync.Mutex
	status   int
	body     string
	requests []url.Values
}

func newStubTokenEndpoint() *stubTokenEndpoint {
	return &stubTokenEndpoint{
		status: http.StatusOK,
		body:   `{"access_token":"at-http-1","refresh_token":"rt-http-1","expires_in":3600,"scope":"read:jira-work offline_access"}`,
	}
}

func (s *stubTokenEndpoint) RoundTrip(req *http.Request) (*http.Response, error) {
	_ = req.ParseForm()
	s.mu.Lock()
	s.requests = append(s.requests, req.PostForm)
	status, body := s.status, s.body
	s.mu.Unlock()
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    req,
	}, nil
}

func (s *stubTokenEndpoint) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

// oauthEnv is a test env with the public base URL set, a stub prober, and the
// token endpoint stubbed at the transport level (no network anywhere).
func oauthEnv(t *testing.T) (*testEnv, *stubProber, *stubTokenEndpoint) {
	t.Helper()
	prober := &stubProber{}
	transport := newStubTokenEndpoint()
	env := setupTestEnv(t, func(o *server.RouterOptions) {
		o.PublicBaseURL = oauthTestBaseURL
		settings := agents.NewMCPSettingsService(o.Store.WorkspaceMCPServers(), o.Store.AgentMCPServers(), o.Store.Agents(), o.EncryptionKey)
		o.Connections = services.NewConnectionsService(
			o.Store.Connections(),
			o.Store.WorkspaceMCPServers(),
			o.Store.Agents(),
			settings,
			o.Store.OAuthApps(),
			o.EncryptionKey,
			o.PublicBaseURL,
			services.WithProber(prober.probe),
			services.WithTokenHTTPClient(&http.Client{Transport: transport}),
		)
	})
	return env, prober, transport
}

// registerAppFromHTTP registers the instance app through the real admin route
// (the same door the instance admin pane rides).
func registerAppFromHTTP(t *testing.T, env *testEnv, superToken, provider string) {
	t.Helper()
	w := doRequest(env.router, http.MethodPut, "/api/v1/admin/oauth-apps/"+provider, superToken, map[string]any{
		"client_id": "client-id-1234", "client_secret": "client-secret-9999",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("register %s app over HTTP: %d %s", provider, w.Code, w.Body.String())
	}
}

// seedOAuthWorkspace wires owner/admin/member/outsider for one workspace.
func seedOAuthWorkspace(t *testing.T, env *testEnv, slug string) (ownerToken, adminToken, memberToken, outsiderToken string) {
	t.Helper()
	ownerUser, ownerToken := createTestUser(t, env, slug+"-owner@example.com", "Owner", "pwd")
	adminUser, adminToken := createTestUser(t, env, slug+"-admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, slug+"-member@example.com", "Member", "pwd")
	_, outsiderToken = createTestUser(t, env, slug+"-outsider@example.com", "Outsider", "pwd")
	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, slug, "OAuth WS "+slug)
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)
	return ownerToken, adminToken, memberToken, outsiderToken
}

// authorizeState extracts the signed state from an authorize_url response.
func authorizeState(t *testing.T, body []byte) (authorizeURL, state string) {
	t.Helper()
	var res struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.AuthorizeURL == "" {
		t.Fatalf("expected an authorize_url response, got %s", body)
	}
	parsed, err := url.Parse(res.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	return res.AuthorizeURL, parsed.Query().Get("state")
}

// Guards: connect-for-oauth and reauthorize ride integrations.write; the app
// registry rides admin.integrations.write under the master tenant.
func TestOAuth_HTTP_Guards(t *testing.T) {
	env, prober, _ := oauthEnv(t)
	ownerToken, adminToken, memberToken, outsiderToken := seedOAuthWorkspace(t, env, "oauth-guard-ws")
	_, superToken := seedTestSuperadmin(t, env, "oauth-root@onclaw.local", "Root", "supersecret123")
	registerAppFromHTTP(t, env, superToken, "atlassian")
	base := "/api/v1/workspaces/oauth-guard-ws/integrations"

	t.Run("connect for an oauth recipe is integrations.write", func(t *testing.T) {
		prober.calls = 0
		w := doRequest(env.router, http.MethodPost, base+"/connections", memberToken, map[string]any{
			"recipe_id": "atlassian", "access_level": "read_only",
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("member connect-for-oauth expected 403, got %d: %s", w.Code, w.Body.String())
		}
		if prober.calls != 0 {
			t.Errorf("the guard must reject before any flow, got %d probes", prober.calls)
		}
		// Owner and admin pass the guard and reach the authorize builder.
		for _, tc := range []struct{ name, token string }{{"owner", ownerToken}, {"admin", adminToken}} {
			w := doRequest(env.router, http.MethodPost, base+"/connections", tc.token, map[string]any{
				"recipe_id": "atlassian", "access_level": "read_only",
			})
			if w.Code != http.StatusOK {
				t.Fatalf("%s connect-for-oauth expected 200, got %d: %s", tc.name, w.Code, w.Body.String())
			}
		}
	})

	t.Run("app registry is admin.integrations.write in the master tenant", func(t *testing.T) {
		// A master-tenant member (not superadmin) passes the tenant gate but
		// holds no admin.integrations.write.
		member, memberToken := createTestUser(t, env, "oauth-master-member@example.com", "Master Member", "pwd")
		masterWs, err := env.store.Workspaces().BySlug(context.Background(), "master")
		if err != nil {
			t.Fatalf("load master workspace: %v", err)
		}
		memberRole, err := env.store.Roles().FindByName(context.Background(), masterWs.ID, domain.RoleMember)
		if err != nil {
			t.Fatalf("load member role: %v", err)
		}
		addMember(t, env, masterWs.ID, member.ID, memberRole.ID)

		w := doRequest(env.router, http.MethodGet, "/api/v1/admin/oauth-apps", memberToken, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("master member expected 403 on the app registry, got %d: %s", w.Code, w.Body.String())
		}
		// A non-master workspace user is 404 — enumeration defense on the
		// master tenant gate.
		w = doRequest(env.router, http.MethodGet, "/api/v1/admin/oauth-apps", outsiderToken, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("non-master tenant expected 404 on the app registry, got %d: %s", w.Code, w.Body.String())
		}
		// The superadmin holds admin.integrations.write.
		w = doRequest(env.router, http.MethodGet, "/api/v1/admin/oauth-apps", superToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("superadmin expected 200 on the app registry, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unauthenticated requests are rejected", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, "/api/v1/admin/oauth-apps", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated app list expected 401, got %d", w.Code)
		}
		// The callback itself is PUBLIC — it must not demand auth.
		w = doRequest(env.router, http.MethodGet, "/api/v1/integrations/oauth/callback?state=junk", "", nil)
		if w.Code != http.StatusFound {
			t.Fatalf("the public callback must not demand auth, got %d", w.Code)
		}
	})
}

// Connect dispatch and reauthorize envelopes + the full callback redirect
// shape over HTTP.
func TestOAuth_HTTP_ConnectCallbackAndReauthorize(t *testing.T) {
	env, prober, transport := oauthEnv(t)
	ownerToken, _, memberToken, _ := seedOAuthWorkspace(t, env, "oauth-flow-ws")
	_, superToken := seedTestSuperadmin(t, env, "oauth-flow-root@onclaw.local", "Root", "supersecret123")
	ws, err := env.store.Workspaces().BySlug(context.Background(), "oauth-flow-ws")
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	base := "/api/v1/workspaces/oauth-flow-ws/integrations"
	callback := "/api/v1/integrations/oauth/callback"

	t.Run("missing app registration names the registration", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "atlassian", "access_level": "read_only",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on the missing app, got %d: %s", w.Code, w.Body.String())
		}
		var envErr server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envErr)
		if envErr.Error.Code != server.CodeInvalidRequest {
			t.Errorf("expected invalid_request, got %q", envErr.Error.Code)
		}
		if !strings.Contains(envErr.Error.Message, "register its OAuth app") || !strings.Contains(envErr.Error.Message, "Atlassian") {
			t.Errorf("expected the naming error, got %q", envErr.Error.Message)
		}
	})

	registerAppFromHTTP(t, env, superToken, "atlassian")

	var state string
	t.Run("connect dispatches to the authorize builder", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "atlassian", "access_level": "read_only",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on the oauth dispatch, got %d: %s", w.Code, w.Body.String())
		}
		rawURL, parsed := authorizeState(t, w.Body.Bytes())
		state = parsed
		if !strings.Contains(rawURL, "auth.atlassian.com/authorize") {
			t.Errorf("expected the recipe's authorize endpoint, got %s", rawURL)
		}
		q, _ := url.Parse(rawURL)
		query := q.Query()
		if query.Get("redirect_uri") != oauthTestBaseURL+"/api/v1/integrations/oauth/callback" {
			t.Errorf("expected the derived redirect uri, got %q", query.Get("redirect_uri"))
		}
		if query.Get("client_id") != "client-id-1234" {
			t.Errorf("expected the registered client id, got %q", query.Get("client_id"))
		}
		// No connection may exist yet — the connect only dispatched a flow.
		wList := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		if strings.Contains(wList.Body.String(), "atlassian") {
			t.Errorf("the dispatch must not store a connection, got %s", wList.Body.String())
		}
		if prober.calls != 0 {
			t.Errorf("the dispatch must not probe, got %d", prober.calls)
		}
	})

	t.Run("callback redirects to the gallery with the outcome", func(t *testing.T) {
		// Public route: no auth header at all.
		w := doRequest(env.router, http.MethodGet, callback+"?code=auth-code-1&state="+url.QueryEscape(state), "", nil)
		if w.Code != http.StatusFound {
			t.Fatalf("expected 302 on the callback, got %d: %s", w.Code, w.Body.String())
		}
		location, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse redirect: %v", err)
		}
		if location.Scheme != "https" || location.Host != "onclaw.example.com" || location.Path != "/settings" {
			t.Fatalf("unexpected redirect target: %s", location)
		}
		query := location.Query()
		if query.Get("pane") != "integrations" || query.Get("oauth") != "atlassian" || query.Get("status") != "connected" {
			t.Fatalf("unexpected redirect query: %v", query)
		}
		if transport.calls() != 1 {
			t.Fatalf("expected exactly one token exchange, got %d", transport.calls())
		}
	})

	t.Run("the activated connection is live in the gallery", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "atlassian") {
			t.Fatalf("expected the activated connection listed, got %d: %s", w.Code, w.Body.String())
		}
		var list struct {
			Connections []struct {
				ID     string   `json:"id"`
				Status string   `json:"status"`
				Scopes []string `json:"granted_scopes"`
			} `json:"connections"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list.Connections) != 1 || list.Connections[0].Status != "connected" {
			t.Fatalf("expected one connected connection, got %+v", list.Connections)
		}
		if len(list.Connections[0].Scopes) == 0 {
			t.Error("expected granted_scopes as an array on the wire")
		}
	})

	t.Run("state rejection is a generic failed redirect", func(t *testing.T) {
		for _, tc := range []struct {
			name, query string
		}{
			{"no state", "code=x"},
			{"garbage state", "code=x&state=junk.junk"},
			{"replayed state", "code=fresh&state=" + url.QueryEscape(state)},
		} {
			w := doRequest(env.router, http.MethodGet, callback+"?"+tc.query, "", nil)
			if w.Code != http.StatusFound {
				t.Fatalf("%s: expected 302, got %d", tc.name, w.Code)
			}
			location, _ := url.Parse(w.Header().Get("Location"))
			query := location.Query()
			if query.Get("status") != "failed" {
				t.Errorf("%s: expected status=failed, got %v", tc.name, query)
			}
			if query.Get("oauth") != "" {
				t.Errorf("%s: a rejected state must not reveal the recipe, got %q", tc.name, query.Get("oauth"))
			}
			if !strings.Contains(query.Get("detail"), "could not be validated") {
				t.Errorf("%s: expected the generic detail, got %q", tc.name, query.Get("detail"))
			}
		}
		// Replay stored nothing new.
		w := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		if strings.Count(w.Body.String(), `"service":"atlassian"`) != 1 {
			t.Error("a replayed state must not create a second connection")
		}
	})

	t.Run("probe failure redirects with the upstream detail and stores nothing", func(t *testing.T) {
		prober.err = errProbeFailure
		defer func() { prober.err = nil }()
		// Dispatch a second flow (fresh service state was consumed) — use a
		// second connection attempt on a DIFFERENT recipe to avoid the
		// duplicate guard: slack.
		registerAppFromHTTP(t, env, superToken, "slack")
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "slack",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("slack dispatch: %d %s", w.Code, w.Body.String())
		}
		_, slackState := authorizeState(t, w.Body.Bytes())

		w = doRequest(env.router, http.MethodGet, callback+"?code=auth-code-2&state="+url.QueryEscape(slackState), "", nil)
		if w.Code != http.StatusFound {
			t.Fatalf("expected 302, got %d", w.Code)
		}
		location, _ := url.Parse(w.Header().Get("Location"))
		query := location.Query()
		if query.Get("oauth") != "slack" || query.Get("status") != "failed" {
			t.Fatalf("unexpected failure redirect: %v", query)
		}
		if !strings.Contains(query.Get("detail"), "probe failed") {
			t.Errorf("expected the probe failure in the detail, got %q", query.Get("detail"))
		}
		wList := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		if strings.Contains(wList.Body.String(), "slack") {
			t.Error("a probe-failed callback must store no connection")
		}
	})

	t.Run("provider denial surfaces the failure under the recipe card", func(t *testing.T) {
		// A fresh flow for slack (registered above; still unconnected because
		// its probe-failed attempt stored nothing).
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "slack",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("slack dispatch: %d %s", w.Code, w.Body.String())
		}
		_, slackState := authorizeState(t, w.Body.Bytes())

		// The provider denies consent and bounces back with error params plus
		// the (valid) state: the redirect names the recipe so the gallery
		// surfaces the failure inline.
		w = doRequest(env.router, http.MethodGet, callback+"?error=access_denied&error_description="+url.QueryEscape("User cancelled the consent")+"&state="+url.QueryEscape(slackState), "", nil)
		if w.Code != http.StatusFound {
			t.Fatalf("expected 302 on denial, got %d", w.Code)
		}
		location, _ := url.Parse(w.Header().Get("Location"))
		query := location.Query()
		if query.Get("status") != "failed" || query.Get("oauth") != "slack" {
			t.Fatalf("expected the denial redirect to name the recipe, got %v", query)
		}
		if !strings.Contains(query.Get("detail"), "User cancelled the consent") {
			t.Errorf("expected the provider's description in the detail, got %q", query.Get("detail"))
		}

		// A denial whose state is absent/garbage keeps the recipe-less
		// redirect (tolerant: no forgery oracle, no recipe leak).
		w = doRequest(env.router, http.MethodGet, callback+"?error=access_denied&state=junk.junk", "", nil)
		if w.Code != http.StatusFound {
			t.Fatalf("expected 302 on garbage-state denial, got %d", w.Code)
		}
		location, _ = url.Parse(w.Header().Get("Location"))
		if q := location.Query(); q.Get("oauth") != "" || q.Get("status") != "failed" {
			t.Fatalf("expected the recipe-less failure redirect, got %v", q)
		}
		// Denials store nothing either way.
		wList := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		if strings.Contains(wList.Body.String(), "slack") {
			t.Error("a denied consent must store no connection")
		}
	})

	t.Run("reauthorize dispatches and re-activates in place", func(t *testing.T) {
		// Find the atlassian connection, expire it, reauthorize.
		w := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		var list struct {
			Connections []struct {
				ID     string `json:"id"`
				Server string `json:"server_id"`
			} `json:"connections"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		connID := list.Connections[0].ID
		conn, err := env.store.Connections().Get(context.Background(), ws.ID, connID)
		if err != nil {
			t.Fatalf("load connection: %v", err)
		}
		conn.Status = "expired"
		if err := env.store.Connections().UpdateTokenLifecycle(context.Background(), conn); err != nil {
			t.Fatalf("expire: %v", err)
		}

		// Member is 403 on reauthorize; owner reaches the builder.
		wMem := doRequest(env.router, http.MethodPost, base+"/connections/"+connID+"/reauthorize", memberToken, nil)
		if wMem.Code != http.StatusForbidden {
			t.Fatalf("member reauthorize expected 403, got %d", wMem.Code)
		}
		prober.err = nil
		w = doRequest(env.router, http.MethodPost, base+"/connections/"+connID+"/reauthorize", ownerToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("reauthorize expected 200, got %d: %s", w.Code, w.Body.String())
		}
		_, reauthState := authorizeState(t, w.Body.Bytes())

		w = doRequest(env.router, http.MethodGet, callback+"?code=auth-code-3&state="+url.QueryEscape(reauthState), "", nil)
		if w.Code != http.StatusFound {
			t.Fatalf("reauthorize callback expected 302, got %d", w.Code)
		}
		location, _ := url.Parse(w.Header().Get("Location"))
		if q := location.Query(); q.Get("status") != "connected" || q.Get("oauth") != "atlassian" {
			t.Fatalf("unexpected reauthorize redirect: %v", q)
		}

		// The connection id and its materialized server survive.
		wList := doRequest(env.router, http.MethodGet, base+"/connections/"+connID, ownerToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected the same connection after reauthorization, got %d", wList.Code)
		}
		var got struct {
			Connection struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Server string `json:"server_id"`
			} `json:"connection"`
		}
		_ = json.Unmarshal(wList.Body.Bytes(), &got)
		if got.Connection.ID != connID || got.Connection.Status != "connected" || got.Connection.Server != list.Connections[0].Server {
			t.Fatalf("expected in-place reactivation, got %+v", got.Connection)
		}
	})
}

// The instance admin app registry over HTTP: envelope shapes, write-only
// secret, and the gallery's availability flip.
func TestOAuth_HTTP_AdminAppsRegistry(t *testing.T) {
	env, _, _ := oauthEnv(t)
	ownerToken, _, _, _ := seedOAuthWorkspace(t, env, "oauth-apps-ws")
	_, superToken := seedTestSuperadmin(t, env, "oauth-apps-root@onclaw.local", "Root", "supersecret123")
	adminBase := "/api/v1/admin/oauth-apps"
	recipesBase := "/api/v1/workspaces/oauth-apps-ws/integrations/recipes"

	t.Run("PUT registers the app with the pinned envelope", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPut, adminBase+"/atlassian", superToken, map[string]any{
			"client_id": "client-id-1234", "client_secret": "client-secret-9999",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on PUT, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			App struct {
				Provider         string `json:"provider"`
				ClientID         string `json:"client_id"`
				ClientSecretHint string `json:"client_secret_hint"`
				RedirectURI      string `json:"redirect_uri"`
				CreatedAt        string `json:"created_at"`
				UpdatedAt        string `json:"updated_at"`
			} `json:"app"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode: %v (%s)", err, w.Body.String())
		}
		if res.App.Provider != "atlassian" || res.App.ClientID != "client-id-1234" || res.App.ClientSecretHint != "9999" {
			t.Fatalf("unexpected app view: %+v", res.App)
		}
		if res.App.RedirectURI != oauthTestBaseURL+"/api/v1/integrations/oauth/callback" {
			t.Errorf("expected the derived redirect uri, got %q", res.App.RedirectURI)
		}
		if res.App.CreatedAt == "" || res.App.UpdatedAt == "" {
			t.Error("expected timestamps on the app view")
		}
		// Write-only: the secret never crosses back out.
		if strings.Contains(w.Body.String(), "client-secret-9999") {
			t.Error("the app registry echoed the client secret")
		}
	})

	t.Run("GET lists and reads hint-only", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, adminBase, superToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on the list, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"apps"`) || !strings.Contains(w.Body.String(), "atlassian") {
			t.Fatalf("unexpected list envelope: %s", w.Body.String())
		}
		w = doRequest(env.router, http.MethodGet, adminBase+"/atlassian", superToken, nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"app"`) {
			t.Fatalf("expected the single-app envelope, got %d: %s", w.Code, w.Body.String())
		}
		w = doRequest(env.router, http.MethodGet, adminBase+"/never-registered", superToken, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 on an unregistered provider, got %d", w.Code)
		}
	})

	t.Run("validation rejects non-oauth and empty payloads", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPut, adminBase+"/acme", superToken, map[string]any{
			"client_id": "x", "client_secret": "y",
		})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not an OAuth integration provider") {
			t.Fatalf("expected the provider rejection, got %d: %s", w.Code, w.Body.String())
		}
		w = doRequest(env.router, http.MethodPut, adminBase+"/linear", superToken, map[string]any{
			"client_id": "lin-1",
		})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "client secret is required") {
			t.Fatalf("expected the first-registration secret requirement, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("the registration alone flips the gallery card", func(t *testing.T) {
		wBefore := doRequest(env.router, http.MethodGet, recipesBase, ownerToken, nil)
		if !strings.Contains(wBefore.Body.String(), `"availability":"coming_soon"`) {
			t.Fatalf("expected atlassian coming_soon before registration, got %s", wBefore.Body.String())
		}
		// Registered above — re-read: the flip needs no code change.
		wAfter := doRequest(env.router, http.MethodGet, recipesBase, ownerToken, nil)
		var res struct {
			Recipes []struct {
				ID           string `json:"id"`
				Availability string `json:"availability"`
			} `json:"recipes"`
		}
		_ = json.Unmarshal(wAfter.Body.Bytes(), &res)
		for _, r := range res.Recipes {
			if r.ID == "atlassian" && r.Availability != "available" {
				t.Errorf("expected atlassian available after registration, got %q", r.Availability)
			}
			if r.ID == "linear" && r.Availability != "coming_soon" {
				t.Errorf("expected linear still coming_soon, got %q", r.Availability)
			}
		}
	})
}
