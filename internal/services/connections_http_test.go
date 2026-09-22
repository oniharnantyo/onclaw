package services_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// ---------------------------------------------------------------------------
// HTTP-kind connections (add-connection-http tasks 5.1): kind-aware connect
// (no server row, encrypted token on the row), the recipe probe call as the
// connect gate (store-nothing hygiene), probe-on-demand status persistence
// through the lifecycle write, disconnect stripping the connection id from
// agents' enabled_mcps, CredentialForConnection, and the agent-scoped
// attached-http listing — fake stores, real crypto, the upstream stubbed by
// an httptest server the test-registered recipe points at (recipes are data).
// ---------------------------------------------------------------------------

const (
	httpTestToken = "stub-live-token-4321"
	httpTestHint  = "4321"
)

// capturedUpstreamRequest records one upstream call: method, request URI
// (path + query), and the auth header value.
type capturedUpstreamRequest struct {
	method    string
	uri       string
	authValue string
}

// stubUpstream is the recipe's base URL: it records every request and serves
// a flip-able status/body.
type stubUpstream struct {
	srv      *httptest.Server
	mu       sync.Mutex
	status   int
	body     string
	requests []capturedUpstreamRequest
}

func newStubUpstream() *stubUpstream {
	su := &stubUpstream{status: http.StatusOK, body: `{"ok":true}`}
	su.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		su.mu.Lock()
		su.requests = append(su.requests, capturedUpstreamRequest{
			method:    r.Method,
			uri:       r.URL.RequestURI(),
			authValue: r.Header.Get("X-Stub-Token"),
		})
		status, body := su.status, su.body
		su.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return su
}

func (su *stubUpstream) setResponse(status int, body string) {
	su.mu.Lock()
	defer su.mu.Unlock()
	su.status, su.body = status, body
}

func (su *stubUpstream) captured() []capturedUpstreamRequest {
	su.mu.Lock()
	defer su.mu.Unlock()
	return append([]capturedUpstreamRequest(nil), su.requests...)
}

// stubHTTPVerbs builds the recipe's declared verbs — service-prefixed tool
// names (validation requires the <recipe id>. prefix).
func stubHTTPVerbs(id string) []domain.RecipeVerb {
	return []domain.RecipeVerb{
		{
			Tool:        id + ".get_things",
			Method:      "GET",
			Path:        "/v1/things",
			Description: "List the things the token can see.",
		},
		{
			Tool:   id + ".get_thing",
			Method: "GET",
			Path:   "/v1/things/{thing_id}",
			Params: []domain.RecipeVerbParam{
				{Name: "thing_id", Type: domain.RecipeParamString, Required: true, In: domain.RecipeParamInPath},
				{Name: "verbose", Type: domain.RecipeParamBoolean, Required: false, In: domain.RecipeParamInQuery},
			},
			Description: "Get one thing.",
		},
	}
}

// registerHTTPTestRecipe registers one http-kind recipe pointed at the stub
// upstream (the registry is global and panics on duplicates, so every caller
// passes a unique id).
func registerHTTPTestRecipe(t *testing.T, id, baseURL, tokenScheme string) *domain.Recipe {
	t.Helper()
	domain.RegisterRecipe(domain.Recipe{
		ID:           id,
		Service:      "Stub HTTP " + id,
		Icon:         "stub",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindHTTP,
		BaseURL:      baseURL,
		TokenHeader:  "X-Stub-Token",
		TokenScheme:  tokenScheme,
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Generate a stub token"}},
		Probe:        domain.RecipeProbe{Method: "GET", Path: "/v1/me"},
		Verbs:        stubHTTPVerbs(id),
	})
	recipe := domain.RecipeByID(id)
	if recipe == nil {
		t.Fatalf("recipe %q did not register", id)
	}
	return recipe
}

