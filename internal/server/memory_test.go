package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestMemory_RoleMatrix covers the HTTP surface of the human memory endpoints
// (agent-memory design D8, tasks 6.1–6.3): user memory is membership-only and
// self-scoped; workspace memory reads ride membership and PUT requires the
// workspace settings-management permission (workspace.write, the same gate as
// the workspace PATCH).
func TestMemory_RoleMatrix(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	adminUser, adminToken := createTestUser(t, env, "admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, "member@example.com", "Member", "pwd")
	member2User, member2Token := createTestUser(t, env, "member2@example.com", "Member Two", "pwd")
	nonMemberUser, nonMemberToken := createTestUser(t, env, "nonmember@example.com", "Non Member", "pwd")
	_ = nonMemberUser

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, "memory-ws", "Memory WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)
	addMember(t, env, ws.ID, member2User.ID, memberRole.ID)

	const userMemPath = "/api/v1/workspaces/memory-ws/me/memory"
	const wsMemPath = "/api/v1/workspaces/memory-ws/memory"

	t.Run("member PUTs and GETs own user memory", func(t *testing.T) {
		// Absent → 200 empty with budget and null timestamp.
		w := doRequest(env.router, http.MethodGet, userMemPath, memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on absent user memory, got %d: %s", w.Code, w.Body.String())
		}
		var empty struct {
			Content   string  `json:"content"`
			MaxChars  int     `json:"max_chars"`
			UpdatedAt *string `json:"updated_at"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &empty); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if empty.Content != "" || empty.MaxChars != domain.MaxMemoryContentChars || empty.UpdatedAt != nil {
			t.Errorf("unexpected empty shape: %+v", empty)
		}

		// PUT persists.
		w = doRequest(env.router, http.MethodPut, userMemPath, memberToken, map[string]any{"content": "I only read PRs after coffee."})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on user memory PUT, got %d: %s", w.Code, w.Body.String())
		}
		var saved struct {
			Content  string `json:"content"`
			MaxChars int    `json:"max_chars"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
			t.Fatalf("decode save: %v", err)
		}
		if saved.Content != "I only read PRs after coffee." || saved.MaxChars != domain.MaxMemoryContentChars {
			t.Errorf("unexpected saved shape: %+v", saved)
		}

		// GET returns it.
		w = doRequest(env.router, http.MethodGet, userMemPath, memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on user memory GET, got %d", w.Code)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
			t.Fatalf("decode get: %v", err)
		}
		if saved.Content != "I only read PRs after coffee." {
			t.Errorf("expected persisted content, got %q", saved.Content)
		}

		// Over-cap content is 422 naming the limit.
		w = doRequest(env.router, http.MethodPut, userMemPath, memberToken, map[string]any{"content": strings.Repeat("x", domain.MaxMemoryContentChars+1)})
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 over cap, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "32000") {
			t.Errorf("expected 422 body to name the limit: %s", w.Body.String())
		}
	})

	t.Run("user memory is self-scoped to the caller", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, userMemPath, member2Token, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var res struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Content != "" {
			t.Errorf("expected member2 to see their own empty memory, got %q", res.Content)
		}
	})

	t.Run("workspace memory: member reads, cannot write; admin writes", func(t *testing.T) {
		// Member GET → 200 (empty).
		w := doRequest(env.router, http.MethodGet, wsMemPath, memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on member workspace memory GET, got %d: %s", w.Code, w.Body.String())
		}

		// Member PUT → 403.
		w = doRequest(env.router, http.MethodPut, wsMemPath, memberToken, map[string]any{"content": "member attempted write"})
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on member workspace memory PUT, got %d: %s", w.Code, w.Body.String())
		}

		// Admin PUT → 200.
		w = doRequest(env.router, http.MethodPut, wsMemPath, adminToken, map[string]any{"content": "# Team memory\nDeploy freeze on Fridays."})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on admin workspace memory PUT, got %d: %s", w.Code, w.Body.String())
		}

		// Owner PUT → 200 and round-trips for every member.
		w = doRequest(env.router, http.MethodPut, wsMemPath, ownerToken, map[string]any{"content": "# Team memory\nDeploy freeze on Fridays and Saturdays."})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on owner workspace memory PUT, got %d: %s", w.Code, w.Body.String())
		}
		for _, token := range []string{memberToken, member2Token, adminToken} {
			w := doRequest(env.router, http.MethodGet, wsMemPath, token, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 on workspace memory GET, got %d", w.Code)
			}
			var res struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &res)
			if res.Content != "# Team memory\nDeploy freeze on Fridays and Saturdays." {
				t.Errorf("expected shared content, got %q", res.Content)
			}
		}

		// Workspace memory over cap is 422 too.
		w = doRequest(env.router, http.MethodPut, wsMemPath, adminToken, map[string]any{"content": strings.Repeat("y", domain.MaxMemoryContentChars+1)})
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 over cap, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown workspace and non-member are 404", func(t *testing.T) {
		for _, ep := range []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/v1/workspaces/unknown-ws/me/memory"},
			{http.MethodPut, "/api/v1/workspaces/unknown-ws/me/memory"},
			{http.MethodGet, "/api/v1/workspaces/unknown-ws/memory"},
			{http.MethodPut, "/api/v1/workspaces/unknown-ws/memory"},
		} {
			w := doRequest(env.router, ep.method, ep.path, memberToken, map[string]any{"content": "hi"})
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s expected 404 for unknown workspace, got %d", ep.method, ep.path, w.Code)
			}
		}
		// Non-members are 404 (indistinguishable from unknown slug).
		for _, ep := range []struct {
			method string
			path   string
		}{
			{http.MethodGet, userMemPath},
			{http.MethodPut, userMemPath},
			{http.MethodGet, wsMemPath},
			{http.MethodPut, wsMemPath},
		} {
			w := doRequest(env.router, ep.method, ep.path, nonMemberToken, map[string]any{"content": "hi"})
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s expected 404 for non-member, got %d", ep.method, ep.path, w.Code)
			}
		}
	})

	t.Run("old agent-memory routes are gone", func(t *testing.T) {
		for _, ep := range []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/v1/workspaces/memory-ws/agents/atlas/memory"},
			{http.MethodDelete, "/api/v1/workspaces/memory-ws/agents/atlas/memory"},
		} {
			for _, token := range []string{ownerToken, memberToken} {
				w := doRequest(env.router, ep.method, ep.path, token, nil)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s %s expected 404 (route removed), got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
				}
			}
		}
	})
}
