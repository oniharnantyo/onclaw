package services_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// OAuth flow over workspace connections (add-connection-oauth tasks 5.1):
// signed-state round trip and rejections, the exchange+probe gate (store-
// nothing on failure), refresh margin + dual write-through, the expired
// transition, reauthorization preserving attachments, gallery availability
// gating, and the instance apps lifecycle — fake stores, real crypto, the
// token endpoint intercepted by a stub transport (the recipe's declared URLs
// are never dialed).
// ---------------------------------------------------------------------------

const (
	testClientID     = "client-id-1234"
	testClientSecret = "client-secret-9999"
	oauthTestToken   = "at-consented-1"
)

// capturedTokenRequest records one token-endpoint call: the URL and the POSTed
// form (grant_type, client credentials, code / refresh token).
type capturedTokenRequest struct {
	url  string
	form url.Values
}

// stubTokenTransport intercepts the token endpoint inside the HTTP client —
// no network anywhere.
type stubTokenTransport struct {
	mu       sync.Mutex
	status   int
	body     string
	requests []capturedTokenRequest
}

func newStubTokenTransport() *stubTokenTransport {
	return &stubTokenTransport{
		status: http.StatusOK,
		body:   `{"access_token":"at-new-1","refresh_token":"rt-new-1","expires_in":3600,"scope":"read:jira-work offline_access"}`,
	}
}

func (s *stubTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	_ = req.ParseForm()
	s.mu.Lock()
	s.requests = append(s.requests, capturedTokenRequest{url: req.URL.String(), form: req.PostForm})
	status, body := s.status, s.body
	s.mu.Unlock()
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    req,
	}, nil
}

func (s *stubTokenTransport) setResponse(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

func (s *stubTokenTransport) captured() []capturedTokenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedTokenRequest(nil), s.requests...)
}

// oauthFlowEnv is one fake store + real settings service + the OAuth flow
// wiring: stub token transport, green stub prober, and the derived public
// base URL.
type oauthFlowEnv struct {
	store    store.Store
	settings *agents.MCPSettingsService
	svc      *services.ConnectionsService
	appsSvc  *services.OAuthAppsService
	wsID     string
	userID   string

	transport *stubTokenTransport

	probeCalls      int
	probeErr        error
	probeCandidates []domain.MCPConnection
}

func newOAuthFlowEnv(t *testing.T) *oauthFlowEnv {
	t.Helper()
	ctx := context.Background()

	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{
		Slug:     "oauth-ws",
		Name:     "OAuth WS",
		Timezone: "UTC",
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ws, err := st.Workspaces().BySlug(ctx, "oauth-ws")
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}

	env := &oauthFlowEnv{
		store:     st,
		settings:  agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), []byte(testEncKey)),
		wsID:      ws.ID,
		userID:    testUserID,
		transport: newStubTokenTransport(),
	}
	env.appsSvc = services.NewOAuthAppsService(st.OAuthApps(), []byte(testEncKey), testPublicBaseURL)
	env.svc = services.NewConnectionsService(
		st.Connections(),
		st.WorkspaceMCPServers(),
		st.Agents(),
		env.settings,
		st.OAuthApps(),
		[]byte(testEncKey),
		testPublicBaseURL,
		services.WithProber(func(ctx context.Context, workspaceID, serverID, name string, conn domain.MCPConnection) (int, error) {
			env.probeCalls++
			env.probeCandidates = append(env.probeCandidates, conn)
			if env.probeErr != nil {
				return 0, env.probeErr
			}
			return testToolCount, nil
		}),
		services.WithTokenHTTPClient(&http.Client{Transport: env.transport}),
	)
	return env
}

// registerProviderApp registers the instance app for a provider through the
// admin service (the same door the instance admin pane rides).
func registerProviderApp(t *testing.T, env *oauthFlowEnv, provider string) {
	t.Helper()
	if _, err := env.appsSvc.Upsert(context.Background(), provider, testClientID, testClientSecret); err != nil {
		t.Fatalf("register %s app: %v", provider, err)
	}
}

// beginOAuthConnect starts a connect for an OAuth recipe and returns the
// authorize URL with its state parameter parsed out.
func beginOAuthConnect(t *testing.T, env *oauthFlowEnv, recipeID, accessLevel string) (authorizeURL, state string, err error) {
	t.Helper()
	res, err := env.svc.Connect(context.Background(), env.wsID, env.userID, recipeID, accessLevel, "")
	if err != nil {
		return "", "", err
	}
	parsed, err := url.Parse(res.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	return res.AuthorizeURL, parsed.Query().Get("state"), nil
}

// seedOAuthConnection persists an OAuth connection with a real refresh
// envelope plus its materialized server (encrypted token row, origin marker)
// — the post-activation shape a refresh or reauthorization starts from.
func seedOAuthConnection(t *testing.T, env *oauthFlowEnv, service string, mutate func(*domain.Connection)) *domain.Connection {
	t.Helper()
	ctx := context.Background()

	envelope, err := secrets.Encrypt([]byte(testEncKey), []byte(env.wsID), []byte("rt-stored-v1"))
	if err != nil {
		t.Fatalf("seal refresh token: %v", err)
	}
	expiresAt := time.Now().Add(5 * time.Minute) // inside the default 10m margin
	conn := &domain.Connection{
		WorkspaceID:       env.wsID,
		Service:           service,
		AccessLevel:       domain.ConnectionAccessReadOnly,
		Status:            domain.ConnectionStatusConnected,
		RefreshCiphertext: envelope,
		ExpiresAt:         &expiresAt,
		GrantedScopes:     []string{"read:jira-work"},
	}
	if mutate != nil {
		mutate(conn)
	}
	if err := env.store.Connections().Create(ctx, conn); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	recipe := domain.RecipeByID(service)
	tokenValue := "Bearer at-old-1"
	if recipe.TokenScheme == "" {
		tokenValue = "at-old-1"
	}
	sealedValue, err := secrets.Encrypt([]byte(testEncKey), []byte(env.wsID), []byte(tokenValue))
	if err != nil {
		t.Fatalf("seal token row: %v", err)
	}
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID:        env.wsID,
		Name:               recipe.Service,
		Enabled:            true,
		OriginConnectionID: conn.ID,
		MCPConnection: domain.MCPConnection{
			Transport: recipe.Transport,
		},
	}
	if recipe.Transport == domain.MCPTransportStdio {
		srv.MCPConnection.Command = "/bin/true"
		srv.MCPConnection.Env = []domain.EnvRow{{Name: recipe.TokenHeader, Value: sealedValue}}
	} else {
		srv.MCPConnection.URL = recipe.Endpoint
		srv.MCPConnection.Headers = []domain.EnvRow{{Name: recipe.TokenHeader, Value: sealedValue}}
	}
	if err := env.store.WorkspaceMCPServers().Create(ctx, srv); err != nil {
		t.Fatalf("seed materialized server: %v", err)
	}
	return conn
}