// connectHTTPStub connects the given http recipe and returns the view.
func connectHTTPStub(t *testing.T, env *connectionsTestEnv, recipeID, token string) (*services.ConnectionView, error) {
	t.Helper()
	res, err := env.svc.Connect(context.Background(), env.wsID, testUserID, recipeID, "", token)
	if err != nil {
		return nil, err
	}
	return res.Connection, nil
}

// Kind-aware connect (tasks 3.1): probe executes the recipe's call with the
// composed auth header, the connection persists, and NO server row or origin
// marker exists. The view carries the http shape: no server id, server
// enabled false, verb-count tool count, hint-only token.
func TestHTTPConnect_KindAwareCreateSkipsMaterialization(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-create", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	if view.Service != recipe.ID || view.Status != domain.ConnectionStatusConnected {
		t.Errorf("expected a connected %s connection, got %s/%s", recipe.ID, view.Service, view.Status)
	}
	if view.ServerID != "" {
		t.Errorf("http connections carry no server id, got %q", view.ServerID)
	}
	if view.ServerEnabled == nil || *view.ServerEnabled {
		t.Error("expected server_enabled false on the http view")
	}
	if view.ToolCount != len(recipe.Verbs) {
		t.Errorf("expected tool_count %d (declared verbs), got %d", len(recipe.Verbs), view.ToolCount)
	}
	if view.TokenHint != httpTestHint {
		t.Errorf("expected last-4 hint %q, got %q", httpTestHint, view.TokenHint)
	}
	if view.AttachedAgents == nil || len(view.AttachedAgents) != 0 {
		t.Errorf("expected an empty attached-agents array, got %v", view.AttachedAgents)
	}

	// No materialization: no server row, no origin marker.
	server, err := env.store.WorkspaceMCPServers().GetByOriginConnection(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("get by origin: %v", err)
	}
	if server != nil {
		t.Errorf("http connect must not materialize a server row, got %+v", server)
	}
	rows, err := env.store.WorkspaceMCPServers().List(ctx, env.wsID)
	if err != nil || len(rows) != 0 {
		t.Errorf("expected an empty workspace MCP registry, got %v (%v)", rows, err)
	}

	// The probe executed the recipe's declared call with the composed header.
	reqs := upstream.captured()
	if len(reqs) != 1 {
		t.Fatalf("expected exactly one upstream probe call, got %d", len(reqs))
	}
	if reqs[0].method != "GET" || reqs[0].uri != "/v1/me" {
		t.Errorf("expected GET /v1/me, got %s %s", reqs[0].method, reqs[0].uri)
	}
	if reqs[0].authValue != httpTestToken {
		t.Errorf("expected the raw token under X-Stub-Token (empty scheme), got %q", reqs[0].authValue)
	}

	// The token is stored encrypted, workspace-scoped, and decrypts to the
	// raw token only through the established envelope.
	stored, err := env.store.Connections().Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("load stored connection: %v", err)
	}
	if stored.RefreshCiphertext == "" || stored.RefreshCiphertext == httpTestToken {
		t.Error("expected an encrypted envelope on the connection row")
	}
	plaintext, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), stored.RefreshCiphertext)
	if err != nil || string(plaintext) != httpTestToken {
		t.Errorf("expected the raw token decryptable with the workspace AAD, got %q (%v)", plaintext, err)
	}
}

// The TokenScheme composition rule is the change-1 rule: scheme prefixes the
// header value.
func TestHTTPConnect_SchemeComposesHeaderValue(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-bearer", upstream.srv.URL, "Bearer")
	env := newConnectionsTestEnv(t)

	if _, err := connectHTTPStub(t, env, recipe.ID, httpTestToken); err != nil {
		t.Fatalf("connect: %v", err)
	}
	reqs := upstream.captured()
	if len(reqs) != 1 {
		t.Fatalf("expected one probe call, got %d", len(reqs))
	}
	if want := "Bearer " + httpTestToken; reqs[0].authValue != want {
		t.Errorf("expected header value %q, got %q", want, reqs[0].authValue)
	}
}

