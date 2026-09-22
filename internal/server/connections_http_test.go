package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// -----------------------------------------------------------------------------
// HTTP-kind connection lifecycle over the REST API (add-connection-http 5.2):
// connect (probe-gated against a stub upstream the test-registered recipe
// points at), the kind-aware view shape (no server_id, server_enabled false,
// verb-count tool count), attach by CONNECTION id through the existing
// enabled_mcps path, probe-on-demand status, disconnect stripping the
// attachment, and the absence of any materialized server row — fake stores,
// real crypto, no prober stub needed (the http lane dials the recipe's base
// URL directly).
// -----------------------------------------------------------------------------

const (
	httpAPITestToken = "fig-smoke-token-2468"
	httpAPITestHint  = "2468"
	httpAPIRecipeID  = "stubhttp-api"
)

// stubUpstreamAPI is the connect/probe upstream: records calls, flips status.
type stubUpstreamAPI struct {
	srv      *httptest.Server
	mu       sync.Mutex
	status   int
	body     string
	requests int
}

func newStubUpstreamAPI() *stubUpstreamAPI {
	su := &stubUpstreamAPI{status: http.StatusOK, body: `{"id":"me","email":"stub@example.com"}`}
	su.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		su.mu.Lock()
		su.requests++
		status, body := su.status, su.body
		su.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return su
}

func (su *stubUpstreamAPI) setResponse(status int, body string) {
	su.mu.Lock()
	defer su.mu.Unlock()
	su.status, su.body = status, body
}

func (su *stubUpstreamAPI) captured() (requests int, status int, body string) {
	su.mu.Lock()
	defer su.mu.Unlock()
	return su.requests, su.status, su.body
}

func init() {
	// The test-registered http recipe (recipes are data): the base URL is
	// wired to the stub upstream per test via registerHTTPAPIRecipe.
}

// registerHTTPAPIRecipe registers the package's http recipe once, pointing at
// the given stub base URL (the recipe registry is global; the id is unique to
// this package's tests).
func registerHTTPAPIRecipe(t *testing.T, baseURL string) {
	t.Helper()
	domain.RegisterRecipe(domain.Recipe{
		ID:           httpAPIRecipeID,
		Service:      "Stub API",
		Icon:         "stub",
		AuthKind:     domain.RecipeAuthPAT,
		Availability: domain.RecipeAvailable,
		Kind:         domain.RecipeKindHTTP,
		BaseURL:      baseURL,
		TokenHeader:  "X-Stub-Token",
		AccessLevels: []string{domain.ConnectionAccessReadOnly},
		Steps:        []domain.RecipeStep{{Title: "Generate a stub token"}},
		Probe:        domain.RecipeProbe{Method: "GET", Path: "/v1/me"},
		Verbs: []domain.RecipeVerb{
			{
				Tool:        httpAPIRecipeID + ".get_me",
				Method:      "GET",
				Path:        "/v1/me",
				Description: "Get the authenticated profile.",
			},
			{
				Tool:   httpAPIRecipeID + ".get_projects",
				Method: "GET",
				Path:   "/v1/projects",
				Description: "List projects.",
			},
		},
	})
}

