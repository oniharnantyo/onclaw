package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// ---------------------------------------------------------------------------
// Connect request/response plumbing for the base-URL origin
// (add-recipe-base-url tasks.md 2.4): the connect request carries the optional
// origin field; the service validates it and the response view exposes the
// stored, resolved origin. Validation failures are the standard 400
// invalid_request envelope, conflicts the 409 conflict envelope naming the
// origin — gin-level tests over the same fake-store service assembly the
// router builds, middleware replaced by direct context seeding.
// ---------------------------------------------------------------------------

const connHandlerEncKey = "01234567890123456789012345678901"

// handlerTestProber is the stubbed MCP probe: connect-time dials never leave
// the process.
func handlerTestProber(context.Context, string, string, string, domain.MCPConnection) (int, error) {
	return 3, nil
}

// noopInvalidator plays the MCP cache seam; connect-only flows never fire it.
type noopInvalidator struct{}

func (noopInvalidator) Invalidate(string, string) {}

// newConnectionsHandlerEnv wires the connections handlers the way the router
// does, with the workspace/user middleware replaced by seeded context values.
func newConnectionsHandlerEnv(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	return newConnectionsHandlerEnvWithProber(t, handlerTestProber)
}

// newConnectionsHandlerEnvWithProber is the env variant with an injected
// probe stub — the token-replacement tests need a failing dial.
func newConnectionsHandlerEnvWithProber(t *testing.T, prober services.ConnectionProber) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	user := &domain.User{Email: "connector@example.com", Name: "Connector"}
	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	settings := agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), []byte(connHandlerEncKey))
	svc := services.NewConnectionsService(
		st.Connections(),
		st.WorkspaceMCPServers(),
		st.Agents(),
		settings,
		st.OAuthApps(),
		[]byte(connHandlerEncKey),
		"",
		services.WithProber(prober),
		// The router's attachment tx seam (add-connection-edit D1) over the
		// fake's WithTx — the test assembly mirrors the composition root.
		services.WithAttachmentTx(func(ctx context.Context, run func(ctx context.Context, agents store.AgentStore) error) error {
			return st.WithTx(ctx, func(s store.Store) error {
				return run(ctx, s.Agents())
			})
		}),
	)
	h := handlers.NewConnectionsHandlers(svc, noopInvalidator{}, nil, "")

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.UserContextKey, user)
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.POST("/integrations/connections", h.Connect)
	r.GET("/integrations/connections", h.ListConnections)
	r.GET("/integrations/connections/:id", h.GetConnection)
	r.PUT("/integrations/connections/:id/agents", h.SetConnectionAgents)
	r.POST("/integrations/connections/:id/token", h.ReplaceConnectionToken)
	return r, st, ws
}

// registerHandlerOriginRecipe registers one mcp-kind PAT recipe declaring an
// origin parameter (unique id — the registry is global).
func registerHandlerOriginRecipe(t *testing.T, id string) {
	t.Helper()
	domain.RegisterRecipe(domain.Recipe{
		ID:           id,
		Service:      "Stub Origin " + id,
		Icon:         "stub",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindMCP,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://stub.example.com/api/v4/mcp",
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		OriginParam: &domain.RecipeOriginParam{
			Name:    "Instance base URL",
			Default: "https://stub.example.com",
			Help:    "The provider instance's origin (SaaS or self-managed).",
		},
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Generate a stub token"}},
		Probe:        domain.RecipeProbe{Tool: "stub.ping"},
	})
}

// registerHandlerPlainRecipe registers one param-less mcp-kind recipe — the
// "undeclared recipes ignore origin" fixture (github carries an origin
// parameter since add-recipe-base-url tasks.md 4.1).
func registerHandlerPlainRecipe(t *testing.T, id string) {
	t.Helper()
	domain.RegisterRecipe(domain.Recipe{
		ID:           id,
		Service:      "Stub Plain " + id,
		Icon:         "stub",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindMCP,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://plain.stub.example.com/mcp",
		TokenHeader:  "Authorization",
		TokenScheme:  "Bearer",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Generate a stub token"}},
		Probe:        domain.RecipeProbe{Tool: "stub.ping"},
	})
}