// ---------------------------------------------------------------------------
// Authorize URL + state (tasks.md 2.1)
// ---------------------------------------------------------------------------

func TestOAuthFlow_AuthorizeURLShape(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")

	authorizeURL, state, err := beginOAuthConnect(t, env, "atlassian", "")
	if err != nil {
		t.Fatalf("begin connect: %v", err)
	}
	if state == "" {
		t.Fatal("expected a signed state on the authorize URL")
	}
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	query := parsed.Query()
	if parsed.Scheme != "https" || parsed.Host != "auth.atlassian.com" || parsed.Path != "/authorize" {
		t.Errorf("expected the recipe's authorize endpoint, got %s", authorizeURL)
	}
	if got := query.Get("client_id"); got != testClientID {
		t.Errorf("expected the registered client id, got %q", got)
	}
	if got := query.Get("redirect_uri"); got != services.DeriveOAuthRedirectURI(testPublicBaseURL) {
		t.Errorf("expected the derived redirect uri, got %q", got)
	}
	if got := query.Get("response_type"); got != "code" {
		t.Errorf("expected response_type=code, got %q", got)
	}
	// An empty access level selects the recipe's read-only default scopes.
	if got := query.Get("scope"); !strings.Contains(got, "offline_access") {
		t.Errorf("expected the read-only consent scopes on the authorize URL, got %q", got)
	}
	if strings.Contains(authorizeURL, testClientSecret) {
		t.Error("authorize URL leaked the client secret")
	}
}

func TestOAuthFlow_StateTamperedAndUnknownRejected(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")

	_, state, err := beginOAuthConnect(t, env, "atlassian", "")
	if err != nil {
		t.Fatalf("begin connect: %v", err)
	}

	cases := []struct {
		name  string
		state string
	}{
		{"unknown state", "totally-made-up.state"},
		{"tampered payload", state[:len(state)-4] + "zzzz"},
		{"empty state", ""},
	}
	for _, tc := range cases {
		res := env.svc.HandleCallback(context.Background(), "auth-code-1", tc.state)
		if res.Status != services.OAuthCallbackFailed {
			t.Errorf("%s: expected failure, got %q", tc.name, res.Status)
		}
		if res.RecipeID != "" {
			t.Errorf("%s: a rejected state must not reveal the recipe, got %q", tc.name, res.RecipeID)
		}
		if !strings.Contains(res.Detail, "could not be validated") {
			t.Errorf("%s: expected the one generic detail, got %q", tc.name, res.Detail)
		}
	}
	// No token exchange may happen on a rejected state.
	if reqs := env.transport.captured(); len(reqs) != 0 {
		t.Errorf("rejected states must never reach the token endpoint, got %d calls", len(reqs))
	}
}

func TestOAuthFlow_StateReplayRejected(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")

	_, state, err := beginOAuthConnect(t, env, "atlassian", domain.ConnectionAccessReadOnly)
	if err != nil {
		t.Fatalf("begin connect: %v", err)
	}
	if res := env.svc.HandleCallback(context.Background(), "auth-code-1", state); res.Status != services.OAuthCallbackConnected {
		t.Fatalf("first callback expected connected, got %q (%s)", res.Status, res.Detail)
	}
	// Replay with a fresh code: the nonce was consumed — rejected before any
	// exchange, and no second connection appears.
	res := env.svc.HandleCallback(context.Background(), "auth-code-2", state)
	if res.Status != services.OAuthCallbackFailed {
		t.Fatalf("replayed callback expected failure, got %q", res.Status)
	}
	connections, err := env.store.Connections().List(context.Background(), env.wsID)
	if err != nil || len(connections) != 1 {
		t.Fatalf("expected exactly one connection after a replayed state, got %d (%v)", len(connections), err)
	}
}