// Probe failure blocks connect with store-nothing hygiene (tasks 3.1): no
// connection row, no server row, and the upstream message rides the
// ErrProbeFailed wrap — never the token.
func TestHTTPConnect_ProbeFailureStoresNothing(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	upstream.setResponse(http.StatusUnauthorized, `{"err":"this Personal Access Token is not valid"}`)
	recipe := registerHTTPTestRecipe(t, "stubhttp-failgate", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	_, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if !errors.Is(err, services.ErrProbeFailed) {
		t.Fatalf("expected ErrProbeFailed, got %v", err)
	}
	for _, want := range []string{"401 Unauthorized", "this Personal Access Token is not valid"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to carry %q, got %q", want, err.Error())
		}
	}
	if strings.Contains(err.Error(), httpTestToken) {
		t.Error("probe failure leaked the token")
	}

	connections, err := env.store.Connections().List(ctx, env.wsID)
	if err != nil || len(connections) != 0 {
		t.Errorf("probe failure stored no connection, got %v (%v)", connections, err)
	}
	rows, err := env.store.WorkspaceMCPServers().List(ctx, env.wsID)
	if err != nil || len(rows) != 0 {
		t.Errorf("probe failure stored no server row, got %v (%v)", rows, err)
	}
}

// Probe on demand (tasks 3.2): re-runs the recipe call with the STORED
// credential and persists the outcome through the lifecycle write — which
// must preserve the token envelope (the write path never wipes the
// credential).
func TestHTTPProbeOnDemand_PersistsStatusAndKeepsCredential(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-probe", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	upstream.setResponse(http.StatusInternalServerError, `{"message":"upstream is having a moment"}`)
	failed, err := env.svc.Probe(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("probe must return the status, not an error: %v", err)
	}
	if failed.Status != domain.ConnectionStatusError {
		t.Errorf("expected persisted error status, got %q", failed.Status)
	}
	if failed.StatusError != "the provider answered 500 Internal Server Error: upstream is having a moment" {
		t.Errorf("expected the status code and provider message as status detail, got %q", failed.StatusError)
	}
	stored, err := env.store.Connections().Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Status != domain.ConnectionStatusError {
		t.Errorf("expected the error status persisted on the row, got %q", stored.Status)
	}
	// The lifecycle write preserved the encrypted credential.
	plaintext, err := secrets.Decrypt([]byte(testEncKey), []byte(env.wsID), stored.RefreshCiphertext)
	if err != nil || string(plaintext) != httpTestToken {
		t.Errorf("lifecycle write wiped or broke the credential: %q (%v)", plaintext, err)
	}

	// Recovery: the probe dials the stored credential and flips connected ↔
	// error through the same write path.
	upstream.setResponse(http.StatusOK, `{"ok":true}`)
	recovered, err := env.svc.Probe(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("recovery probe: %v", err)
	}
	if recovered.Status != domain.ConnectionStatusConnected || recovered.StatusError != "" {
		t.Errorf("expected a clean connected view, got %+v", recovered)
	}
	reqs := upstream.captured()
	if len(reqs) != 3 {
		t.Fatalf("expected connect + 2 probes upstream, got %d", len(reqs))
	}
	if reqs[2].authValue != httpTestToken {
		t.Errorf("expected the stored raw token on the probe dial, got %q", reqs[2].authValue)
	}
}

