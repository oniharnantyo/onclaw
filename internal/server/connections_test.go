package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// -----------------------------------------------------------------------------
// Workspace service connections (add-workspace-connections 5.2): permission
// matrix over integrations.write, connect/probe-gate envelopes, the
// managed-server pointer errors, and hint-only reads — fake-based HTTP tests
// with a stubbed prober injected through the router options.
// -----------------------------------------------------------------------------

const (
	connTestToken  = "ghp_http-smoke-token-9876"
	connTestHint   = "9876"
	connToolCount  = 3
	connTimeoutOpt = 2 * time.Second
)

// stubProber is the injected prober seam: tests flip err between steps.
type stubProber struct {
	mu    sync.Mutex
	err   error
	calls int
}

func (p *stubProber) probe(_ context.Context, _, _, _ string, _ domain.MCPConnection) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return 0, p.err
	}
	return connToolCount, nil
}

// connectionsEnv is a test env whose router serves a connections service with
// the stub prober; it returns the shared prober so steps can re-program it.
func connectionsEnv(t *testing.T) (*testEnv, *stubProber) {
	t.Helper()
	prober := &stubProber{}
	env := setupTestEnv(t, func(o *server.RouterOptions) {
		settings := agents.NewMCPSettingsService(o.Store.WorkspaceMCPServers(), o.Store.AgentMCPServers(), o.Store.Agents(), o.EncryptionKey)
		o.Connections = services.NewConnectionsService(
			o.Store.Connections(),
			o.Store.WorkspaceMCPServers(),
			o.Store.Agents(),
			settings,
			services.WithProber(prober.probe),
			services.WithProbeTimeout(connTimeoutOpt),
		)
	})
	return env, prober
}

// connMembers wires owner/admin/member/outsider plus a custom role holding
// tools.write but NOT integrations.write, and a superadmin-permission role.
func connMembers(t *testing.T, env *testEnv, slug string) (ownerToken, adminToken, memberToken, toolMgrToken, superToken, outsiderToken string) {
	t.Helper()
	ownerUser, ownerToken := createTestUser(t, env, slug+"-owner@example.com", "Owner", "pwd")
	adminUser, adminToken := createTestUser(t, env, slug+"-admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, slug+"-member@example.com", "Member", "pwd")
	toolMgrUser, toolMgrToken := createTestUser(t, env, slug+"-toolmgr@example.com", "Tool Mgr", "pwd")
	superUser, superToken := createTestUser(t, env, slug+"-super@example.com", "Super", "pwd")
	_, outsiderToken = createTestUser(t, env, slug+"-outsider@example.com", "Outsider", "pwd")

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, slug, "Connections WS "+slug)

	// The custom role carries tool management but NOT integrations.write —
	// design.md D10: service credentials never ride tools.write.
	toolMgrRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Tool Manager",
		Permissions: []string{domain.WorkspaceRead, domain.ToolsWrite},
	}
	if err := env.store.Roles().Create(context.Background(), toolMgrRole); err != nil {
		t.Fatalf("create tool manager role: %v", err)
	}
	// The superadmin-permission role proves the all-workspace set connects.
	superRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Superadmin",
		Permissions: domain.SuperadminPermissions,
	}
	if err := env.store.Roles().Create(context.Background(), superRole); err != nil {
		t.Fatalf("create superadmin role: %v", err)
	}

	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)
	addMember(t, env, ws.ID, toolMgrUser.ID, toolMgrRole.ID)
	addMember(t, env, ws.ID, superUser.ID, superRole.ID)
	return ownerToken, adminToken, memberToken, toolMgrToken, superToken, outsiderToken
}

type connectionRow struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspace_id"`
	Service        string   `json:"service"`
	AccessLevel    string   `json:"access_level"`
	Status         string   `json:"status"`
	StatusError    string   `json:"status_error"`
	TokenHint      string   `json:"token_hint"`
	ServerID       string   `json:"server_id"`
	ServerEnabled  bool     `json:"server_enabled"`
	ToolCount      int      `json:"tool_count"`
	AttachedAgents []string `json:"attached_agents"`
}