func TestOAuthFlow_StateExpiredRejected(t *testing.T) {
	ctx := context.Background()
	st := storefake.New()
	if err := st.Workspaces().Create(ctx, &domain.Workspace{Slug: "ttl-ws", Name: "TTL", Timezone: "UTC"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	ws, _ := st.Workspaces().BySlug(ctx, "ttl-ws")
	transport := newStubTokenTransport()
	svc := services.NewConnectionsService(
		st.Connections(), st.WorkspaceMCPServers(), st.Agents(),
		agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), []byte(testEncKey)),
		st.OAuthApps(), []byte(testEncKey), testPublicBaseURL,
		services.WithStateTTL(time.Millisecond),
		services.WithTokenHTTPClient(&http.Client{Transport: transport}),
	)
	appsSvc := services.NewOAuthAppsService(st.OAuthApps(), []byte(testEncKey), testPublicBaseURL)
	if _, err := appsSvc.Upsert(ctx, "atlassian", testClientID, testClientSecret); err != nil {
		t.Fatalf("register app: %v", err)
	}

	res, err := svc.Connect(ctx, ws.ID, testUserID, "atlassian", "", "")
	if err != nil {
		t.Fatalf("begin connect: %v", err)
	}
	state, err := url.Parse(res.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	out := svc.HandleCallback(ctx, "auth-code-1", state.Query().Get("state"))
	if out.Status != services.OAuthCallbackFailed {
		t.Fatalf("expected the expired state to fail, got %q", out.Status)
	}
	if reqs := transport.captured(); len(reqs) != 0 {
		t.Errorf("an expired state must never reach the token endpoint, got %d calls", len(reqs))
	}
}

func TestOAuthFlow_StateFromAnotherKeyRejected(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")

	// A service sealed with a DIFFERENT instance key over the same stores —
	// its states are forgeries to the real service.
	forged := services.NewConnectionsService(
		env.store.Connections(), env.store.WorkspaceMCPServers(), env.store.Agents(),
		env.settings, env.store.OAuthApps(), []byte("another-key-3456789012345678901234"), testPublicBaseURL,
		services.WithTokenHTTPClient(&http.Client{Transport: env.transport}),
	)
	res, err := forged.Connect(context.Background(), env.wsID, env.userID, "atlassian", "", "")
	if err != nil {
		t.Fatalf("forge begin connect: %v", err)
	}
	state, _ := url.Parse(res.AuthorizeURL)

	out := env.svc.HandleCallback(context.Background(), "auth-code-1", state.Query().Get("state"))
	if out.Status != services.OAuthCallbackFailed {
		t.Fatalf("expected the forged state to fail, got %q", out.Status)
	}
}

// ---------------------------------------------------------------------------
// Callback: exchange + probe gate + activation (tasks.md 2.2, design.md D5)
// ---------------------------------------------------------------------------

func TestOAuthFlow_CallbackActivatesConnection(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()

	_, state, err := beginOAuthConnect(t, env, "atlassian", domain.ConnectionAccessReadOnly)
	if err != nil {
		t.Fatalf("begin connect: %v", err)
	}
	// The exchange carries the registered credentials and the code.
	res := env.svc.HandleCallback(ctx, "auth-code-1", state)
	if res.Status != services.OAuthCallbackConnected {
		t.Fatalf("expected connected, got %q (%s)", res.Status, res.Detail)
	}
	if res.RecipeID != "atlassian" {
		t.Errorf("expected the recipe id on the result, got %q", res.RecipeID)
	}

	reqs := env.transport.captured()
	if len(reqs) != 1 {
		t.Fatalf("expected exactly one token exchange, got %d", len(reqs))
	}
	form := reqs[0].form
	if reqs[0].url != "https://auth.atlassian.com/oauth/token" {
		t.Errorf("expected the recipe's token endpoint, got %s", reqs[0].url)
	}
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "auth-code-1" {
		t.Errorf("unexpected exchange grant: %v", form)
	}
	if form.Get("client_id") != testClientID || form.Get("client_secret") != testClientSecret {
		t.Errorf("expected the registered app credentials, got client_id=%q", form.Get("client_id"))
	}
	if form.Get("redirect_uri") != services.DeriveOAuthRedirectURI(testPublicBaseURL) {
		t.Errorf("expected the derived redirect uri on the exchange, got %q", form.Get("redirect_uri"))
	}

	// The connection row: full token set, encrypted refresh envelope
	// (workspace AAD), expiry, granted scopes, connected status.
	conn, err := env.store.Connections().GetByService(ctx, env.wsID, "atlassian")
	if err != nil {
		t.Fatalf("expected the activated connection, got %v", err)
	}
	if conn.Status != domain.ConnectionStatusConnected || conn.AccessLevel != domain.ConnectionAccessReadOnly {
		t.Errorf("unexpected connection row: %+v", conn)
	}
	if conn.ExpiresAt == nil || time.Until(*conn.ExpiresAt) <= 30*time.Minute {
		t.Errorf("expected an expiry advanced by the provider's expires_in, got %v", conn.ExpiresAt)
	}
	if len(conn.GrantedScopes) == 0 || conn.GrantedScopes[0] != "read:jira-work" {
		t.Errorf("expected the consented scopes stored, got %v", conn.GrantedScopes)
	}
	refreshToken, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), conn.RefreshCiphertext)
	if err != nil || string(refreshToken) != "rt-new-1" {
		t.Errorf("expected the refresh envelope to open with the workspace AAD, got %q (%v)", refreshToken, err)
	}

	// The materialized server: the access token in the single secret row and
	// the origin marker (probe-before-persist saw zero rows).
	servers, err := env.store.WorkspaceMCPServers().List(ctx, env.wsID)
	if err != nil || len(servers) != 1 {
		t.Fatalf("expected exactly one materialized server, got %d (%v)", len(servers), err)
	}
	if servers[0].OriginConnectionID != conn.ID {
		t.Errorf("expected the origin marker, got %q", servers[0].OriginConnectionID)
	}
	row, err := env.settings.WorkspaceServerForRuntime(ctx, env.wsID, servers[0].ID)
	if err != nil {
		t.Fatalf("runtime view: %v", err)
	}
	if len(row.Headers) != 1 || row.Headers[0].Name != "Authorization" || row.Headers[0].Value != "Bearer at-new-1" {
		t.Errorf("expected the fresh access token in the Authorization row, got %+v", row.Headers)
	}
	// The probe ran BEFORE anything was stored, on the candidate connection.
	rows, _ := env.store.Connections().List(ctx, env.wsID)
	if env.probeCalls != 1 || len(rows) != 1 {
		t.Errorf("expected one probe gating one stored connection, got %d probes / %d rows", env.probeCalls, len(rows))
	}
}