// Disconnect (tasks 3.2): the http connection id is stripped from every
// agent's enabled_mcps while other attachments (an MCP server id, an inert
// reference) survive; no server row is involved.
func TestHTTPDisconnect_StripsConnectionIDFromAgents(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-disc", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// A hand-made workspace server row: the contrast attachment an http
	// disconnect must NOT strip.
	handMade := &domain.WorkspaceMCPServer{
		WorkspaceID: env.wsID,
		Name:        "hand-made-http-disc",
		Enabled:     true,
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStdio,
			Command:   "/bin/true",
		},
	}
	if err := env.store.WorkspaceMCPServers().Create(ctx, handMade); err != nil {
		t.Fatalf("seed hand-made server: %v", err)
	}
	agent := &domain.Agent{
		WorkspaceID: env.wsID,
		Name:        "Atlas",
		Slug:        "atlas-http-disc",
		EnabledMCPS: []string{view.ID, handMade.ID, "inert-reference-id"},
	}
	if err := env.store.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	serverID, err := env.svc.Disconnect(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if serverID != "" {
		t.Errorf("http disconnect carries no server id, got %q", serverID)
	}

	reloaded, err := env.store.Agents().ByID(ctx, env.wsID, agent.ID)
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	joined := strings.Join(reloaded.EnabledMCPS, ",")
	if strings.Contains(joined, view.ID) {
		t.Error("expected the connection id stripped from enabled_mcps")
	}
	if len(reloaded.EnabledMCPS) != 2 {
		t.Fatalf("expected the other attachments to survive, got %v", reloaded.EnabledMCPS)
	}
	for _, want := range []string{handMade.ID, "inert-reference-id"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q to survive the strip, got %v", want, reloaded.EnabledMCPS)
		}
	}
	if _, err := env.store.Connections().Get(ctx, env.wsID, view.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected the connection gone, got %v", err)
	}
	rows, _ := env.store.WorkspaceMCPServers().List(ctx, env.wsID)
	if len(rows) != 1 || rows[0].ID != handMade.ID {
		t.Errorf("expected the hand-made server untouched, got %v", rows)
	}
}

// CredentialForConnection (contract §3): raw decrypted token for the
// connection's own workspace; unknown, cross-workspace, non-http, and
// unopenable credentials are errors that never quote material.
func TestCredentialForConnection_RawTokenAndErrorPaths(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-cred", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	token, err := env.svc.CredentialForConnection(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	if token != httpTestToken {
		t.Errorf("expected the RAW token (no scheme composition), got %q", token)
	}

	if _, err := env.svc.CredentialForConnection(ctx, env.wsID, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown id expected ErrNotFound, got %v", err)
	}

	// Cross-workspace: a second workspace cannot resolve the connection.
	ws2 := &domain.Workspace{Slug: "conn-http-ws-2", Name: "Other WS", Timezone: "UTC"}
	if err := env.store.Workspaces().Create(ctx, ws2); err != nil {
		t.Fatalf("seed second workspace: %v", err)
	}
	if _, err := env.svc.CredentialForConnection(ctx, ws2.ID, view.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-workspace id expected ErrNotFound, got %v", err)
	}

	// An MCP-kind connection id does not carry http verb tools.
	mcpView, err := connectView(t, env, "github", "", "ghp-some-pat")
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	if _, err := env.svc.CredentialForConnection(ctx, env.wsID, mcpView.ID); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected a non-not-found naming error for the mcp-kind id, got %v", err)
	}

	// An unopenable credential errors without leaking the envelope or token.
	// The row lives in the second workspace so the one-per-service uniqueness
	// of the first is untouched.
	tampered := &domain.Connection{
		WorkspaceID:       ws2.ID,
		Service:           recipe.ID,
		AccessLevel:       domain.ConnectionAccessReadOnly,
		RefreshCiphertext: "v1:not-base64!!:c2l4dGVlbg",
	}
	if err := env.store.Connections().Create(ctx, tampered); err != nil {
		t.Fatalf("seed tampered row: %v", err)
	}
	_, err = env.svc.CredentialForConnection(ctx, ws2.ID, tampered.ID)
	if err == nil {
		t.Fatal("expected an error for the tampered envelope")
	}
	if strings.Contains(err.Error(), httpTestToken) || strings.Contains(err.Error(), tampered.RefreshCiphertext) {
		t.Errorf("error leaked credential material: %q", err.Error())
	}
}