func TestConnectionsHTTP_LifecycleAgainstStubUpstream(t *testing.T) {
	upstream := newStubUpstreamAPI()
	defer upstream.srv.Close()
	registerHTTPAPIRecipe(t, upstream.srv.URL)

	env, _ := connectionsEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "httpconn-owner@example.com", "Owner", "pwd")
	memberUser, memberToken := createTestUser(t, env, "httpconn-member@example.com", "Member", "pwd")
	_ = memberUser
	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, "httpconn-ws", "HTTP Conn WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	base := "/api/v1/workspaces/httpconn-ws/integrations"

	// The member is 403 on the http connect too — integrations.write is the
	// same trust tier for every kind.
	t.Run("member connect is 403", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", memberToken, map[string]any{
			"recipe_id": httpAPIRecipeID, "token": httpAPITestToken,
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
		if requests, _, _ := upstream.captured(); requests != 0 {
			t.Errorf("the guard must reject before any probe, got %d upstream calls", requests)
		}
	})

	// A read_write request is rejected: the recipe offers read_only only.
	t.Run("access level outside the offer is 400", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": httpAPIRecipeID, "access_level": "read_write", "token": httpAPITestToken,
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	var conn map[string]any
	t.Run("connect returns the kind-aware view and stores nothing server-side", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": httpAPIRecipeID, "token": httpAPITestToken,
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Connection map[string]any `json:"connection"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode: %v (%s)", err, w.Body.String())
		}
		conn = res.Connection

		if _, present := conn["server_id"]; present {
			t.Error("http rows must carry NO server_id key")
		}
		if enabled, ok := conn["server_enabled"].(bool); !ok || enabled {
			t.Errorf("expected server_enabled false, got %v", conn["server_enabled"])
		}
		if status, _ := conn["status"].(string); status != "connected" {
			t.Errorf("expected connected status, got %v", conn["status"])
		}
		if hint, _ := conn["token_hint"].(string); hint != httpAPITestHint {
			t.Errorf("expected last-4 hint, got %v", conn["token_hint"])
		}
		if count, _ := conn["tool_count"].(float64); int(count) != 2 {
			t.Errorf("expected tool_count 2 (declared verbs), got %v", conn["tool_count"])
		}
		if strings.Contains(w.Body.String(), httpAPITestToken) {
			t.Error("connect response leaked the plaintext token")
		}

		// No server row anywhere: the MCP registry stays empty — the
		// managed-server guard has nothing to guard.
		wMCP := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/httpconn-ws/mcp-servers", ownerToken, nil)
		if wMCP.Code != http.StatusOK {
			t.Fatalf("mcp list: %d %s", wMCP.Code, wMCP.Body.String())
		}
		if strings.Contains(wMCP.Body.String(), `"id"`) && strings.Contains(wMCP.Body.String(), "Stub API") {
			t.Errorf("http connect materialized a server row: %s", wMCP.Body.String())
		}

		// The probe dials the recipe's declared call with the composed header.
		requests, status, _ := upstream.captured()
		if requests != 1 || status != http.StatusOK {
			t.Fatalf("expected one successful probe, got %d calls (last status %d)", requests, status)
		}
	})

	t.Run("duplicate service is 409", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": httpAPIRecipeID, "token": "another",
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
		}
	})

	// Attach by CONNECTION id through the existing enabled_mcps patch.
	t.Run("attach by connection id", func(t *testing.T) {
		connID, _ := conn["id"].(string)

		wProv := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/httpconn-ws/providers", ownerToken, map[string]any{
			"type": "openai", "name": "OpenAI Prod", "key": "sk-test-key-openai",
		})
		var provRes struct {
			Provider struct {
				ID string `json:"id"`
			} `json:"provider"`
		}
		_ = json.Unmarshal(wProv.Body.Bytes(), &provRes)

		wAgent := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/httpconn-ws/agents", ownerToken, map[string]any{
			"name": "Atlas", "slug": "atlas", "role": "tester", "brief": "brief",
			"provider_id": provRes.Provider.ID, "model": "gpt-4o",
		})
		if wAgent.Code != http.StatusCreated {
			t.Fatalf("agent create: %d %s", wAgent.Code, wAgent.Body.String())
		}
		var agentRes struct {
			Agent struct {
				ID string `json:"id"`
			} `json:"agent"`
		}
		_ = json.Unmarshal(wAgent.Body.Bytes(), &agentRes)
		if agentRes.Agent.ID == "" {
			t.Fatalf("agent create returned no id: %s", wAgent.Body.String())
		}

		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/httpconn-ws/agents/atlas", ownerToken, map[string]any{
			"enabled_mcps": []string{connID},
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("enabled_mcps patch: %d %s", wPatch.Code, wPatch.Body.String())
		}

		wGet := doRequest(env.router, http.MethodGet, base+"/connections/"+connID, ownerToken, nil)
		if wGet.Code != http.StatusOK {
			t.Fatalf("get: %d %s", wGet.Code, wGet.Body.String())
		}
		var got struct {
			Connection struct {
				AttachedAgents []string `json:"attached_agents"`
			} `json:"connection"`
		}
		_ = json.Unmarshal(wGet.Body.Bytes(), &got)
		if len(got.Connection.AttachedAgents) != 1 || got.Connection.AttachedAgents[0] != "Atlas" {
			t.Errorf("expected [Atlas] attached by connection id, got %v", got.Connection.AttachedAgents)
		}
	})

	t.Run("probe persists the outcome on the connection", func(t *testing.T) {
		connID, _ := conn["id"].(string)

		upstream.setResponse(http.StatusUnauthorized, `{"err":"expired token"}`)
		wFail := doRequest(env.router, http.MethodPost, base+"/connections/"+connID+"/probe", ownerToken, nil)
		if wFail.Code != http.StatusOK {
			t.Fatalf("a failed probe is a status, not an error: %d %s", wFail.Code, wFail.Body.String())
		}
		var failed struct {
			Connection struct {
				Status      string `json:"status"`
				StatusError string `json:"status_error"`
			} `json:"connection"`
		}
		_ = json.Unmarshal(wFail.Body.Bytes(), &failed)
		if failed.Connection.Status != "error" {
			t.Errorf("expected error status, got %+v", failed.Connection)
		}
		if !strings.Contains(failed.Connection.StatusError, "expired token") {
			t.Errorf("expected the upstream message, got %q", failed.Connection.StatusError)
		}
		if strings.Contains(wFail.Body.String(), httpAPITestToken) {
			t.Error("probe response leaked the token")
		}

		upstream.setResponse(http.StatusOK, `{"id":"me"}`)
		wOK := doRequest(env.router, http.MethodPost, base+"/connections/"+connID+"/probe", ownerToken, nil)
		var recovered struct {
			Connection struct {
				Status string `json:"status"`
			} `json:"connection"`
		}
		_ = json.Unmarshal(wOK.Body.Bytes(), &recovered)
		if recovered.Connection.Status != "connected" {
			t.Errorf("expected recovery to connected, got %+v", recovered.Connection)
		}
	})

	t.Run("disconnect strips the connection id from the agent", func(t *testing.T) {
		connID, _ := conn["id"].(string)

		wDel := doRequest(env.router, http.MethodDelete, base+"/connections/"+connID, ownerToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", wDel.Code, wDel.Body.String())
		}
		wGone := doRequest(env.router, http.MethodGet, base+"/connections/"+connID, ownerToken, nil)
		if wGone.Code != http.StatusNotFound {
			t.Errorf("disconnected connection expected 404, got %d", wGone.Code)
		}

		wAgent := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/httpconn-ws/agents/atlas", ownerToken, nil)
		var agentRes struct {
			Agent struct {
				EnabledMCPS []string `json:"enabled_mcps"`
			} `json:"agent"`
		}
		_ = json.Unmarshal(wAgent.Body.Bytes(), &agentRes)
		for _, id := range agentRes.Agent.EnabledMCPS {
			if id == connID {
				t.Errorf("expected the connection id stripped, got %v", agentRes.Agent.EnabledMCPS)
			}
		}

		// The MCP registry never grew a row across the whole lifecycle.
		wMCP := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/httpconn-ws/mcp-servers", ownerToken, nil)
		var list struct {
			Servers []map[string]any `json:"servers"`
		}
		_ = json.Unmarshal(wMCP.Body.Bytes(), &list)
		if len(list.Servers) != 0 {
			t.Errorf("expected an empty MCP registry across the lifecycle, got %s", wMCP.Body.String())
		}
	})
}