func TestOAuthFlow_CallbackProbeFailureStoresNothing(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()

	_, state, err := beginOAuthConnect(t, env, "atlassian", "")
	if err != nil {
		t.Fatalf("begin connect: %v", err)
	}
	env.probeErr = errors.New("token rejected by upstream")
	res := env.svc.HandleCallback(ctx, "auth-code-1", state)
	if res.Status != services.OAuthCallbackFailed {
		t.Fatalf("expected failure, got %q", res.Status)
	}
	if !strings.Contains(res.Detail, "probe failed") || !strings.Contains(res.Detail, "token rejected by upstream") {
		t.Errorf("expected the probe failure verbatim in the detail, got %q", res.Detail)
	}

	// Store-nothing hygiene (design.md D5): the exchanged token set is
	// discarded — no connection row, no server row, and the transport saw the
	// exchange exactly once (it is never retried against a dead probe).
	connections, err := env.store.Connections().List(ctx, env.wsID)
	if err != nil || len(connections) != 0 {
		t.Fatalf("probe failure must store no connection, got %d (%v)", len(connections), err)
	}
	servers, err := env.store.WorkspaceMCPServers().List(ctx, env.wsID)
	if err != nil || len(servers) != 0 {
		t.Fatalf("probe failure must store no server, got %d (%v)", len(servers), err)
	}
	// The consumed state cannot be retried either.
	retry := env.svc.HandleCallback(ctx, "auth-code-2", state)
	if retry.Status != services.OAuthCallbackFailed || retry.RecipeID != "" {
		t.Errorf("the failed flow's state must stay single-use, got %+v", retry)
	}
}

func TestOAuthFlow_MissingAppDuplicateAndBaseURL(t *testing.T) {
	env := newOAuthFlowEnv(t)
	ctx := context.Background()

	// Missing registration: the connect attempt names the required
	// instance-level registration (tasks.md 2.5).
	_, _, err := beginOAuthConnect(t, env, "atlassian", "")
	if !errors.Is(err, services.ErrOAuthAppNotRegistered) {
		t.Fatalf("expected ErrOAuthAppNotRegistered, got %v", err)
	}
	if !strings.Contains(err.Error(), "Atlassian") || !strings.Contains(err.Error(), "register its OAuth app") {
		t.Errorf("expected the naming error, got %q", err.Error())
	}
	if env.probeCalls != 0 {
		t.Errorf("missing app must not probe, got %d", env.probeCalls)
	}

	// Duplicate: an already-connected service reauthorizes instead.
	registerProviderApp(t, env, "atlassian")
	if res := env.svc.HandleCallback(ctx, "code-1", mustState(t, env, "atlassian")); res.Status != services.OAuthCallbackConnected {
		t.Fatalf("expected the first connect to activate, got %q (%s)", res.Status, res.Detail)
	}
	_, _, err = beginOAuthConnect(t, env, "atlassian", "")
	if !errors.Is(err, domain.ErrConnectionExists) {
		t.Fatalf("expected ErrConnectionExists on the second connect, got %v", err)
	}

	// Empty public base URL: OAuth connect is unavailable until configured.
	noBase := services.NewConnectionsService(
		env.store.Connections(), env.store.WorkspaceMCPServers(), env.store.Agents(),
		env.settings, env.store.OAuthApps(), []byte(testEncKey), "",
	)
	if _, err := noBase.Connect(ctx, env.wsID, env.userID, "atlassian", "", ""); !errors.Is(err, services.ErrOAuthUnavailable) {
		t.Fatalf("expected ErrOAuthUnavailable, got %v", err)
	}
}

// mustState begins a connect and returns just the state (helper for tests
// that don't need the URL).
func mustState(t *testing.T, env *oauthFlowEnv, recipeID string) string {
	t.Helper()
	_, state, err := beginOAuthConnect(t, env, recipeID, "")
	if err != nil {
		t.Fatalf("begin connect: %v", err)
	}
	return state
}

// ---------------------------------------------------------------------------
// Reauthorization (tasks.md 2.4, design.md D6)
// ---------------------------------------------------------------------------

