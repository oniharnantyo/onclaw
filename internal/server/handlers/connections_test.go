package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		services.WithProber(handlerTestProber),
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
	ID       string `json:"id"`
	Service  string `json:"service"`
	Origin   string `json:"origin"`
	Access   string `json:"access_level"`
	Status   string `json:"status"`
	TokenHit string `json:"token_hint"`
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