type connectionResponse struct {
	Connection connectionRow `json:"connection"`
}

type connectionListResponse struct {
	Connections []connectionRow `json:"connections"`
}

func decodeConnection(t *testing.T, body []byte) connectionRow {
	t.Helper()
	var res connectionResponse
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("failed to decode connection response: %v (%s)", err, body)
	}
	return res.Connection
}

// Scenario matrix: Member reads the gallery but cannot manage; the custom
// tools.write role is rejected (integrations.write is its own trust tier,
// D10); Owner/Admin/the superadmin-permission set manage; non-members 404.
func TestConnections_PermissionMatrix(t *testing.T) {
	env, _ := connectionsEnv(t)
	slug := "conn-perm-ws"
	ownerToken, adminToken, memberToken, toolMgrToken, superToken, outsiderToken := connMembers(t, env, slug)

	base := "/api/v1/workspaces/" + slug + "/integrations"

	t.Run("non-member gets 404 across all endpoints", func(t *testing.T) {
		for _, ep := range []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, base + "/recipes", nil},
			{http.MethodGet, base + "/connections", nil},
			{http.MethodPost, base + "/connections", map[string]any{"recipe_id": "github", "token": "x"}},
			{http.MethodDelete, base + "/connections/some-id", nil},
			{http.MethodPost, base + "/connections/some-id/probe", nil},
		} {
			w := doRequest(env.router, ep.method, ep.path, outsiderToken, ep.body)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s expected 404 for non-member, got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
			}
		}
	})

	t.Run("member reads the gallery but cannot manage", func(t *testing.T) {
		wRecipes := doRequest(env.router, http.MethodGet, base+"/recipes", memberToken, nil)
		if wRecipes.Code != http.StatusOK {
			t.Fatalf("expected 200 on member recipes read, got %d: %s", wRecipes.Code, wRecipes.Body.String())
		}
		wList := doRequest(env.router, http.MethodGet, base+"/connections", memberToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected 200 on member connections read, got %d: %s", wList.Code, wList.Body.String())
		}
		var list connectionListResponse
		if err := json.Unmarshal(wList.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(list.Connections) != 0 {
			t.Errorf("expected an empty connection list, got %+v", list.Connections)
		}

		for _, wr := range []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodPost, base + "/connections", map[string]any{"recipe_id": "github", "access_level": "read_only", "token": connTestToken}},
			{http.MethodDelete, base + "/connections/some-id", nil},
			{http.MethodPost, base + "/connections/some-id/probe", nil},
		} {
			w := doRequest(env.router, wr.method, wr.path, memberToken, wr.body)
			if w.Code != http.StatusForbidden {
				t.Errorf("%s %s expected 403 for member, got %d: %s", wr.method, wr.path, w.Code, w.Body.String())
			}
			var envErr server.ErrorEnvelope
			_ = json.Unmarshal(w.Body.Bytes(), &envErr)
			if envErr.Error.Code != server.CodeForbidden {
				t.Errorf("expected forbidden code, got %q", envErr.Error.Code)
			}
		}
	})

	t.Run("custom role with tools.write but without integrations.write is rejected", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", toolMgrToken, map[string]any{
			"recipe_id": "github", "access_level": "read_only", "token": connTestToken,
		})
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for the tools.write custom role, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("owner, admin, and the superadmin set pass the guard", func(t *testing.T) {
		// One connection per service: the owner creates it; admin and the
		// superadmin-permission holder reach the service-layer conflict
		// (409) — either status proves integrations.write held. A 403 here
		// would mean the guard rejected them.
		expectations := []struct {
			name   string
			token  string
			status int
		}{
			{"owner", ownerToken, http.StatusCreated},
			{"admin", adminToken, http.StatusConflict},
			{"superadmin-permissions", superToken, http.StatusConflict},
		}
		for _, exp := range expectations {
			w := doRequest(env.router, http.MethodPost, base+"/connections", exp.token, map[string]any{
				"recipe_id": "github", "access_level": "read_only", "token": "tok-for-" + exp.name,
			})
			if w.Code != exp.status {
				t.Errorf("%s connect expected %d, got %d: %s", exp.name, exp.status, w.Code, w.Body.String())
			}
		}
		// Exactly one github connection exists.
		wList := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		var list connectionListResponse
		_ = json.Unmarshal(wList.Body.Bytes(), &list)
		if len(list.Connections) != 1 {
			t.Fatalf("expected exactly one github connection, got %d", len(list.Connections))
		}
	})

	t.Run("admin and the superadmin set manage a second service end to end", func(t *testing.T) {
		// Admin holds integrations.write: a full connect on a second service
		// (gitlab) succeeds, defaulting the access level to read-only.
		w := doRequest(env.router, http.MethodPost, base+"/connections", adminToken, map[string]any{
			"recipe_id": "gitlab", "token": "glpat-admin-token-1",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("admin gitlab connect expected 201, got %d: %s", w.Code, w.Body.String())
		}
		gitlabConn := decodeConnection(t, w.Body.Bytes())
		if gitlabConn.AccessLevel != "read_only" {
			t.Errorf("expected the recipe's read-only default, got %q", gitlabConn.AccessLevel)
		}

		// The superadmin-permission holder runs the manage verbs (probe,
		// disconnect ride integrations.write too).
		wProbe := doRequest(env.router, http.MethodPost, base+"/connections/"+gitlabConn.ID+"/probe", superToken, nil)
		if wProbe.Code != http.StatusOK {
			t.Errorf("superadmin probe expected 200, got %d: %s", wProbe.Code, wProbe.Body.String())
		}
		wDel := doRequest(env.router, http.MethodDelete, base+"/connections/"+gitlabConn.ID, superToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Errorf("superadmin disconnect expected 204, got %d: %s", wDel.Code, wDel.Body.String())
		}
	})
}