func TestOAuthFlow_ReauthorizeReplacesTokenSetInPlace(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()

	// Connect, then attach an agent to the materialized server.
	if res := env.svc.HandleCallback(ctx, "code-1", mustState(t, env, "atlassian")); res.Status != services.OAuthCallbackConnected {
		t.Fatalf("connect: %q (%s)", res.Status, res.Detail)
	}
	conn, err := env.store.Connections().GetByService(ctx, env.wsID, "atlassian")
	if err != nil {
		t.Fatalf("load connection: %v", err)
	}
	server, err := env.store.WorkspaceMCPServers().GetByOriginConnection(ctx, env.wsID, conn.ID)
	if err != nil || server == nil {
		t.Fatalf("load server: %v (%v)", server, err)
	}
	agent := &domain.Agent{WorkspaceID: env.wsID, Slug: "atlas", Name: "Atlas", EnabledMCPS: []string{server.ID}}
	if err := env.store.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("attach agent: %v", err)
	}

	// The token lapses: the connection sits expired.
	expired := conn
	expired.Status = domain.ConnectionStatusExpired
	if err := env.store.Connections().UpdateTokenLifecycle(ctx, expired); err != nil {
		t.Fatalf("expire connection: %v", err)
	}

	// Reauthorize: a NEW consent flow naming the existing connection.
	authorizeURL, err := env.svc.BeginReauthorize(ctx, env.wsID, env.userID, conn.ID)
	if err != nil {
		t.Fatalf("begin reauthorize: %v", err)
	}
	state, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	env.transport.setResponse(http.StatusOK, `{"access_token":"at-renewed-9","refresh_token":"rt-renewed-9","expires_in":7200,"scope":"read:jira-work offline_access"}`)

	res := env.svc.HandleCallback(ctx, "code-2", state.Query().Get("state"))
	if res.Status != services.OAuthCallbackConnected {
		t.Fatalf("reauthorize callback: %q (%s)", res.Status, res.Detail)
	}

	// The connection id, its server, and the attachment are preserved; only
	// the token set and status were replaced.
	after, err := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("expected the same connection row, got %v", err)
	}
	if after.Status != domain.ConnectionStatusConnected {
		t.Errorf("expected connected after reauthorization, got %q", after.Status)
	}
	refreshToken, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), after.RefreshCiphertext)
	if err != nil || string(refreshToken) != "rt-renewed-9" {
		t.Errorf("expected the rotated refresh envelope, got %q (%v)", refreshToken, err)
	}
	serverAfter, err := env.store.WorkspaceMCPServers().GetByOriginConnection(ctx, env.wsID, conn.ID)
	if err != nil || serverAfter == nil || serverAfter.ID != server.ID {
		t.Fatalf("expected the same materialized server, got %v (%v)", serverAfter, err)
	}
	agentAfter, err := env.store.Agents().ByID(ctx, env.wsID, agent.ID)
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if !slicesContainsID(agentAfter.EnabledMCPS, server.ID) {
		t.Errorf("expected the attachment preserved, got %v", agentAfter.EnabledMCPS)
	}
	row, err := env.settings.WorkspaceServerForRuntime(ctx, env.wsID, server.ID)
	if err != nil {
		t.Fatalf("runtime view: %v", err)
	}
	if row.Headers[0].Value != "Bearer at-renewed-9" {
		t.Errorf("expected the replaced token row, got %q", row.Headers[0].Value)
	}
}

func slicesContainsID(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Refresh-on-resolution (tasks.md 2.3, design.md D3)
// ---------------------------------------------------------------------------

func TestResolveCredential_WithinMarginRefreshesWithWriteThrough(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()
	conn := seedOAuthConnection(t, env, "atlassian", nil)

	value, err := env.svc.ResolveCredential(ctx, env.wsID, "atlassian")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if value != "Bearer at-new-1" {
		t.Errorf("expected the refreshed token value, got %q", value)
	}

	// Exactly one refresh-grant call carrying the decrypted refresh token.
	reqs := env.transport.captured()
	if len(reqs) != 1 {
		t.Fatalf("expected one refresh call, got %d", len(reqs))
	}
	if reqs[0].form.Get("grant_type") != "refresh_token" || reqs[0].form.Get("refresh_token") != "rt-stored-v1" {
		t.Errorf("unexpected refresh grant: %v", reqs[0].form)
	}

	// Write-through BOTH rows: the connection's lifecycle advanced and the
	// server's token row carries the fresh token.
	after, err := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("reload connection: %v", err)
	}
	if after.ExpiresAt == nil || !after.ExpiresAt.After(*conn.ExpiresAt) {
		t.Errorf("expected the stored expiry advanced, got %v", after.ExpiresAt)
	}
	refreshToken, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), after.RefreshCiphertext)
	if err != nil || string(refreshToken) != "rt-new-1" {
		t.Errorf("expected the rotated refresh envelope persisted, got %q (%v)", refreshToken, err)
	}
	row, err := env.settings.WorkspaceServerForRuntime(ctx, env.wsID, mustServerID(t, env, conn.ID))
	if err != nil {
		t.Fatalf("runtime view: %v", err)
	}
	if row.Headers[0].Value != "Bearer at-new-1" {
		t.Errorf("expected the server token row renewed, got %q", row.Headers[0].Value)
	}
}

func TestResolveCredential_OutsideMarginPassesThrough(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()
	seedOAuthConnection(t, env, "atlassian", func(c *domain.Connection) {
		expiresAt := time.Now().Add(30 * time.Minute) // well outside the 10m margin
		c.ExpiresAt = &expiresAt
	})

	value, err := env.svc.ResolveCredential(ctx, env.wsID, "atlassian")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if value != "Bearer at-old-1" {
		t.Errorf("expected the stored token passthrough, got %q", value)
	}
	if reqs := env.transport.captured(); len(reqs) != 0 {
		t.Errorf("expected no refresh calls outside the margin, got %d", len(reqs))
	}
}