type handlerConnectionRow struct {
	ID       string   `json:"id"`
	Service  string   `json:"service"`
	Origin   string   `json:"origin"`
	Access   string   `json:"access_level"`
	Status   string   `json:"status"`
	TokenHit string   `json:"token_hint"`
	Attached []string `json:"attached_agents"`
}

func postConnect(t *testing.T, r *gin.Engine, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/connections", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func decodeHandlerConnection(t *testing.T, w *httptest.ResponseRecorder) handlerConnectionRow {
	t.Helper()
	var res struct {
		Connection handlerConnectionRow `json:"connection"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode connection response: %v (%s)", err, w.Body.String())
	}
	return res.Connection
}

// A connect carrying an origin stores and returns the resolved origin; an
// empty origin resolves the recipe's declared default.
func TestConnectionsHandler_ConnectCarriesOrigin(t *testing.T) {
	registerHandlerOriginRecipe(t, "stuborigin-handler-1")
	r, st, ws := newConnectionsHandlerEnv(t)

	w := postConnect(t, r, map[string]any{
		"recipe_id": "stuborigin-handler-1",
		"token":     "tok-handler-1",
		"origin":    "https://stub.self.example.com:8443/",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}
	conn := decodeHandlerConnection(t, w)
	if want := "https://stub.self.example.com:8443"; conn.Origin != want {
		t.Errorf("expected the normalized origin %q in the response view, got %q", want, conn.Origin)
	}

	// The stored row and the read path carry the same origin.
	stored, err := st.Connections().Get(context.Background(), ws.ID, conn.ID)
	if err != nil {
		t.Fatalf("load stored connection: %v", err)
	}
	if stored.Origin != conn.Origin {
		t.Errorf("expected the stored origin %q to match the response, got %q", conn.Origin, stored.Origin)
	}
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/integrations/connections/"+conn.ID, nil))
	if wGet.Code != http.StatusOK {
		t.Fatalf("get expected 200, got %d: %s", wGet.Code, wGet.Body.String())
	}
	if got := decodeHandlerConnection(t, wGet); got.Origin != conn.Origin {
		t.Errorf("expected the get view to carry the origin, got %q", got.Origin)
	}

	// An omitted origin presets the declared default.
	wDefault := postConnect(t, r, map[string]any{
		"recipe_id": "stuborigin-handler-1",
		"token":     "tok-handler-2",
	})
	if wDefault.Code != http.StatusCreated {
		t.Fatalf("default-origin connect expected 201, got %d: %s", wDefault.Code, wDefault.Body.String())
	}
	if conn := decodeHandlerConnection(t, wDefault); conn.Origin != "https://stub.example.com" {
		t.Errorf("expected the declared default in the response view, got %q", conn.Origin)
	}
}

// Invalid origins on parametrized recipes are 400 invalid_request envelopes;
// the validation error names the base-URL field.
func TestConnectionsHandler_OriginValidationEnvelopes(t *testing.T) {
	registerHandlerOriginRecipe(t, "stuborigin-handler-2")
	r, st, ws := newConnectionsHandlerEnv(t)

	cases := []struct {
		name   string
		origin string
	}{
		{"path-bearing origin", "https://stub.example.com/api/v4"},
		{"schemeless origin", "stub.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postConnect(t, r, map[string]any{
				"recipe_id": "stuborigin-handler-2",
				"token":     "tok-validation",
				"origin":    tc.origin,
			})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			var envErr handlers.ErrorEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &envErr); err != nil {
				t.Fatalf("decode error envelope: %v (%s)", err, w.Body.String())
			}
			if envErr.Error.Code != handlers.CodeInvalidRequest {
				t.Errorf("expected %q code, got %q", handlers.CodeInvalidRequest, envErr.Error.Code)
			}
			if !strings.Contains(envErr.Error.Message, "Instance base URL") {
				t.Errorf("expected the error to name the base-URL field, got %q", envErr.Error.Message)
			}
		})
	}

	// Nothing was stored on any rejected request.
	rows, err := st.Connections().List(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no stored connections after rejections, got %v", rows)
	}
}

// A submitted origin for a recipe that declares no parameter is IGNORED: the
// connect succeeds against the fixed endpoint and the stored origin is empty
// (spec: "Undeclared recipes ignore origin"). github declares an origin
// parameter since add-recipe-base-url tasks.md 4.1, so the plain fixture is
// a test-registered param-less mcp recipe.
func TestConnectionsHandler_OriginIgnoredForUndeclaredRecipe(t *testing.T) {
	r, st, ws := newConnectionsHandlerEnv(t)
	registerHandlerPlainRecipe(t, "stuborigin-handler-plain")

	w := postConnect(t, r, map[string]any{
		"recipe_id": "stuborigin-handler-plain",
		"token":     "tok-undeclared",
		"origin":    "https://elsewhere.example.com",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}
	conn := decodeHandlerConnection(t, w)
	if conn.Origin != "" {
		t.Errorf("expected the undeclared origin ignored (no origin in the view), got %q", conn.Origin)
	}
	if conn.Service != "stuborigin-handler-plain" {
		t.Errorf("expected the plain recipe connection, got %q", conn.Service)
	}
	stored, err := st.Connections().Get(context.Background(), ws.ID, conn.ID)
	if err != nil {
		t.Fatalf("load stored connection: %v", err)
	}
	if stored.Origin != "" {
		t.Errorf("expected the stored origin empty, got %q", stored.Origin)
	}
}

// The same origin twice is a 409 conflict envelope naming the origin.
func TestConnectionsHandler_SameOriginConflict(t *testing.T) {
	registerHandlerOriginRecipe(t, "stuborigin-handler-3")
	r, _, _ := newConnectionsHandlerEnv(t)

	first := postConnect(t, r, map[string]any{
		"recipe_id": "stuborigin-handler-3",
		"token":     "tok-first",
		"origin":    "https://conflict.stub.example.com",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first connect expected 201, got %d: %s", first.Code, first.Body.String())
	}

	second := postConnect(t, r, map[string]any{
		"recipe_id": "stuborigin-handler-3",
		"token":     "tok-second",
		"origin":    "https://conflict.stub.example.com",
	})
	if second.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", second.Code, second.Body.String())
	}
	var envErr handlers.ErrorEnvelope
	if err := json.Unmarshal(second.Body.Bytes(), &envErr); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, second.Body.String())
	}
	if envErr.Error.Code != handlers.CodeConflict {
		t.Errorf("expected conflict code, got %q", envErr.Error.Code)
	}
	if !strings.Contains(envErr.Error.Message, "https://conflict.stub.example.com") {
		t.Errorf("expected the conflict to name the origin, got %q", envErr.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// Connection edit (add-connection-edit tasks 3.1–3.3): the atomic attachment
// save (PUT agents) and the in-place token replacement (POST token) over the
// same fake-store service assembly, the attachment diff riding the fake's
// WithTx seam exactly as the router wires it.
// ---------------------------------------------------------------------------

// registerHandlerOAuthRecipe registers one mcp-kind OAuth recipe — the
// token-replacement refusal fixture.
func registerHandlerOAuthRecipe(t *testing.T, id string) {
	t.Helper()
	domain.RegisterRecipe(domain.Recipe{
		ID:           id,
		Service:      "Stub OAuth " + id,
		Icon:         "stub",
		AuthKind:     domain.RecipeAuthOAuth,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindMCP,
		Transport:    domain.MCPTransportStreamableHTTP,
		Endpoint:     "https://oauth.stub.example.com/mcp",
		TokenHeader:  "Authorization",
		AuthorizeURL: "https://oauth.stub.example.com/authorize",
		TokenURL:     "https://oauth.stub.example.com/token",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Authorize"}},
		Probe:        domain.RecipeProbe{Tool: "stub.ping"},
	})
}

// seedHandlerAgent creates one workspace agent with the inherit (empty)
// provider pair — the edit tests only need rows whose enabled_mcps move.
func seedHandlerAgent(t *testing.T, st store.Store, ws *domain.Workspace, name, slug string) *domain.Agent {
	t.Helper()
	agent := &domain.Agent{WorkspaceID: ws.ID, Name: name, Slug: slug}
	if err := st.Agents().Create(context.Background(), agent); err != nil {
		t.Fatalf("seed agent %s: %v", name, err)
	}
	return agent
}

// putConnectionAgents runs one attachment save.
func putConnectionAgents(t *testing.T, r *gin.Engine, connectionID, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/integrations/connections/"+connectionID+"/agents", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// postConnectionToken runs one token replacement.
func postConnectionToken(t *testing.T, r *gin.Engine, connectionID, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/connections/"+connectionID+"/token", strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// connectPlain connects the named recipe with the given token and returns the
// created connection's view row.
func connectPlain(t *testing.T, r *gin.Engine, recipeID, token string) handlerConnectionRow {
	t.Helper()
	w := postConnect(t, r, map[string]any{"recipe_id": recipeID, "token": token})
	if w.Code != http.StatusCreated {
		t.Fatalf("connect expected 201, got %d: %s", w.Code, w.Body.String())
	}
	return decodeHandlerConnection(t, w)
}

// seedConnectionRow inserts a connection directly — the flows whose gate sits
// after the connection read (probe failure, OAuth refusal) skip connect.
func seedConnectionRow(t *testing.T, st store.Store, ws *domain.Workspace, service string) *domain.Connection {
	t.Helper()
	conn := &domain.Connection{WorkspaceID: ws.ID, Service: service, AccessLevel: domain.ConnectionAccessReadOnly}
	if err := st.Connections().Create(context.Background(), conn); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	return conn
}

// One save sets the complete agent set and the refreshed view lists the
// attached names; an empty array detaches everyone.
func TestConnectionsHandler_SetAgentsRoundTrip(t *testing.T) {
	registerHandlerPlainRecipe(t, "stubedit-agents-1")
	r, st, ws := newConnectionsHandlerEnv(t)
	conn := connectPlain(t, r, "stubedit-agents-1", "tok-agents-1")

	atlas := seedHandlerAgent(t, st, ws, "Atlas", "atlas")
	beacon := seedHandlerAgent(t, st, ws, "Beacon", "beacon")

	w := putConnectionAgents(t, r, conn.ID, fmt.Sprintf(`{"agent_ids":[%q,%q]}`, atlas.ID, beacon.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("set agents expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeHandlerConnection(t, w)
	want := []string{"Atlas", "Beacon"}
	slices.Sort(got.Attached)
	if !slices.Equal(got.Attached, want) {
		t.Errorf("expected attached agents %v, got %v", want, got.Attached)
	}

	// The empty array is the detach-all save.
	wClear := putConnectionAgents(t, r, conn.ID, `{"agent_ids":[]}`)
	if wClear.Code != http.StatusOK {
		t.Fatalf("detach-all expected 200, got %d: %s", wClear.Code, wClear.Body.String())
	}
	if cleared := decodeHandlerConnection(t, wClear); len(cleared.Attached) != 0 {
		t.Errorf("expected no attached agents after detach-all, got %v", cleared.Attached)
	}
}

// An unknown agent id is the standard validation envelope and nothing
// changes — no attachment moved anywhere.
func TestConnectionsHandler_SetAgentsUnknownAgentRejected(t *testing.T) {
	registerHandlerPlainRecipe(t, "stubedit-agents-2")
	r, st, ws := newConnectionsHandlerEnv(t)
	conn := connectPlain(t, r, "stubedit-agents-2", "tok-agents-2")
	seedHandlerAgent(t, st, ws, "Atlas", "atlas")

	w := putConnectionAgents(t, r, conn.ID, `{"agent_ids":["agent-does-not-exist"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var envErr handlers.ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envErr); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, w.Body.String())
	}
	if envErr.Error.Code != handlers.CodeInvalidRequest {
		t.Errorf("expected %q code, got %q", handlers.CodeInvalidRequest, envErr.Error.Code)
	}
	if !strings.Contains(envErr.Error.Message, "agent-does-not-exist") {
		t.Errorf("expected the error to name the unknown agent, got %q", envErr.Error.Message)
	}

	// Nothing changed: the connection attaches no one and the agent's
	// allowlist is untouched.
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/integrations/connections/"+conn.ID, nil))
	if got := decodeHandlerConnection(t, wGet); len(got.Attached) != 0 {
		t.Errorf("expected no attachments after the rejected save, got %v", got.Attached)
	}
	stored, err := st.Agents().ListForWorkspace(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	for _, agent := range stored {
		if len(agent.EnabledMCPS) != 0 {
			t.Errorf("agent %s allowlist changed on a rejected save: %v", agent.Name, agent.EnabledMCPS)
		}
	}
}

// An unknown connection id is the standard 404 not-found envelope on both
// edit endpoints.
func TestConnectionsHandler_EditUnknownConnectionNotFound(t *testing.T) {
	registerHandlerPlainRecipe(t, "stubedit-agents-3")
	r, _, _ := newConnectionsHandlerEnv(t)

	wAgents := putConnectionAgents(t, r, "no-such-connection", `{"agent_ids":[]}`)
	if wAgents.Code != http.StatusNotFound {
		t.Fatalf("agents 404 expected, got %d: %s", wAgents.Code, wAgents.Body.String())
	}
	var envAgents handlers.ErrorEnvelope
	if err := json.Unmarshal(wAgents.Body.Bytes(), &envAgents); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, wAgents.Body.String())
	}
	if envAgents.Error.Code != handlers.CodeNotFound {
		t.Errorf("agents: expected %q code, got %q", handlers.CodeNotFound, envAgents.Error.Code)
	}

	wToken := postConnectionToken(t, r, "no-such-connection", `{"token":"tok-x"}`)
	if wToken.Code != http.StatusNotFound {
		t.Fatalf("token 404 expected, got %d: %s", wToken.Code, wToken.Body.String())
	}
	var envToken handlers.ErrorEnvelope
	if err := json.Unmarshal(wToken.Body.Bytes(), &envToken); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, wToken.Body.String())
	}
	if envToken.Error.Code != handlers.CodeNotFound {
		t.Errorf("token: expected %q code, got %q", handlers.CodeNotFound, envToken.Error.Code)
	}
}