// Happy path + envelopes: connect persists the joined view with a hint-only
// token; the probe gate stores nothing on failure; validation errors are the
// documented envelopes.
func TestConnections_ConnectEnvelopesAndLifecycle(t *testing.T) {
	env, prober := connectionsEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "conn-owner@example.com", "Owner", "pwd")
	memberUser, memberToken := createTestUser(t, env, "conn-member@example.com", "Member", "pwd")
	_ = memberUser
	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, "conn-flow-ws", "Connections Flow WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	base := "/api/v1/workspaces/conn-flow-ws/integrations"

	t.Run("validation envelopes", func(t *testing.T) {
		cases := []struct {
			name   string
			body   map[string]any
			status int
			code   string
		}{
			{"unknown recipe", map[string]any{"recipe_id": "nope", "token": "tok"}, http.StatusBadRequest, server.CodeInvalidRequest},
			{"coming soon accepts no connections", map[string]any{"recipe_id": "atlassian", "token": "tok"}, http.StatusBadRequest, server.CodeInvalidRequest},
			{"bad access level", map[string]any{"recipe_id": "github", "access_level": "root", "token": "tok"}, http.StatusBadRequest, server.CodeInvalidRequest},
			{"empty token", map[string]any{"recipe_id": "github", "token": "   "}, http.StatusBadRequest, server.CodeInvalidRequest},
		}
		for _, tc := range cases {
			w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, tc.body)
			if w.Code != tc.status {
				t.Errorf("%s: expected %d, got %d: %s", tc.name, tc.status, w.Code, w.Body.String())
			}
			var envErr server.ErrorEnvelope
			_ = json.Unmarshal(w.Body.Bytes(), &envErr)
			if envErr.Error.Code != tc.code {
				t.Errorf("%s: expected code %q, got %q", tc.name, tc.code, envErr.Error.Code)
			}
		}
		if prober.calls != 0 {
			t.Errorf("validation failures must not probe, got %d", prober.calls)
		}
	})

	var conn connectionRow
	t.Run("successful connect returns the joined view", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "github", "access_level": "read_only", "token": connTestToken,
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		conn = decodeConnection(t, w.Body.Bytes())
		if conn.ID == "" || conn.ServerID == "" {
			t.Fatalf("expected connection and materialized server ids, got %+v", conn)
		}
		if conn.Status != "connected" || conn.AccessLevel != "read_only" || conn.Service != "github" {
			t.Errorf("unexpected joined view: %+v", conn)
		}
		if conn.TokenHint != connTestHint {
			t.Errorf("expected hint %q, got %q", connTestHint, conn.TokenHint)
		}
		if len(conn.AttachedAgents) != 0 {
			t.Errorf("expected empty attached agents, got %v", conn.AttachedAgents)
		}
		if strings.Contains(w.Body.String(), connTestToken) {
			t.Error("connect response leaked the plaintext token")
		}

		// The materialized server appears in the MCP registry (D1) — the
		// attach surface agents ride.
		wMCP := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/conn-flow-ws/mcp-servers", ownerToken, nil)
		if wMCP.Code != http.StatusOK || !strings.Contains(wMCP.Body.String(), conn.ServerID) {
			t.Errorf("expected the materialized server in the MCP registry (%d): %s", wMCP.Code, wMCP.Body.String())
		}
	})

	t.Run("duplicate service is a 409 conflict naming the connection", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "github", "token": "another-token",
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
		}
		var envErr server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envErr)
		if envErr.Error.Code != server.CodeConflict {
			t.Errorf("expected conflict code, got %q", envErr.Error.Code)
		}
		if !strings.Contains(envErr.Error.Message, conn.ID) {
			t.Errorf("expected the conflict to name the existing connection, got %q", envErr.Error.Message)
		}
	})

	t.Run("probe failure stores nothing and carries the upstream message", func(t *testing.T) {
		// A second service keeps the happy path's github row intact.
		prober.err = errors.New("Bad credentials")
		w := doRequest(env.router, http.MethodPost, base+"/connections", ownerToken, map[string]any{
			"recipe_id": "gitlab", "token": "glpat-invalid",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on probe failure, got %d: %s", w.Code, w.Body.String())
		}
		var envErr server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envErr)
		if envErr.Error.Code != server.CodeInvalidRequest {
			t.Errorf("expected invalid_request code, got %q", envErr.Error.Code)
		}
		if !strings.Contains(envErr.Error.Message, "probe failed: Bad credentials") {
			t.Errorf("expected the upstream message verbatim, got %q", envErr.Error.Message)
		}

		wList := doRequest(env.router, http.MethodGet, base+"/connections", ownerToken, nil)
		var list connectionListResponse
		_ = json.Unmarshal(wList.Body.Bytes(), &list)
		for _, c := range list.Connections {
			if c.Service == "gitlab" {
				t.Error("probe failure must not store a connection row")
			}
		}
		prober.err = nil
	})

	t.Run("get, list, and probe round-trips", func(t *testing.T) {
		wGet := doRequest(env.router, http.MethodGet, base+"/connections/"+conn.ID, memberToken, nil)
		if wGet.Code != http.StatusOK {
			t.Fatalf("member get expected 200, got %d: %s", wGet.Code, wGet.Body.String())
		}
		if got := decodeConnection(t, wGet.Body.Bytes()); got.ID != conn.ID || got.TokenHint != connTestHint {
			t.Errorf("unexpected get view: %+v", got)
		}

		// Probe-on-demand persists the failure as a status, not an error.
		prober.err = errors.New("upstream down")
		wProbe := doRequest(env.router, http.MethodPost, base+"/connections/"+conn.ID+"/probe", ownerToken, nil)
		if wProbe.Code != http.StatusOK {
			t.Fatalf("probe expected 200, got %d: %s", wProbe.Code, wProbe.Body.String())
		}
		probed := decodeConnection(t, wProbe.Body.Bytes())
		if probed.Status != "error" || probed.StatusError != "upstream down" {
			t.Errorf("expected persisted error status, got %+v", probed)
		}
		prober.err = nil
		wProbe = doRequest(env.router, http.MethodPost, base+"/connections/"+conn.ID+"/probe", ownerToken, nil)
		if probed := decodeConnection(t, wProbe.Body.Bytes()); probed.Status != "connected" {
			t.Errorf("expected connected after recovery, got %+v", probed)
		}

		// Unknown ids are 404 not_found.
		w404 := doRequest(env.router, http.MethodGet, base+"/connections/no-such-id", ownerToken, nil)
		if w404.Code != http.StatusNotFound {
			t.Errorf("unknown connection expected 404, got %d", w404.Code)
		}
	})

	t.Run("attach an agent over the existing enabled_mcps path", func(t *testing.T) {
		// Provider + agent fixture, then the standard agent MCP opt-in patch
		// with the materialized server id (the connect dialog's hand-off).
		wProv := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/conn-flow-ws/providers", ownerToken, map[string]any{
			"type": "openai", "name": "OpenAI Prod", "key": "sk-test-key-openai",
		})
		var provRes struct {
			Provider struct {
				ID string `json:"id"`
			} `json:"provider"`
		}
		_ = json.Unmarshal(wProv.Body.Bytes(), &provRes)

		wAgent := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/conn-flow-ws/agents", ownerToken, map[string]any{
			"name": "Atlas", "slug": "atlas", "role": "tester", "brief": "brief",
			"provider_id": provRes.Provider.ID, "model": "gpt-4o",
		})
		if wAgent.Code != http.StatusCreated {
			t.Fatalf("agent create: %d %s", wAgent.Code, wAgent.Body.String())
		}

		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/conn-flow-ws/agents/atlas", ownerToken, map[string]any{
			"enabled_mcps": []string{conn.ServerID},
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("enabled_mcps patch: %d %s", wPatch.Code, wPatch.Body.String())
		}

		// The connection view joins the attached agent's name.
		wGet := doRequest(env.router, http.MethodGet, base+"/connections/"+conn.ID, ownerToken, nil)
		if got := decodeConnection(t, wGet.Body.Bytes()); len(got.AttachedAgents) != 1 || got.AttachedAgents[0] != "Atlas" {
			t.Errorf("expected [Atlas] attached, got %+v", got.AttachedAgents)
		}
	})

	t.Run("disconnect cascades and the server is gone", func(t *testing.T) {
		wDel := doRequest(env.router, http.MethodDelete, base+"/connections/"+conn.ID, ownerToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 on disconnect, got %d: %s", wDel.Code, wDel.Body.String())
		}
		wGone := doRequest(env.router, http.MethodGet, base+"/connections/"+conn.ID, ownerToken, nil)
		if wGone.Code != http.StatusNotFound {
			t.Errorf("disconnected connection expected 404, got %d", wGone.Code)
		}
		// The materialized server is gone from the MCP registry too, and the
		// agent's attachment reference was stripped by the cascade.
		wMCP := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/conn-flow-ws/mcp-servers", ownerToken, nil)
		if strings.Contains(wMCP.Body.String(), conn.ServerID) {
			t.Error("expected the materialized server removed by the cascade")
		}
		wAgent := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/conn-flow-ws/agents/atlas", ownerToken, nil)
		var agentRes struct {
			Agent struct {
				EnabledMCPS []string `json:"enabled_mcps"`
			} `json:"agent"`
		}
		_ = json.Unmarshal(wAgent.Body.Bytes(), &agentRes)
		for _, id := range agentRes.Agent.EnabledMCPS {
			if id == conn.ServerID {
				t.Error("expected the agent's attachment reference stripped by the cascade")
			}
		}
	})
}