func TestResolveCredential_RefreshWithoutRotationKeepsEnvelope(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()
	seedOAuthConnection(t, env, "atlassian", nil)

	env.transport.setResponse(http.StatusOK, `{"access_token":"at-new-2","expires_in":1800}`)
	if _, err := env.svc.ResolveCredential(ctx, env.wsID, "atlassian"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	after, err := env.store.Connections().GetByService(ctx, env.wsID, "atlassian")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	refreshToken, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), after.RefreshCiphertext)
	if err != nil || string(refreshToken) != "rt-stored-v1" {
		t.Errorf("an omitted refresh token must keep the stored envelope, got %q (%v)", refreshToken, err)
	}
}

// TestResolveCredential_RecipeMarginUsesRecipeValue proves the margin comes
// from the recipe when it declares one: 30 minutes out is beyond the default
// 10m margin but inside this recipe's 1h.
func TestResolveCredential_RecipeMarginUsesRecipeValue(t *testing.T) {
	domain.RegisterRecipe(domain.Recipe{
		ID:            "margin-provider",
		Service:       "Margin Provider",
		Icon:          "margin",
		AuthKind:      domain.RecipeAuthOAuth,
		Availability:  domain.RecipeComingSoon,
		Transport:     domain.MCPTransportStreamableHTTP,
		Endpoint:      "https://margin.example.com/mcp",
		TokenHeader:   "Authorization",
		TokenScheme:   "Bearer",
		AuthorizeURL:  "https://margin.example.com/authorize",
		TokenURL:      "https://margin.example.com/oauth/token",
		RefreshMargin: time.Hour,
		AccessLevels:  []string{domain.ConnectionAccessReadOnly},
		Probe:         domain.RecipeProbe{Tool: "list_things"},
	})

	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "margin-provider")
	ctx := context.Background()
	seedOAuthConnection(t, env, "margin-provider", func(c *domain.Connection) {
		expiresAt := time.Now().Add(30 * time.Minute)
		c.ExpiresAt = &expiresAt
	})

	if _, err := env.svc.ResolveCredential(ctx, env.wsID, "margin-provider"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if reqs := env.transport.captured(); len(reqs) != 1 {
		t.Fatalf("expected the recipe's 1h margin to trigger the refresh, got %d calls", len(reqs))
	}
}

func TestResolveCredential_FailureExpiresConnection(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()
	conn := seedOAuthConnection(t, env, "atlassian", nil)

	env.transport.setResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"refresh token is revoked"}`)
	_, err := env.svc.ResolveCredential(ctx, env.wsID, "atlassian")
	if err == nil || !strings.Contains(err.Error(), "refresh token is revoked") {
		t.Fatalf("expected the provider error verbatim, got %v", err)
	}

	// The expired transition persisted on the connection row...
	after, err := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Status != domain.ConnectionStatusExpired {
		t.Fatalf("expected the expired status, got %q", after.Status)
	}
	// ...and the provider error rides the server row's status detail (the
	// connection row deliberately carries no error column).
	serverID := mustServerID(t, env, conn.ID)
	servers, _ := env.store.WorkspaceMCPServers().List(ctx, env.wsID)
	for _, srv := range servers {
		if srv.ID == serverID && (srv.Status != domain.MCPStatusError || !strings.Contains(srv.StatusError, "refresh token is revoked")) {
			t.Errorf("expected the provider error on the server row, got %q/%q", srv.Status, srv.StatusError)
		}
	}

	// The view prefers the persisted expired status over the server row's.
	view, err := env.svc.Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("get view: %v", err)
	}
	if view.Status != domain.ConnectionStatusExpired {
		t.Errorf("expected the expired status surfaced, got %q", view.Status)
	}
	if !strings.Contains(view.StatusError, "refresh token is revoked") {
		t.Errorf("expected the provider error surfaced, got %q", view.StatusError)
	}

	// Repeated failure is idempotent (expired → expired is legal), and the
	// expired status survives — probe outcomes never mask it.
	env.transport.setResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"still revoked"}`)
	if _, err := env.svc.ResolveCredential(ctx, env.wsID, "atlassian"); err == nil {
		t.Fatal("expected the repeated refresh to fail again")
	}
	final, _ := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if final.Status != domain.ConnectionStatusExpired {
		t.Errorf("expected expired held idempotently, got %q", final.Status)
	}
}

func mustServerID(t *testing.T, env *oauthFlowEnv, connectionID string) string {
	t.Helper()
	srv, err := env.store.WorkspaceMCPServers().GetByOriginConnection(context.Background(), env.wsID, connectionID)
	if err != nil || srv == nil {
		t.Fatalf("expected the linked server, got %v (%v)", srv, err)
	}
	return srv.ID
}

func mustConnectionID(t *testing.T, env *oauthFlowEnv, service string) string {
	t.Helper()
	conn, err := env.store.Connections().GetByService(context.Background(), env.wsID, service)
	if err != nil {
		t.Fatalf("expected the %s connection, got %v", service, err)
	}
	return conn.ID
}

// ---------------------------------------------------------------------------
// Runtime credential source (refresh-on-resolution on the run path,
// tasks.md 2.3: an attached agent's run resolving the connection's
// credentials). The adapter wraps the real MCP settings service and is what
// the composition root feeds the runner's MCP policy/status writer and the
// hooks invoker.
// ---------------------------------------------------------------------------