// AttachedHTTPConnections (contract §2): server-first disambiguation over the
// agent's enabled_mcps — only http-kind connection ids come back, scoped to
// the one agent.
func TestAttachedHTTPConnections_ServerFirstAndScoped(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-attach", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	httpView, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("http connect: %v", err)
	}
	mcpView, err := connectView(t, env, "github", "", "ghp-some-pat")
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}

	agent := &domain.Agent{
		WorkspaceID: env.wsID,
		Name:        "Beacon",
		Slug:        "beacon-http",
		EnabledMCPS: []string{httpView.ID, mcpView.ServerID, "inert-reference-id"},
	}
	if err := env.store.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	plain := &domain.Agent{WorkspaceID: env.wsID, Name: "Plain", Slug: "plain-http"}
	if err := env.store.Agents().Create(ctx, plain); err != nil {
		t.Fatalf("seed plain agent: %v", err)
	}

	attached, err := env.svc.AttachedHTTPConnections(ctx, env.wsID, agent.ID)
	if err != nil {
		t.Fatalf("attached: %v", err)
	}
	if len(attached) != 1 || attached[0].ID != httpView.ID {
		t.Fatalf("expected exactly the http connection, got %+v", attached)
	}
	if attached[0].Service != recipe.ID {
		t.Errorf("expected the recipe id on the ref, got %q", attached[0].Service)
	}
	if attached[0].Status != domain.ConnectionStatusConnected {
		t.Errorf("expected the persisted status on the ref, got %q", attached[0].Status)
	}

	empty, err := env.svc.AttachedHTTPConnections(ctx, env.wsID, plain.ID)
	if err != nil || len(empty) != 0 {
		t.Errorf("expected an empty listing for the unattached agent, got %v (%v)", empty, err)
	}

	if _, err := env.svc.AttachedHTTPConnections(ctx, env.wsID, "no-such-agent"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown agent expected ErrNotFound, got %v", err)
	}
}

// Status hygiene on the view: a probe failure's provider message is response
// detail; the next plain read carries the persisted status without it (the
// connection row has no error column — the detail is view-level, not stored).
func TestHTTPView_StatusDetailIsResponseLevel(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-view", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)
	ctx := context.Background()

	view, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	upstream.setResponse(http.StatusForbidden, `{"error":"token lacks file access"}`)
	if _, err := env.svc.Probe(ctx, env.wsID, view.ID); err != nil {
		t.Fatalf("probe: %v", err)
	}

	fresh, err := env.svc.Get(ctx, env.wsID, view.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fresh.Status != domain.ConnectionStatusError {
		t.Errorf("expected the persisted error status, got %q", fresh.Status)
	}
	if fresh.StatusError != "" {
		t.Errorf("expected no stored status detail on a fresh read, got %q", fresh.StatusError)
	}

	// List is kind-aware too: http rows keep the http shape.
	list, err := env.svc.List(ctx, env.wsID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v (%v)", list, err)
	}
	if list[0].ServerID != "" || list[0].ServerEnabled == nil || *list[0].ServerEnabled || list[0].ToolCount != len(recipe.Verbs) {
		t.Errorf("list view lost the http shape: %+v", list[0])
	}
}

// Duplicate http connect is the same one-per-service conflict.
func TestHTTPConnect_DuplicateIsConflict(t *testing.T) {
	upstream := newStubUpstream()
	defer upstream.srv.Close()
	recipe := registerHTTPTestRecipe(t, "stubhttp-dup", upstream.srv.URL, "")
	env := newConnectionsTestEnv(t)

	first, err := connectHTTPStub(t, env, recipe.ID, httpTestToken)
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}
	_, err = connectHTTPStub(t, env, recipe.ID, "another-token")
	if !errors.Is(err, domain.ErrConnectionExists) {
		t.Fatalf("expected ErrConnectionExists, got %v", err)
	}
	if !strings.Contains(err.Error(), first.ID) {
		t.Errorf("expected the conflict to name the existing connection, got %q", err.Error())
	}
}