// Malformed JSON bodies ride the connect handler's binding convention: the
// standard invalid_request envelope.
func TestConnectionsHandler_EditEndpointsRejectMalformedBody(t *testing.T) {
	registerHandlerPlainRecipe(t, "stubedit-agents-4")
	r, _, _ := newConnectionsHandlerEnv(t)
	conn := connectPlain(t, r, "stubedit-agents-4", "tok-agents-4")

	wAgents := putConnectionAgents(t, r, conn.ID, `{not json`)
	if wAgents.Code != http.StatusBadRequest {
		t.Fatalf("agents malformed body expected 400, got %d: %s", wAgents.Code, wAgents.Body.String())
	}
	var envAgents handlers.ErrorEnvelope
	if err := json.Unmarshal(wAgents.Body.Bytes(), &envAgents); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, wAgents.Body.String())
	}
	if envAgents.Error.Code != handlers.CodeInvalidRequest {
		t.Errorf("agents: expected %q code, got %q", handlers.CodeInvalidRequest, envAgents.Error.Code)
	}

	wToken := postConnectionToken(t, r, conn.ID, `{not json`)
	if wToken.Code != http.StatusBadRequest {
		t.Fatalf("token malformed body expected 400, got %d: %s", wToken.Code, wToken.Body.String())
	}
}