func TestRuntimeCredentialSource_WithinMarginRefreshesBothRows(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()
	conn := seedOAuthConnection(t, env, "atlassian", nil)
	serverID := mustServerID(t, env, conn.ID)

	// The runtime resolution path (what the MCP policy/invoker call per dial).
	source := services.NewRuntimeCredentialSource(env.settings, env.svc)
	row, err := source.WorkspaceServerForRuntime(ctx, env.wsID, serverID)
	if err != nil {
		t.Fatalf("runtime resolution: %v", err)
	}
	if len(row.Headers) != 1 || row.Headers[0].Value != "Bearer at-new-1" {
		t.Fatalf("expected the fresh token from the runtime resolution, got %+v", row.Headers)
	}
	if reqs := env.transport.captured(); len(reqs) != 1 || reqs[0].form.Get("grant_type") != "refresh_token" {
		t.Fatalf("expected one refresh grant on resolution, got %+v", reqs)
	}

	// The dual write-through: the connection's lifecycle advanced AND the
	// server's token row carries the fresh token (asserted via the returned
	// row above; re-read from the store to be sure it persisted).
	after, err := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("reload connection: %v", err)
	}
	if after.ExpiresAt == nil || !after.ExpiresAt.After(*conn.ExpiresAt) {
		t.Errorf("expected the stored expiry advanced, got %v", after.ExpiresAt)
	}
	refreshToken, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), after.RefreshCiphertext)
	if err != nil || string(refreshToken) != "rt-new-1" {
		t.Errorf("expected the rotated refresh envelope persisted, got %q (%v)", refreshToken, err)
	}
	stored, err := env.store.WorkspaceMCPServers().Get(ctx, env.wsID, serverID)
	if err != nil {
		t.Fatalf("reload server: %v", err)
	}
	if !strings.HasPrefix(stored.Headers[0].Value, "v1:") {
		t.Errorf("expected the renewed row stored as an envelope, got %q", stored.Headers[0].Value)
	}
}

func TestRuntimeCredentialSource_OutsideMarginReturnsStoredCredential(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()
	seedOAuthConnection(t, env, "atlassian", func(c *domain.Connection) {
		expiresAt := time.Now().Add(30 * time.Minute)
		c.ExpiresAt = &expiresAt
	})
	serverID := mustServerID(t, env, mustConnectionID(t, env, "atlassian"))

	source := services.NewRuntimeCredentialSource(env.settings, env.svc)
	row, err := source.WorkspaceServerForRuntime(ctx, env.wsID, serverID)
	if err != nil {
		t.Fatalf("runtime resolution: %v", err)
	}
	if row.Headers[0].Value != "Bearer at-old-1" {
		t.Errorf("expected the stored credential passthrough, got %q", row.Headers[0].Value)
	}
	if reqs := env.transport.captured(); len(reqs) != 0 {
		t.Errorf("expected no refresh calls outside the margin, got %d", len(reqs))
	}
}

func TestRuntimeCredentialSource_PluralResolutionRefreshesOnlyLinkedRows(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()
	seedOAuthConnection(t, env, "atlassian", nil) // inside the default margin
	handMade := &domain.WorkspaceMCPServer{
		WorkspaceID:   env.wsID,
		Name:          "Hand Made",
		Enabled:       true,
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "/bin/true", Env: []domain.EnvRow{{Name: "API_KEY", Value: "hand-secret-1"}}},
	}
	if err := env.settings.CreateWorkspaceServer(ctx, handMade); err != nil {
		t.Fatalf("seed hand-made server: %v", err)
	}

	// The policy's per-run resolution (SettingsRuntime's plural accessor).
	source := services.NewRuntimeCredentialSource(env.settings, env.svc)
	rows, err := source.WorkspaceServersForRuntime(ctx, env.wsID)
	if err != nil {
		t.Fatalf("plural resolution: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected both servers resolved, got %d", len(rows))
	}
	for _, row := range rows {
		switch row.Name {
		case "Atlassian":
			if row.Headers[0].Value != "Bearer at-new-1" {
				t.Errorf("expected the linked row refreshed, got %q", row.Headers[0].Value)
			}
		case "Hand Made":
			if row.Env[0].Value != "hand-secret-1" {
				t.Errorf("expected the unlinked row decrypted untouched, got %q", row.Env[0].Value)
			}
		}
	}
	if reqs := env.transport.captured(); len(reqs) != 1 {
		t.Errorf("expected exactly one refresh (the linked row only), got %d", len(reqs))
	}
}

