package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/agents/mcp/oauth"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// The MCP OAuth flow fixtures (add-mcp-oauth-client tasks 6.1–6.3): one
// httptest server plays protected resource AND authorization server — the
// challenge probe, the well-known documents, DCR, the device endpoint, and a
// scripted token endpoint. Pattern mirrors the services layer's mcpOAuthFixture.
// ---------------------------------------------------------------------------

type asTokenReply struct {
	status int
	body   string
}

type mcpASFixture struct {
	srv           *httptest.Server
	mu            sync.Mutex
	tokenCalls    int
	tokenForms    []url.Values
	tokenScript   []asTokenReply
	scriptFrom    int
	registerCalls int
	registerForms []url.Values
	deviceCalls   int
}

func newMCPASFixture(t *testing.T) *mcpASFixture {
	t.Helper()
	f := &mcpASFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		idx := f.tokenCalls
		f.tokenCalls++
		f.tokenForms = append(f.tokenForms, r.PostForm)
		reply := asTokenReply{status: http.StatusOK, body: `{"access_token":"at-mcp-new","refresh_token":"rt-mcp-new","expires_in":3600,"scope":"mcp"}`}
		if idx >= f.scriptFrom && len(f.tokenScript) > 0 {
			at := idx - f.scriptFrom
			if at >= len(f.tokenScript) {
				at = len(f.tokenScript) - 1
			}
			reply = f.tokenScript[at]
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	})
	mux.HandleFunc("/oauth/register", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.registerCalls++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_id":"dcr-client-1","client_secret":"dcr-secret-1","token_endpoint_auth_method":"client_secret_basic"}`))
	})
	mux.HandleFunc("/oauth/device", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.deviceCalls++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"dev-1","user_code":"ABCD-EFGH","verification_uri":"` + f.srv.URL + `/activate","verification_uri_complete":"` + f.srv.URL + `/activate?code=ABCD-EFGH","expires_in":600,"interval":1}`))
	})
	mux.HandleFunc("/api/v4/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+f.srv.URL+`/.well-known/oauth-protected-resource/api/v4/mcp"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/api/v4/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"` + f.srv.URL + `/api/v4/mcp","authorization_servers":["` + f.srv.URL + `"],"scopes_supported":["mcp"]}`))
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer":"` + f.srv.URL + `","authorization_endpoint":"` + f.srv.URL + `/oauth/authorize","token_endpoint":"` + f.srv.URL + `/oauth/token","registration_endpoint":"` + f.srv.URL + `/oauth/register","device_authorization_endpoint":"` + f.srv.URL + `/oauth/device","scopes_supported":["mcp"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none","client_secret_basic"]}`))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *mcpASFixture) base() string { return strings.TrimRight(f.srv.URL, "/") }

func (f *mcpASFixture) script(replies ...asTokenReply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenScript = replies
	f.scriptFrom = f.tokenCalls
}

func (f *mcpASFixture) tokenForm(i int) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenForms[i]
}

func (f *mcpASFixture) tokens() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenCalls
}

func (f *mcpASFixture) registrations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registerCalls
}

// ---------------------------------------------------------------------------
// The two handler envs: the browser flow (public base URL set) and the
// headless device flow (no public base URL — task 6.2's whole point).
// ---------------------------------------------------------------------------

const testPublicBase = "https://onclaw.example.com"

type mcpOAuthEnv struct {
	st         store.Store
	ws         *domain.Workspace
	settings   *agents.MCPSettingsService
	encKey     []byte
	fixture    *mcpASFixture
	publicBase string
	engine     *gin.Engine
}

func newMCPOAuthEnv(t *testing.T, publicBase string) *mcpOAuthEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	fixture := newMCPASFixture(t)
	encKey := []byte("01234567890123456789012345678901")

	user := &domain.User{Email: "mcp-oauth@example.com", Name: "MCP OAuth"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	settings := agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), encKey)
	client := oauth.NewClient(encKey,
		oauth.WithClientHTTPClient(fixture.srv.Client()),
		oauth.WithDiscovery(oauth.NewDiscovery(oauth.WithHTTPClient(fixture.srv.Client()))),
	)
	h := handlers.NewMCPOAuthHandlers(settings, st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), st.MCPTokens(), client, encKey, publicBase)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.UserContextKey, user)
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.POST("/mcp-servers/:id/oauth/authorize", h.BeginWorkspaceAuthorize)
	r.POST("/mcp-servers/:id/oauth/device", h.BeginWorkspaceDevice)
	r.POST("/mcp-servers/:id/oauth/device/poll", h.PollWorkspaceDevice)
	r.POST("/agents/:agent/mcp-servers/:id/oauth/authorize", h.BeginAgentAuthorize)
	r.POST("/agents/:agent/mcp-servers/:id/oauth/device", h.BeginAgentDevice)
	r.POST("/agents/:agent/mcp-servers/:id/oauth/device/poll", h.PollAgentDevice)
	r.GET(services.MCPOAuthCallbackPath, h.Callback)

	return &mcpOAuthEnv{st: st, ws: ws, settings: settings, encKey: encKey, fixture: fixture, publicBase: publicBase, engine: r}
}

// seedOAuthServer registers an oauth-mode row against the fixture and returns
// its id; a mutated func adjusts the BYO rows (nil keeps the DCR shape: no
// configured client).
func (e *mcpOAuthEnv) seedOAuthServer(t *testing.T, name string, mutate func(*domain.MCPConnection)) string {
	t.Helper()
	conn := domain.MCPConnection{
		Transport: domain.MCPTransportStreamableHTTP,
		URL:       e.fixture.base() + "/api/v4/mcp",
		AuthMode:  domain.MCPAuthModeOAuth,
	}
	if mutate != nil {
		mutate(&conn)
	}
	srv := &domain.WorkspaceMCPServer{WorkspaceID: e.ws.ID, Name: name, MCPConnection: conn, Enabled: true}
	if err := e.settings.CreateWorkspaceServer(context.Background(), srv); err != nil {
		t.Fatalf("seed oauth server: %v", err)
	}
	return srv.ID
}

func doMCPRequest(r *gin.Engine, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != nil {
		blob, _ := json.Marshal(body)
		reader = strings.NewReader(string(blob))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == handlers.MCPOAuthSessionCookie {
			return c
		}
	}
	t.Fatalf("no %s cookie on the begin response", handlers.MCPOAuthSessionCookie)
	return nil
}

// beginAuthorize drives the begin endpoint once and returns the parsed
// authorize URL query (state, client_id, redirect_uri, code_challenge, …)
// together with the sealed session cookie — a matched pair minted by the same
// call (the session is nonce-bound to the state).
func (e *mcpOAuthEnv) beginAuthorize(t *testing.T, serverID string) (url.Values, *http.Cookie) {
	t.Helper()
	w := doMCPRequest(e.engine, http.MethodPost, "/mcp-servers/"+serverID+"/oauth/authorize", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("begin authorize: status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.AuthorizeURL == "" {
		t.Fatalf("begin authorize body: %v / %s", err, w.Body.String())
	}
	parsed, err := url.Parse(res.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	return parsed.Query(), sessionCookie(t, w)
}

// ---------------------------------------------------------------------------
// Browser flow: begin + callback (tasks 6.1 / 5.3)
// ---------------------------------------------------------------------------

func TestMCPOAuthAuthorizeBeginAndCallbackHappyPath(t *testing.T) {
	e := newMCPOAuthEnv(t, testPublicBase)
	ctx := context.Background()
	serverID := e.seedOAuthServer(t, "Notion", nil)

	query, cookie := e.beginAuthorize(t, serverID)

	// DCR ran and its result is persisted on the row (register once) with the
	// registration secret sealed under the workspace AAD.
	if e.fixture.registrations() != 1 {
		t.Fatalf("register calls = %d, want 1", e.fixture.registrations())
	}
	if got := query.Get("client_id"); got != "dcr-client-1" {
		t.Fatalf("authorize client_id = %q, want the DCR result", got)
	}
	if got := query.Get("redirect_uri"); got != testPublicBase+"/api/v1/mcp/oauth/callback" {
		t.Fatalf("authorize redirect_uri = %q", got)
	}
	if query.Get("state") == "" || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorize query missing state/PKCE: %v", query)
	}
	row, err := e.st.WorkspaceMCPServers().Get(ctx, e.ws.ID, serverID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.OAuthClientID != "dcr-client-1" {
		t.Fatalf("row client id = %q, want the persisted DCR registration", row.OAuthClientID)
	}
	if !strings.HasPrefix(row.OAuthClientSecret, "v1:") || strings.Contains(row.OAuthClientSecret, "dcr-secret-1") {
		t.Fatalf("row client secret = %q, want the sealed envelope", row.OAuthClientSecret)
	}

	// The provider redirects back: the callback exchanges the code, replaces
	// the token rows, and clears the row to connected.
	w := doMCPRequest(e.engine, http.MethodGet, services.MCPOAuthCallbackPath+"?code=the-code&state="+url.QueryEscape(query.Get("state")), nil, cookie)
	if w.Code != http.StatusFound {
		t.Fatalf("callback status %d: %s", w.Code, w.Body.String())
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse bounce: %v", err)
	}
	if location.Path != "/settings" {
		t.Fatalf("bounce path = %q", location.Path)
	}
	bounce := location.Query()
	if bounce.Get("pane") != "mcp" || bounce.Get("mcp_oauth") != serverID || bounce.Get("status") != "connected" {
		t.Fatalf("bounce query = %v", bounce)
	}

	// The token form carried the code and the PKCE verifier (bound to the
	// session), authenticated as the registered client.
	form := e.fixture.tokenForm(0)
	if form.Get("code") != "the-code" || form.Get("code_verifier") == "" {
		t.Fatalf("token form = %v; want code + code_verifier", form)
	}

	// Token rows replaced, decryptable under the workspace AAD; the row is
	// connected with the detail cleared (task 5.3).
	tokenRow, err := e.st.MCPTokens().Get(ctx, e.ws.ID, "", serverID)
	if err != nil {
		t.Fatalf("token row: %v", err)
	}
	access, err := secrets.Decrypt(e.encKey, []byte(e.ws.ID), tokenRow.AccessTokenCiphertext)
	if err != nil || string(access) != "at-mcp-new" {
		t.Fatalf("stored access token = (%q, %v)", access, err)
	}
	if tokenRow.RefreshTokenCiphertext == "" || tokenRow.GrantedScopes == nil || len(tokenRow.GrantedScopes) != 1 || tokenRow.GrantedScopes[0] != "mcp" {
		t.Fatalf("token row = %+v", tokenRow)
	}
	if tokenRow.Issuer != e.fixture.base() {
		t.Fatalf("token issuer = %q, want the authorization server", tokenRow.Issuer)
	}
	row, err = e.st.WorkspaceMCPServers().Get(ctx, e.ws.ID, serverID)
	if err != nil {
		t.Fatalf("reload row: %v", err)
	}
	if row.Status != domain.MCPStatusConnected || row.StatusError != "" {
		t.Fatalf("row status = (%q, %q), want connected with the detail cleared", row.Status, row.StatusError)
	}
}

// ---------------------------------------------------------------------------
// Begin rejections
// ---------------------------------------------------------------------------

func TestMCPOAuthCallbackForgedStateRejected(t *testing.T) {
	e := newMCPOAuthEnv(t, testPublicBase)
	serverID := e.seedOAuthServer(t, "Notion", nil)

	w := doMCPRequest(e.engine, http.MethodGet, services.MCPOAuthCallbackPath+"?code=x&state=forged-state", nil)
	if w.Code != http.StatusFound {
		t.Fatalf("callback status %d", w.Code)
	}
	bounce := w.Header().Get("Location")
	if !strings.Contains(bounce, "status=failed") || !strings.Contains(bounce, "could+not+be+validated") {
		t.Fatalf("bounce = %q, want the generic rejection", bounce)
	}
	if e.fixture.tokens() != 0 {
		t.Fatalf("token endpoint hit %d times; a rejected state must never exchange a code", e.fixture.tokens())
	}
	if _, err := e.st.MCPTokens().Get(context.Background(), e.ws.ID, "", serverID); err == nil {
		t.Fatal("a token row exists after a forged-state callback")
	}
}

func TestMCPOAuthCallbackStateReplayRejected(t *testing.T) {
	e := newMCPOAuthEnv(t, testPublicBase)
	serverID := e.seedOAuthServer(t, "Notion", nil)

	state, cookie := e.beginAuthorize(t, serverID)

	first := doMCPRequest(e.engine, http.MethodGet, services.MCPOAuthCallbackPath+"?code=c1&state="+url.QueryEscape(state.Get("state")), nil, cookie)
	if first.Code != http.StatusFound || !strings.Contains(first.Header().Get("Location"), "status=connected") {
		t.Fatalf("first callback = %d %q", first.Code, first.Header().Get("Location"))
	}

	// The replay carries the same state and session with a fresh code: the
	// consumed nonce rejects it before any exchange.
	second := doMCPRequest(e.engine, http.MethodGet, services.MCPOAuthCallbackPath+"?code=c2&state="+url.QueryEscape(state.Get("state")), nil, cookie)
	if second.Code != http.StatusFound || !strings.Contains(second.Header().Get("Location"), "status=failed") {
		t.Fatalf("replay callback = %d %q", second.Code, second.Header().Get("Location"))
	}
	if e.fixture.tokens() != 1 {
		t.Fatalf("token endpoint hit %d times; a replayed state must not exchange again", e.fixture.tokens())
	}
}

func TestMCPOAuthCallbackProviderDenialBounces(t *testing.T) {
	e := newMCPOAuthEnv(t, testPublicBase)
	serverID := e.seedOAuthServer(t, "Notion", nil)

	state, _ := e.beginAuthorize(t, serverID)
	w := doMCPRequest(e.engine, http.MethodGet, services.MCPOAuthCallbackPath+"?error=access_denied&error_description=user+declined&state="+url.QueryEscape(state.Get("state")), nil)
	if w.Code != http.StatusFound {
		t.Fatalf("callback status %d", w.Code)
	}
	bounce := w.Header().Get("Location")
	if !strings.Contains(bounce, "status=failed") || !strings.Contains(bounce, "declined") || !strings.Contains(bounce, "mcp_oauth="+serverID) {
		t.Fatalf("bounce = %q", bounce)
	}
	if e.fixture.tokens() != 0 {
		t.Fatalf("token endpoint hit %d times on a denial", e.fixture.tokens())
	}
}

// TestMCPOAuthReauthorizeClearsExpired pins task 5.3's lifecycle half: a row
// parked in expired (a failed refresh) exits ONLY through reauthorization.
func TestMCPOAuthReauthorizeClearsExpired(t *testing.T) {
	e := newMCPOAuthEnv(t, testPublicBase)
	ctx := context.Background()
	serverID := e.seedOAuthServer(t, "Notion", nil)
	if err := e.settings.SetWorkspaceServerStatus(ctx, e.ws.ID, serverID, domain.MCPStatusExpired, "refresh refused", 0); err != nil {
		t.Fatalf("mark expired: %v", err)
	}

	query, cookie := e.beginAuthorize(t, serverID)
	w := doMCPRequest(e.engine, http.MethodGet, services.MCPOAuthCallbackPath+"?code=the-code&state="+url.QueryEscape(query.Get("state")), nil, cookie)
	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "status=connected") {
		t.Fatalf("reauthorize callback = %d %q", w.Code, w.Header().Get("Location"))
	}
	row, err := e.st.WorkspaceMCPServers().Get(ctx, e.ws.ID, serverID)
	if err != nil {
		t.Fatalf("reload row: %v", err)
	}
	if row.Status != domain.MCPStatusConnected || row.StatusError != "" {
		t.Fatalf("row status = (%q, %q); expired must exit to connected with the detail cleared", row.Status, row.StatusError)
	}
}

// ---------------------------------------------------------------------------
// Begin rejections
// ---------------------------------------------------------------------------

func TestMCPOAuthBeginUnknownServerIs404(t *testing.T) {
	e := newMCPOAuthEnv(t, testPublicBase)
	w := doMCPRequest(e.engine, http.MethodPost, "/mcp-servers/nope/oauth/authorize", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != "not_found" {
		t.Fatalf("envelope = %s", w.Body.String())
	}
}

func TestMCPOAuthBeginNonOAuthRowRejected(t *testing.T) {
	e := newMCPOAuthEnv(t, testPublicBase)
	ctx := context.Background()
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID: e.ws.ID,
		Name:        "static",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       e.fixture.base() + "/api/v4/mcp",
			Headers:   []domain.EnvRow{{Name: "Authorization", Value: "Bearer pat"}},
		},
		Enabled: true,
	}
	if err := e.settings.CreateWorkspaceServer(ctx, srv); err != nil {
		t.Fatalf("seed static server: %v", err)
	}
	w := doMCPRequest(e.engine, http.MethodPost, "/mcp-servers/"+srv.ID+"/oauth/authorize", nil)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not configured for OAuth") {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestMCPOAuthBrowserFlowUnavailableHeadless(t *testing.T) {
	e := newMCPOAuthEnv(t, "")
	serverID := e.seedOAuthServer(t, "Notion", nil)
	w := doMCPRequest(e.engine, http.MethodPost, "/mcp-servers/"+serverID+"/oauth/authorize", nil)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "device authorization flow") {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	cb := doMCPRequest(e.engine, http.MethodGet, services.MCPOAuthCallbackPath+"?code=x&state=y", nil)
	if cb.Code != http.StatusBadRequest {
		t.Fatalf("callback status %d", cb.Code)
	}
}

// ---------------------------------------------------------------------------
// Agent-private scope (task 6.1: both scopes)
// ---------------------------------------------------------------------------

func TestMCPOAuthAgentPrivateScopeFlow(t *testing.T) {
	e := newMCPOAuthEnv(t, testPublicBase)
	ctx := context.Background()
	agent := &domain.Agent{WorkspaceID: e.ws.ID, Name: "Atlas", Slug: "atlas"}
	if err := e.st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	conn := domain.MCPConnection{
		Transport: domain.MCPTransportStreamableHTTP,
		URL:       e.fixture.base() + "/api/v4/mcp",
		AuthMode:  domain.MCPAuthModeOAuth,
	}
	srv := &domain.AgentMCPServer{WorkspaceID: e.ws.ID, AgentID: agent.ID, Name: "private", MCPConnection: conn, Enabled: true}
	if err := e.settings.CreateAgentServer(ctx, srv); err != nil {
		t.Fatalf("seed agent server: %v", err)
	}

	// Begin by slug (the route param convention).
	w := doMCPRequest(e.engine, http.MethodPost, "/agents/atlas/mcp-servers/"+srv.ID+"/oauth/authorize", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("agent begin status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("agent begin body: %v", err)
	}
	parsed, _ := url.Parse(res.AuthorizeURL)
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatalf("agent begin authorize url = %q", res.AuthorizeURL)
	}
	cookie := sessionCookie(t, w)

	cb := doMCPRequest(e.engine, http.MethodGet, services.MCPOAuthCallbackPath+"?code=the-code&state="+url.QueryEscape(state), nil, cookie)
	if cb.Code != http.StatusFound || !strings.Contains(cb.Header().Get("Location"), "status=connected") {
		t.Fatalf("agent callback = %d %q", cb.Code, cb.Header().Get("Location"))
	}
	if _, err := e.st.MCPTokens().Get(ctx, e.ws.ID, agent.ID, srv.ID); err != nil {
		t.Fatalf("agent-scope token row: %v", err)
	}
	if _, err := e.st.MCPTokens().Get(ctx, e.ws.ID, "", srv.ID); err == nil {
		t.Fatal("agent-scope tokens leaked into the workspace scope")
	}
	row, err := e.st.AgentMCPServers().Get(ctx, agent.ID, srv.ID)
	if err != nil {
		t.Fatalf("agent row: %v", err)
	}
	if row.Status != domain.MCPStatusConnected {
		t.Fatalf("agent row status = %q", row.Status)
	}
}

// ---------------------------------------------------------------------------
// Device flow (task 6.2) — headless env: no public base URL anywhere
// ---------------------------------------------------------------------------

func (e *mcpOAuthEnv) beginDevice(t *testing.T, path, serverID string) map[string]any {
	t.Helper()
	w := doMCPRequest(e.engine, http.MethodPost, path+serverID+"/oauth/device", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("device begin status %d: %s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("device begin body: %v", err)
	}
	for _, key := range []string{"device_session", "user_code", "verification_uri", "expires_in", "interval"} {
		if _, ok := res[key]; !ok {
			t.Fatalf("device begin response missing %q: %v", key, res)
		}
	}
	return res
}

func (e *mcpOAuthEnv) pollDevice(t *testing.T, path, serverID, session string) (int, map[string]any) {
	t.Helper()
	w := doMCPRequest(e.engine, http.MethodPost, path+serverID+"/oauth/device/poll", map[string]string{"device_session": session})
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	return w.Code, res
}

func TestMCPOAuthDeviceFlowHappyPath(t *testing.T) {
	e := newMCPOAuthEnv(t, "") // headless: the device flow is the flow
	ctx := context.Background()
	serverID := e.seedOAuthServer(t, "Notion", nil)

	res := e.beginDevice(t, "/mcp-servers/", serverID)
	if res["user_code"] != "ABCD-EFGH" {
		t.Fatalf("user_code = %v", res["user_code"])
	}
	session, _ := res["device_session"].(string)

	// First poll: the provider is still waiting for the user.
	e.fixture.script(asTokenReply{status: http.StatusUnauthorized, body: `{"error":"authorization_pending"}`})
	code, body := e.pollDevice(t, "/mcp-servers/", serverID, session)
	if code != http.StatusOK || body["status"] != "pending" {
		t.Fatalf("poll 1 = %d %v", code, body)
	}

	// The user approved: the next poll completes, stores the tokens, and
	// clears the row (the register-once DCR result rides along).
	e.fixture.script(asTokenReply{status: http.StatusOK, body: `{"access_token":"at-device","refresh_token":"rt-device","expires_in":3600,"scope":"mcp"}`})
	code, body = e.pollDevice(t, "/mcp-servers/", serverID, session)
	if code != http.StatusOK || body["status"] != "completed" {
		t.Fatalf("poll 2 = %d %v", code, body)
	}
	tokenRow, err := e.st.MCPTokens().Get(ctx, e.ws.ID, "", serverID)
	if err != nil {
		t.Fatalf("token row: %v", err)
	}
	access, err := secrets.Decrypt(e.encKey, []byte(e.ws.ID), tokenRow.AccessTokenCiphertext)
	if err != nil || string(access) != "at-device" {
		t.Fatalf("stored access token = (%q, %v)", access, err)
	}
	row, err := e.st.WorkspaceMCPServers().Get(ctx, e.ws.ID, serverID)
	if err != nil {
		t.Fatalf("row: %v", err)
	}
	if row.Status != domain.MCPStatusConnected || row.StatusError != "" {
		t.Fatalf("row status = (%q, %q)", row.Status, row.StatusError)
	}
}

func TestMCPOAuthDeviceFlowDeniedAndExpired(t *testing.T) {
	e := newMCPOAuthEnv(t, "")
	serverID := e.seedOAuthServer(t, "Notion", nil)
	res := e.beginDevice(t, "/mcp-servers/", serverID)
	session, _ := res["device_session"].(string)

	e.fixture.script(asTokenReply{status: http.StatusUnauthorized, body: `{"error":"access_denied","error_description":"nope"}`})
	code, body := e.pollDevice(t, "/mcp-servers/", serverID, session)
	if code != http.StatusOK || body["status"] != "denied" {
		t.Fatalf("denied poll = %d %v", code, body)
	}
	if _, err := e.st.MCPTokens().Get(context.Background(), e.ws.ID, "", serverID); err == nil {
		t.Fatal("a denied flow stored tokens")
	}

	// Expired: a fresh flow whose device code lapsed at the provider.
	e.fixture.script(asTokenReply{status: http.StatusUnauthorized, body: `{"error":"expired_token"}`})
	code, body = e.pollDevice(t, "/mcp-servers/", serverID, session)
	if code != http.StatusOK || body["status"] != "expired" {
		t.Fatalf("expired poll = %d %v", code, body)
	}
}

func TestMCPOAuthDeviceSessionBindingEnforced(t *testing.T) {
	e := newMCPOAuthEnv(t, "")
	serverA := e.seedOAuthServer(t, "A", nil)
	serverB := e.seedOAuthServer(t, "B", nil)

	res := e.beginDevice(t, "/mcp-servers/", serverA)
	session, _ := res["device_session"].(string)

	// The blob minted for A must not authorize a poll against B.
	code, _ := e.pollDevice(t, "/mcp-servers/", serverB, session)
	if code != http.StatusBadRequest {
		t.Fatalf("cross-binding poll status = %d, want 400", code)
	}

	// And a tampered blob is a 400, never a token write.
	code, _ = e.pollDevice(t, "/mcp-servers/", serverA, session+"x")
	if code != http.StatusBadRequest {
		t.Fatalf("tampered blob status = %d, want 400", code)
	}
	if e.fixture.tokens() != 0 {
		t.Fatalf("token endpoint hit %d times from rejected sessions", e.fixture.tokens())
	}
}

// ---------------------------------------------------------------------------
// BYO secret handling on the config surface (handlers/mcp.go) and the
// needs-authorization status detail (task 6.3)
// ---------------------------------------------------------------------------

// noopConnectionNamer plays the managed-server pointer seam; none of these
// rows are origin-marked.
type noopConnectionNamer struct{}

func (noopConnectionNamer) ConnectionDisplayName(context.Context, string, string) string { return "" }

func TestMCPServerCreateEncryptsOAuthSecretAndSurfacesNeedsAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	st := storefake.New()
	fixture := newMCPASFixture(t)
	encKey := []byte("01234567890123456789012345678901")
	user := &domain.User{Email: "cfg@example.com", Name: "Config"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	settings := agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), encKey)
	client := oauth.NewClient(encKey,
		oauth.WithClientHTTPClient(fixture.srv.Client()),
		oauth.WithDiscovery(oauth.NewDiscovery(oauth.WithHTTPClient(fixture.srv.Client()))),
	)
	// The same composition-root wiring the router builds: the config
	// handler's probes and the dial seam share the credential resolution.
	dialCreds := services.NewMCPOAuthDialCredentials(st.MCPTokens(), st.WorkspaceMCPServers(), st.AgentMCPServers(), settings, encKey, testPublicBase, client)
	h := handlers.NewMCPServerHandlers(settings, st.Agents(), noopInvalidator{}, 0, noopConnectionNamer{}, encKey, dialCreds)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.UserContextKey, user)
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.POST("/mcp-servers", h.CreateWorkspaceServer)
	r.GET("/mcp-servers", h.ListWorkspaceServers)

	payload := map[string]any{
		"name":                "Notion",
		"transport":           domain.MCPTransportStreamableHTTP,
		"url":                 fixture.base() + "/api/v4/mcp",
		"auth_mode":           domain.MCPAuthModeOAuth,
		"oauth_client_id":     "byo-app-1",
		"oauth_client_secret": "byo-secret-99",
	}
	w := doMCPRequest(r, http.MethodPost, "/mcp-servers", payload)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body.String())
	}

	rows, err := st.WorkspaceMCPServers().List(ctx, ws.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list rows: %v (%d)", err, len(rows))
	}
	stored := rows[0]
	if !strings.HasPrefix(stored.OAuthClientSecret, "v1:") || strings.Contains(stored.OAuthClientSecret, "byo-secret-99") {
		t.Fatalf("stored client secret = %q, want the sealed envelope", stored.OAuthClientSecret)
	}

	// The on-save probe resolved the BYO client, found no usable credential,
	// and surfaced the needs-authorization signal as the row's status detail —
	// probe/run parity with the run path (task 6.3).
	if stored.Status != domain.MCPStatusError || !strings.Contains(stored.StatusError, "OAuth authorization") {
		t.Fatalf("probe status = (%q, %q); want error with the needs-authorization detail", stored.Status, stored.StatusError)
	}

	// The read view carries the hint, never the secret.
	lw := doMCPRequest(r, http.MethodGet, "/mcp-servers", nil)
	var listRes struct {
		Servers []struct {
			OAuthClientSecretHint string `json:"oauth_client_secret_hint"`
			OAuthClientSecret     string `json:"oauth_client_secret"`
			AuthMode              string `json:"auth_mode"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(lw.Body.Bytes(), &listRes); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(listRes.Servers) != 1 {
		t.Fatalf("servers = %d", len(listRes.Servers))
	}
	view := listRes.Servers[0]
	if view.AuthMode != domain.MCPAuthModeOAuth {
		t.Fatalf("view auth_mode = %q", view.AuthMode)
	}
	if view.OAuthClientSecret != "" {
		t.Fatalf("view leaked the client secret field: %q", view.OAuthClientSecret)
	}
	if view.OAuthClientSecretHint != "t-99" {
		t.Fatalf("view hint = %q, want the last four characters", view.OAuthClientSecretHint)
	}
}