// A valid replacement swaps the token in place — the last-4 hint updates and
// the attached agent keeps its tools — and a blank submission is rejected.
func TestConnectionsHandler_ReplaceTokenSwapsTokenInPlace(t *testing.T) {
	registerHandlerPlainRecipe(t, "stubedit-token-1")
	r, st, ws := newConnectionsHandlerEnv(t)
	// "Bearer tok-old-1234" hints as its last four characters.
	conn := connectPlain(t, r, "stubedit-token-1", "tok-old-1234")
	if conn.TokenHit != "1234" {
		t.Fatalf("expected the old hint %q, got %q", "1234", conn.TokenHit)
	}
	atlas := seedHandlerAgent(t, st, ws, "Atlas", "atlas")
	if w := putConnectionAgents(t, r, conn.ID, fmt.Sprintf(`{"agent_ids":[%q]}`, atlas.ID)); w.Code != http.StatusOK {
		t.Fatalf("attach before replace expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w := postConnectionToken(t, r, conn.ID, `{"token":"tok-new-9876"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("replace expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeHandlerConnection(t, w)
	if got.TokenHit != "9876" {
		t.Errorf("expected the refreshed hint %q, got %q", "9876", got.TokenHit)
	}
	if !slices.Equal(got.Attached, []string{"Atlas"}) {
		t.Errorf("expected attachments preserved across the replace, got %v", got.Attached)
	}

	// A blank submission is invalid — the keep-semantics live at the dialog
	// layer, which skips the call entirely (design.md D3).
	wBlank := postConnectionToken(t, r, conn.ID, `{"token":"   "}`)
	if wBlank.Code != http.StatusBadRequest {
		t.Fatalf("blank token expected 400, got %d: %s", wBlank.Code, wBlank.Body.String())
	}
}

// A failed candidate probe is the connect envelope mapping: 400
// invalid_request with the upstream message verbatim, nothing stored.
func TestConnectionsHandler_ReplaceTokenProbeFailureKeepsToken(t *testing.T) {
	registerHandlerPlainRecipe(t, "stubedit-token-2")
	r, st, ws := newConnectionsHandlerEnvWithProber(t, func(context.Context, string, string, string, domain.MCPConnection) (int, error) {
		return 0, errors.New("dial refused: unauthorized")
	})
	// Connect is probe-gated, so the connection row is seeded directly — the
	// replace reads it before any dial.
	conn := seedConnectionRow(t, st, ws, "stubedit-token-2")

	w := postConnectionToken(t, r, conn.ID, `{"token":"tok-bad"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var envErr handlers.ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envErr); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, w.Body.String())
	}
	if envErr.Error.Code != handlers.CodeInvalidRequest {
		t.Errorf("expected %q code, got %q", handlers.CodeInvalidRequest, envErr.Error.Code)
	}
	if !strings.Contains(envErr.Error.Message, "dial refused: unauthorized") {
		t.Errorf("expected the upstream probe message verbatim, got %q", envErr.Error.Message)
	}
}

// An OAuth-kind connection refuses token replacement through the service
// sentinel whose message directs to reauthorization.
func TestConnectionsHandler_ReplaceTokenOAuthRefused(t *testing.T) {
	registerHandlerOAuthRecipe(t, "stubedit-oauth-1")
	r, st, ws := newConnectionsHandlerEnv(t)
	conn := seedConnectionRow(t, st, ws, "stubedit-oauth-1")

	w := postConnectionToken(t, r, conn.ID, `{"token":"tok-whatever"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var envErr handlers.ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envErr); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, w.Body.String())
	}
	if envErr.Error.Code != handlers.CodeInvalidRequest {
		t.Errorf("expected %q code, got %q", handlers.CodeInvalidRequest, envErr.Error.Code)
	}
	if !strings.Contains(envErr.Error.Message, "reauthorizing") {
		t.Errorf("expected the refusal to direct to reauthorization, got %q", envErr.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// Permission tier (add-connection-edit D5): the guard middleware mirrors the
// router's RequirePermission against the fake store so the Member 403 path
// exercises the real permission algebra (domain.HasPermission).
// ---------------------------------------------------------------------------

// newConnectionsGuardEnv mounts the two edit routes behind a permission
// mirror, with an Owner and a plain Member enrolled in the workspace. The
// returned member is the seeded Member user; the setter swaps the request's
// current user.
func newConnectionsGuardEnv(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace, *domain.User, func(*domain.User)) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	ownerRole := &domain.Role{WorkspaceID: ws.ID, Name: "Owner", Permissions: domain.OwnerPermissions}
	if err := st.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("seed owner role: %v", err)
	}
	memberRole := &domain.Role{WorkspaceID: ws.ID, Name: "Member", Permissions: domain.MemberPermissions}
	if err := st.Roles().Create(ctx, memberRole); err != nil {
		t.Fatalf("seed member role: %v", err)
	}
	owner := &domain.User{Email: "owner@example.com", Name: "Owner"}
	member := &domain.User{Email: "member@example.com", Name: "Member"}
	for _, u := range []*domain.User{owner, member} {
		if err := st.Users().Create(ctx, u); err != nil {
			t.Fatalf("create user %s: %v", u.Email, err)
		}
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: owner.ID, RoleID: ownerRole.ID}); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	if err := st.Members().Add(ctx, &domain.Member{WorkspaceID: ws.ID, UserID: member.ID, RoleID: memberRole.ID}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	settings := agents.NewMCPSettingsService(st.WorkspaceMCPServers(), st.AgentMCPServers(), st.Agents(), []byte(connHandlerEncKey))
	svc := services.NewConnectionsService(
		st.Connections(),
		st.WorkspaceMCPServers(),
		st.Agents(),
		settings,
		st.OAuthApps(),
		[]byte(connHandlerEncKey),
		"",
		services.WithProber(handlerTestProber),
	)
	h := handlers.NewConnectionsHandlers(svc, noopInvalidator{}, nil, "")

	var currentUser *domain.User
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.UserContextKey, currentUser)
		resolved, err := st.Workspaces().BySlug(c.Request.Context(), c.Param("ws"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found"}})
			return
		}
		m, err := st.Members().Get(c.Request.Context(), resolved.ID, currentUser.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found"}})
			return
		}
		role, err := st.Roles().ByID(c.Request.Context(), m.RoleID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found"}})
			return
		}
		m.Role = role
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Set(handlers.MemberContextKey, m)
		c.Set(handlers.RoleContextKey, role)
		c.Next()
	})
	perm := func(permission string) gin.HandlerFunc {
		// Mirrors the router's RequirePermission guard (the real middleware
		// lives in internal/server, which cannot be imported here).
		return func(c *gin.Context) {
			role, ok := handlers.CurrentRole(c)
			if !ok || role == nil || !domain.HasPermission(role.Permissions, permission) {
				handlers.AbortForbidden(c, "insufficient permissions")
				return
			}
			c.Next()
		}
	}
	group := r.Group("/api/v1/workspaces/:ws")
	group.PUT("/integrations/connections/:id/agents", perm(domain.IntegrationsWrite), h.SetConnectionAgents)
	group.POST("/integrations/connections/:id/token", perm(domain.IntegrationsWrite), h.ReplaceConnectionToken)
	return r, st, ws, member, func(u *domain.User) { currentUser = u }
}

// A plain Member — no integrations.write — is 403 on both edit endpoints,
// the same trust tier as connect/disconnect (design.md D5).
func TestConnectionsHandler_MemberForbiddenOnConnectionEdit(t *testing.T) {
	r, _, _, member, as := newConnectionsGuardEnv(t)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"set agents", http.MethodPut, "/api/v1/workspaces/acme/integrations/connections/conn-1/agents", `{"agent_ids":[]}`},
		{"replace token", http.MethodPost, "/api/v1/workspaces/acme/integrations/connections/conn-1/token", `{"token":"tok-x"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			as(member)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
			}
			var envErr handlers.ErrorEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &envErr); err != nil {
				t.Fatalf("decode error envelope: %v (%s)", err, w.Body.String())
			}
			if envErr.Error.Code != handlers.CodeForbidden {
				t.Errorf("expected %q code, got %q", handlers.CodeForbidden, envErr.Error.Code)
			}
		})
	}
}