func TestRuntimeCredentialSource_FailedRefreshFailsOpenAndFlipsExpired(t *testing.T) {
	env := newOAuthFlowEnv(t)
	registerProviderApp(t, env, "atlassian")
	ctx := context.Background()
	conn := seedOAuthConnection(t, env, "atlassian", nil)
	serverID := mustServerID(t, env, conn.ID)
	env.transport.setResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"refresh token is revoked"}`)

	// FAIL-OPEN: the resolution succeeds with the STORED credential — a
	// refresh failure must never break the dial (the run degrades exactly as
	// it does for an errored MCP server).
	source := services.NewRuntimeCredentialSource(env.settings, env.svc)
	row, err := source.WorkspaceServerForRuntime(ctx, env.wsID, serverID)
	if err != nil {
		t.Fatalf("expected the stored credential fail-open, got error %v", err)
	}
	if row.Headers[0].Value != "Bearer at-old-1" {
		t.Errorf("expected the stored credential, got %q", row.Headers[0].Value)
	}

	// ...while the expired transition and the provider error persisted.
	after, err := env.store.Connections().Get(ctx, env.wsID, conn.ID)
	if err != nil {
		t.Fatalf("reload connection: %v", err)
	}
	if after.Status != domain.ConnectionStatusExpired {
		t.Errorf("expected the expired status flipped, got %q", after.Status)
	}
	srv, err := env.store.WorkspaceMCPServers().Get(ctx, env.wsID, serverID)
	if err != nil {
		t.Fatalf("reload server: %v", err)
	}
	if srv.Status != domain.MCPStatusError || !strings.Contains(srv.StatusError, "refresh token is revoked") {
		t.Errorf("expected the provider error persisted on the server row, got %q/%q", srv.Status, srv.StatusError)
	}
}

// ---------------------------------------------------------------------------
// Gallery availability (tasks.md 2.5)
// ---------------------------------------------------------------------------

func TestEnrichRecipes_AvailabilityFollowsRegistration(t *testing.T) {
	env := newOAuthFlowEnv(t)
	ctx := context.Background()

	findRecipe := func(id string) *domain.Recipe {
		for i := range domain.Recipes() {
			r := domain.Recipes()[i]
			if r.ID == id {
				return &r
			}
		}
		return nil
	}

	// Unregistered: the OAuth recipe stays a coming-soon card...
	recipes := env.svc.EnrichRecipes(ctx)
	for _, r := range recipes {
		if r.ID == "atlassian" && r.Availability != domain.RecipeComingSoon {
			t.Errorf("expected atlassian coming_soon without a registration, got %q", r.Availability)
		}
	}
	// ...and PAT recipes are untouched.
	if r := findRecipe("github"); r == nil || r.Availability != domain.RecipeAvailable {
		t.Errorf("expected github untouched (available), got %+v", findRecipe("github"))
	}

	// The registration alone flips the card — no code change (spec).
	registerProviderApp(t, env, "atlassian")
	recipes = env.svc.EnrichRecipes(ctx)
	for _, r := range recipes {
		if r.ID == "atlassian" && r.Availability != domain.RecipeAvailable {
			t.Errorf("expected atlassian available after registration, got %q", r.Availability)
		}
		if r.ID == "slack" && r.Availability != domain.RecipeComingSoon {
			t.Errorf("expected slack still coming_soon, got %q", r.Availability)
		}
	}
}

// ---------------------------------------------------------------------------
// Instance OAuth apps service (tasks.md 2.6)
// ---------------------------------------------------------------------------

func TestOAuthAppsService_UpsertLifecycle(t *testing.T) {
	env := newOAuthFlowEnv(t)
	ctx := context.Background()

	// A first registration without a secret is refused.
	if _, err := env.appsSvc.Upsert(ctx, "atlassian", testClientID, ""); err == nil || !strings.Contains(err.Error(), "client secret is required") {
		t.Fatalf("expected the first-registration secret requirement, got %v", err)
	}

	view, err := env.appsSvc.Upsert(ctx, "atlassian", testClientID, testClientSecret)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if view.Provider != "atlassian" || view.ClientID != testClientID {
		t.Errorf("unexpected view: provider=%q client_id=%q", view.Provider, view.ClientID)
	}
	if view.ClientSecretHint != "9999" {
		t.Errorf("expected the last-4 hint, got %q", view.ClientSecretHint)
	}
	if view.RedirectURI != services.DeriveOAuthRedirectURI(testPublicBaseURL) {
		t.Errorf("expected the derived redirect uri, got %q", view.RedirectURI)
	}

	// At rest the secret exists only as the instance-scoped (EMPTY AAD)
	// envelope (contract §4).
	stored, err := env.store.OAuthApps().Get(ctx, "atlassian")
	if err != nil {
		t.Fatalf("load stored row: %v", err)
	}
	secret, err := secrets.Decrypt([]byte(testEncKey), nil, stored.ClientSecretCiphertext)
	if err != nil || string(secret) != testClientSecret {
		t.Errorf("expected the empty-AAD instance envelope, got %q (%v)", secret, err)
	}

	// An update: created_at is fixed at birth, updated_at advances, and an
	// omitted secret keeps the stored credential.
	createdAt := view.CreatedAt
	updated, err := env.appsSvc.Upsert(ctx, "atlassian", "client-id-2", "new-secret-8888")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !updated.CreatedAt.Equal(createdAt) {
		t.Errorf("expected created_at fixed at birth, got %v vs %v", updated.CreatedAt, createdAt)
	}
	if updated.ClientSecretHint != "8888" {
		t.Errorf("expected the new hint, got %q", updated.ClientSecretHint)
	}
	kept, err := env.appsSvc.Upsert(ctx, "atlassian", "client-id-2", "")
	if err != nil {
		t.Fatalf("update without secret: %v", err)
	}
	if kept.ClientSecretHint != "8888" {
		t.Errorf("expected the stored credential kept, got %q", kept.ClientSecretHint)
	}

	// The registry rejects providers that are not OAuth recipes.
	if _, err := env.appsSvc.Upsert(ctx, "acme", "id", "sec"); err == nil || !strings.Contains(err.Error(), "not an OAuth integration provider") {
		t.Errorf("expected the unknown-provider rejection, got %v", err)
	}
	if _, err := env.appsSvc.Upsert(ctx, "github", "id", "sec"); err == nil {
		t.Error("expected the PAT provider rejection")
	}

	// List is provider-ordered and hint-only.
	_, err = env.appsSvc.Upsert(ctx, "linear", "lin-id", "lin-secret-7777")
	if err != nil {
		t.Fatalf("upsert linear: %v", err)
	}
	views, err := env.appsSvc.List(ctx)
	if err != nil || len(views) != 2 {
		t.Fatalf("expected two registered apps, got %d (%v)", len(views), err)
	}
	if views[0].Provider != "atlassian" || views[1].Provider != "linear" {
		t.Errorf("expected provider-ordered listing, got %q then %q", views[0].Provider, views[1].Provider)
	}
}