// Managed-server immutability (design.md D11): the connection's materialized
// server rejects edit/delete with a pointer error naming the owning
// connection; probe stays allowed; hand-made servers behave exactly as before.
func TestConnections_ManagedServerPointerErrors(t *testing.T) {
	env, prober := connectionsEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "managed-owner@example.com", "Owner", "pwd")
	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "managed-ws", "Managed WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)

	// Connect (stub prober green) to materialize the managed server.
	prober.calls = 0
	wConnect := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/managed-ws/integrations/connections", ownerToken, map[string]any{
		"recipe_id": "github", "token": connTestToken,
	})
	if wConnect.Code != http.StatusCreated {
		t.Fatalf("connect: %d %s", wConnect.Code, wConnect.Body.String())
	}
	conn := decodeConnection(t, wConnect.Body.Bytes())
	serverPath := "/api/v1/workspaces/managed-ws/mcp-servers/" + conn.ServerID

	t.Run("edit is rejected naming the owning connection", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPatch, serverPath, ownerToken, map[string]any{"enabled": false})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 on managed-server edit, got %d: %s", w.Code, w.Body.String())
		}
		var envErr server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &envErr)
		if envErr.Error.Code != server.CodeConflict {
			t.Errorf("expected conflict code, got %q", envErr.Error.Code)
		}
		if !strings.Contains(envErr.Error.Message, "GitHub") || !strings.Contains(envErr.Error.Message, conn.ID) {
			t.Errorf("expected the pointer error to name the connection, got %q", envErr.Error.Message)
		}
		if !strings.Contains(envErr.Error.Message, "Integrations") {
			t.Errorf("expected the pointer error to name the Integrations surface, got %q", envErr.Error.Message)
		}
	})

	t.Run("delete is rejected with the same pointer", func(t *testing.T) {
		w := doRequest(env.router, http.MethodDelete, serverPath, ownerToken, nil)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 on managed-server delete, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), conn.ID) {
			t.Errorf("expected the pointer error to name the connection, got %s", w.Body.String())
		}
		// The row survives both rejected mutations.
		wList := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/managed-ws/mcp-servers", ownerToken, nil)
		if !strings.Contains(wList.Body.String(), conn.ServerID) {
			t.Error("managed server must survive rejected edit/delete")
		}
	})

	t.Run("probe stays allowed on the managed server", func(t *testing.T) {
		// The probe endpoint dials fresh; against the recipe's real endpoint
		// it fails and persists the error status — with a 200 response either
		// way (the MCP probe convention).
		w := doRequest(env.router, http.MethodPost, serverPath+"/probe", ownerToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on managed-server probe, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("hand-made servers are unaffected", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/managed-ws/mcp-servers", ownerToken, map[string]any{
			"name": "Hand Made", "transport": "stdio", "command": "/nonexistent/onclaw-test-binary",
		})
		if wCreate.Code != http.StatusCreated {
			t.Fatalf("hand-made create: %d %s", wCreate.Code, wCreate.Body.String())
		}
		var created struct {
			Server struct {
				ID string `json:"id"`
			} `json:"server"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &created)

		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/managed-ws/mcp-servers/"+created.Server.ID, ownerToken, map[string]any{"enabled": false})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("hand-made patch: %d %s", wPatch.Code, wPatch.Body.String())
		}
		wDelete := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/managed-ws/mcp-servers/"+created.Server.ID, ownerToken, nil)
		if wDelete.Code != http.StatusNoContent {
			t.Fatalf("hand-made delete: %d %s", wDelete.Code, wDelete.Body.String())
		}
	})
}
