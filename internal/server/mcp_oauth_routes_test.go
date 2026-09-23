package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
)

// routeTestTenancy bundles the seeded workspace and its built-in roles.
type routeTestTenancy struct {
	ws         *domain.Workspace
	ownerRole  *domain.Role
	memberRole *domain.Role
}

func setupRouteTestTenancy(t *testing.T, env *testEnv) routeTestTenancy {
	t.Helper()
	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, "acme", "Acme")
	return routeTestTenancy{ws: ws, ownerRole: ownerRole, memberRole: memberRole}
}

// assertErrorEnvelope pins the standard error envelope shape and code.
func assertErrorEnvelope(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d: %s, want %d", w.Code, w.Body.String(), status)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != code {
		t.Fatalf("envelope = %s, want code %q", w.Body.String(), code)
	}
}

// The MCP OAuth routes' permission contract (add-mcp-oauth-client tasks
// 6.1/6.2): the authorize/device endpoints gate EXACTLY like the server
// config routes they extend — tools.write for the workspace registry,
// agents.write for agent-private servers — so an unauthenticated caller gets
// the standard 401 envelope, a plain Member the standard 403, and a
// sufficiently-privileged caller addressing an unknown row the standard 404
// (the row lookup precedes any discovery dial — nothing leaves the process).
func TestMCPOAuthRoutesPermissionGating(t *testing.T) {
	env := setupTestEnv(t, func(o *server.RouterOptions) {
		o.PublicBaseURL = "https://onclaw.example.com"
	})
	ctx := setupRouteTestTenancy(t, env)

	owner, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "correct-horse-1")
	addMember(t, env, ctx.ws.ID, owner.ID, ctx.ownerRole.ID)
	member, memberToken := createTestUser(t, env, "member@example.com", "Member", "correct-horse-2")
	addMember(t, env, ctx.ws.ID, member.ID, ctx.memberRole.ID)

	base := fmt.Sprintf("/api/v1/workspaces/%s", ctx.ws.Slug)
	mcpRoutes := []struct {
		name      string
		path      string
		ownerCode int
		ownerBody any
	}{
		{"authorize", base + "/mcp-servers/some-id/oauth/authorize", http.StatusNotFound, nil},
		{"device-begin", base + "/mcp-servers/some-id/oauth/device", http.StatusNotFound, nil},
		// The poll endpoint validates the sealed session blob BEFORE the row
		// lookup (a blob can only be minted for an existing row), so the
		// privileged-but-unknown shape is the 400 invalid_request envelope.
		{"device-poll", base + "/mcp-servers/some-id/oauth/device/poll", http.StatusBadRequest, map[string]string{"device_session": "tampered"}},
		{"agent-authorize", base + "/agents/atlas/mcp-servers/some-id/oauth/authorize", http.StatusNotFound, nil},
		// The agent routes resolve the agent scope first (the sibling
		// agent-server handlers' convention), so an unknown agent is 404.
		{"agent-device-begin", base + "/agents/atlas/mcp-servers/some-id/oauth/device", http.StatusNotFound, nil},
		{"agent-device-poll", base + "/agents/atlas/mcp-servers/some-id/oauth/device/poll", http.StatusNotFound, nil},
	}
	for _, route := range mcpRoutes {
		t.Run("unauthenticated 401 "+route.name, func(t *testing.T) {
			w := doRequest(env.router, http.MethodPost, route.path, "", nil)
			assertErrorEnvelope(t, w, http.StatusUnauthorized, "unauthenticated")
		})
		t.Run("member 403 "+route.name, func(t *testing.T) {
			w := doRequest(env.router, http.MethodPost, route.path, memberToken, nil)
			assertErrorEnvelope(t, w, http.StatusForbidden, "forbidden")
		})
		t.Run("owner shape "+route.name, func(t *testing.T) {
			w := doRequest(env.router, http.MethodPost, route.path, ownerToken, route.ownerBody)
			if route.ownerCode == http.StatusNotFound {
				assertErrorEnvelope(t, w, http.StatusNotFound, "not_found")
				return
			}
			assertErrorEnvelope(t, w, route.ownerCode, "invalid_request")
		})
	}
}
