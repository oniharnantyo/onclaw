package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
)

// -----------------------------------------------------------------------------
// 1. Agents Permission Matrix & 404 Tests
// -----------------------------------------------------------------------------

func TestAgents_PermissionMatrix_And_404(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	adminUser, adminToken := createTestUser(t, env, "admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, "member@example.com", "Member", "pwd")
	nonMemberUser, nonMemberToken := createTestUser(t, env, "nonmember@example.com", "Non Member", "pwd")
	_ = nonMemberUser

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, "agents-perm-ws", "Agents Perm WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	// Create a provider config for the agents
	wProv := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/agents-perm-ws/providers", ownerToken, map[string]any{
		"type": "openai",
		"name": "OpenAI Provider",
		"key":  "sk-test-1234",
	})
	if wProv.Code != http.StatusCreated {
		t.Fatalf("failed to create provider: %s", wProv.Body.String())
	}
	var provRes struct {
		Provider handlers.ProviderResponse `json:"provider"`
	}
	_ = json.Unmarshal(wProv.Body.Bytes(), &provRes)
	provID := provRes.Provider.ID

	// Create an agent with Owner
	wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/agents-perm-ws/agents", ownerToken, map[string]any{
		"name":        "Atlas Assistant",
		"slug":        "atlas",
		"role":        "devops-helper",
		"description": "Helps manage devops",
		"brief":       "Assist users with devops questions and pipeline debugging",
		"provider_id": provID,
		"model":       "gpt-4o",
	})
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for agent creation, got %d: %s", wCreate.Code, wCreate.Body.String())
	}

	t.Run("non-member gets 404 across all agent endpoints", func(t *testing.T) {
		endpoints := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents", nil},
			{http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents/atlas", nil},
			{http.MethodPost, "/api/v1/workspaces/agents-perm-ws/agents", map[string]any{"name": "X", "slug": "x"}},
			{http.MethodPatch, "/api/v1/workspaces/agents-perm-ws/agents/atlas", map[string]any{"name": "X"}},
			{http.MethodDelete, "/api/v1/workspaces/agents-perm-ws/agents/atlas", nil},
			{http.MethodPost, "/api/v1/workspaces/agents-perm-ws/agents/atlas/regenerate", nil},
			{http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents/atlas/memory", nil},
			{http.MethodDelete, "/api/v1/workspaces/agents-perm-ws/agents/atlas/memory", nil},
			{http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents/atlas/sessions/sess-1/events", nil},
		}

		for _, ep := range endpoints {
			w := doRequest(env.router, ep.method, ep.path, nonMemberToken, ep.body)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s expected 404 for non-member, got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
			}
		}
	})

	t.Run("old agent-memory routes are gone for every member", func(t *testing.T) {
		// The per-agent-per-user memory endpoints were removed with the
		// agent_user_memories model (agent-memory design D8); requests 404
		// regardless of role.
		for _, ep := range []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents/atlas/memory"},
			{http.MethodDelete, "/api/v1/workspaces/agents-perm-ws/agents/atlas/memory"},
		} {
			for _, token := range []string{ownerToken, adminToken, memberToken} {
				w := doRequest(env.router, ep.method, ep.path, token, nil)
				if w.Code != http.StatusNotFound {
					t.Errorf("%s %s expected 404 (route removed), got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
				}
			}
		}
	})

	t.Run("member has agents.read, but not agents.write", func(t *testing.T) {
		// GET roster -> 200 OK
		wList := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents", memberToken, nil)
		if wList.Code != http.StatusOK {
			t.Errorf("expected 200 OK on list agents for member, got %d", wList.Code)
		}

		// GET agent -> 200 OK
		wGet := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents/atlas", memberToken, nil)
		if wGet.Code != http.StatusOK {
			t.Errorf("expected 200 OK on get agent for member, got %d", wGet.Code)
		}

		// GET session events -> 200 OK
		wEvents := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents/atlas/sessions/sess-1/events", memberToken, nil)
		if wEvents.Code != http.StatusOK {
			t.Errorf("expected 200 OK on get session events for member, got %d: %s", wEvents.Code, wEvents.Body.String())
		}

		// POST create -> 403 Forbidden
		wPost := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/agents-perm-ws/agents", memberToken, map[string]any{
			"name":        "Member Agent",
			"slug":        "member-agent",
			"role":        "role",
			"brief":       "brief",
			"provider_id": provID,
			"model":       "gpt-4o",
		})
		if wPost.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member POST agent, got %d", wPost.Code)
		}

		// PATCH agent -> 403 Forbidden
		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/agents-perm-ws/agents/atlas", memberToken, map[string]any{
			"name": "Hacked Atlas",
		})
		if wPatch.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member PATCH agent, got %d", wPatch.Code)
		}

		// DELETE agent -> 403 Forbidden
		wDel := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/agents-perm-ws/agents/atlas", memberToken, nil)
		if wDel.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member DELETE agent, got %d", wDel.Code)
		}

		// REGENERATE agent -> 403 Forbidden
		wRegen := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/agents-perm-ws/agents/atlas/regenerate", memberToken, nil)
		if wRegen.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member REGENERATE agent, got %d", wRegen.Code)
		}
	})

	t.Run("admin and owner have full agents.write permissions", func(t *testing.T) {
		// Admin creates agent
		wCreate2 := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/agents-perm-ws/agents", adminToken, map[string]any{
			"name":        "Hermes Admin Agent",
			"slug":        "hermes",
			"role":        "router",
			"brief":       "Routes tasks",
			"provider_id": provID,
			"model":       "gpt-4o",
		})
		if wCreate2.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for admin create agent, got %d: %s", wCreate2.Code, wCreate2.Body.String())
		}

		// Owner patches agent
		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/agents-perm-ws/agents/hermes", ownerToken, map[string]any{
			"description": "Updated by owner",
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for owner patch agent, got %d", wPatch.Code)
		}

		// Admin deletes agent
		wDel := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/agents-perm-ws/agents/hermes", adminToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content for admin delete agent, got %d", wDel.Code)
		}
	})

	t.Run("cross-tenant agent lookup returns 404", func(t *testing.T) {
		wsB, ownerRoleB, _, _ := createTestWorkspaceWithRoles(t, env, "tenant-b-agents", "Tenant B")
		userB, tokenB := createTestUser(t, env, "user-b-agents@example.com", "User B", "pwd")
		addMember(t, env, wsB.ID, userB.ID, ownerRoleB.ID)

		// Requesting atlas from workspace B returns 404
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/tenant-b-agents/agents/atlas", tokenB, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for cross-tenant agent lookup, got %d", w.Code)
		}
	})
}

// -----------------------------------------------------------------------------
// 2. Agents CRUD, Addressing & Capability Validation Tests
// -----------------------------------------------------------------------------

func TestAgents_CRUD_And_Validation(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "crud-agents-ws", "CRUD Agents WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)

	// Create OpenAI provider
	wProvOpenAI := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/providers", ownerToken, map[string]any{
		"type": "openai",
		"name": "OpenAI Prod",
		"key":  "sk-test-key-openai",
	})
	var provOpenAIRes struct {
		Provider handlers.ProviderResponse `json:"provider"`
	}
	_ = json.Unmarshal(wProvOpenAI.Body.Bytes(), &provOpenAIRes)
	openAIProvID := provOpenAIRes.Provider.ID

	// Create Anthropic provider
	wProvAnthropic := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/providers", ownerToken, map[string]any{
		"type": "anthropic",
		"name": "Anthropic Prod",
		"key":  "sk-ant-test-key",
	})
	var provAnthropicRes struct {
		Provider handlers.ProviderResponse `json:"provider"`
	}
	_ = json.Unmarshal(wProvAnthropic.Body.Bytes(), &provAnthropicRes)
	anthropicProvID := provAnthropicRes.Provider.ID

	t.Run("create agent with wizard defaults", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Sherlock",
			"slug":        "sherlock",
			"role":        "investigator",
			"description": "Finds bugs",
			"brief":       "Investigate stack traces and find root causes",
			"provider_id": openAIProvID,
			"model":       "gpt-4o",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Agent domain.Agent `json:"agent"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode agent: %v", err)
		}

		agent := res.Agent
		if agent.Slug != "sherlock" || agent.Name != "Sherlock" {
			t.Errorf("unexpected agent: %+v", agent)
		}
		if agent.Autonomy != domain.AutonomyApproval {
			t.Errorf("expected default autonomy %q, got %q", domain.AutonomyApproval, agent.Autonomy)
		}
		if agent.Temperature != 1.0 {
			t.Errorf("expected default temperature 1.0, got %f", agent.Temperature)
		}
		expectedDir := domain.AgentWorkspaceDir(env.workspaceDir, "crud-agents-ws", "sherlock")
		if _, err := os.Stat(expectedDir); err != nil {
			t.Errorf("expected agent workspace directory on disk: %v", err)
		}
		// Creation seeds no base prompt (markdown-card-elements D8 — the L1
		// prompt is injected per build, never materialized); generation writes
		// the three documents before the ready transition.
		for _, name := range []string{"IDENTITY.md", "SOUL.md", "BOOTSTRAP.md"} {
			if _, err := os.Stat(filepath.Join(expectedDir, name)); err != nil {
				t.Errorf("expected %s in agent workspace dir: %v", name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(expectedDir, "AGENTS.md")); !os.IsNotExist(err) {
			t.Errorf("agent workspace must not carry a seeded AGENTS.md, stat err: %v", err)
		}
		// Generation runs synchronously with a stubbed model factory, so the
		// create response already carries the final prompt state.
		if agent.PromptsStatus != domain.PromptsStatusReady {
			t.Errorf("expected prompts_status %q, got %q", domain.PromptsStatusReady, agent.PromptsStatus)
		}
		if len(agent.Tools) != 0 || len(agent.EnabledMCPS) != 0 {
			t.Errorf("expected empty capability arrays, got tools=%v mcps=%v", agent.Tools, agent.EnabledMCPS)
		}
	})

	t.Run("lookup by slug first and ID fallback", func(t *testing.T) {
		// By slug
		wSlug := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/crud-agents-ws/agents/sherlock", ownerToken, nil)
		if wSlug.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on slug lookup, got %d", wSlug.Code)
		}
		var res struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wSlug.Body.Bytes(), &res)
		agentID := res.Agent.ID
		// Read-side responses compose the prompt documents from the files.
		if res.Agent.Identity != "# Identity\nStub identity" || res.Agent.Soul != "# Soul\nStub soul" || res.Agent.Bootstrap != promptdocs.BootstrapTemplate {
			t.Errorf("expected identity/soul/bootstrap composed from workspace files, got identity=%q soul=%q bootstrap=%q", res.Agent.Identity, res.Agent.Soul, res.Agent.Bootstrap)
		}

		// By ID
		wID := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/crud-agents-ws/agents/%s", agentID), ownerToken, nil)
		if wID.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on ID lookup fallback, got %d", wID.Code)
		}
	})

	t.Run("slug conflict returns 409", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Sherlock Copy",
			"slug":        "sherlock",
			"role":        "investigator",
			"brief":       "Duplicate brief",
			"provider_id": openAIProvID,
			"model":       "gpt-4o",
		})
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict on duplicate slug, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid slug returns 400", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Bad Slug Agent",
			"slug":        "INVALID_SLUG!!",
			"role":        "role",
			"brief":       "brief",
			"provider_id": openAIProvID,
			"model":       "gpt-4o",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid slug, got %d", w.Code)
		}
	})

	t.Run("managed fields are ignored on input", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":           "Managed Fields Test",
			"slug":           "managed-test",
			"role":           "tester",
			"brief":          "testing managed fields",
			"provider_id":    openAIProvID,
			"model":          "gpt-4o",
			"prompts_status": "ready",
			"prompts_error":  "fake error",
			"memory":         "fake memory",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		// Managed inputs are ignored; the response carries the stub's final state.
		if res.Agent.PromptsStatus != domain.PromptsStatusReady {
			t.Errorf("prompts_status should reflect generation, got %q", res.Agent.PromptsStatus)
		}
		if res.Agent.PromptsError != nil {
			t.Errorf("prompts_error should be nil, got %v", res.Agent.PromptsError)
		}
	})

	t.Run("slug is immutable on update", func(t *testing.T) {
		// A slug in an update payload is ignored (managed-field semantics):
		// the slug is the on-disk identity under derived paths.
		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/crud-agents-ws/agents/managed-test", ownerToken, map[string]any{
			"slug":        "renamed-slug",
			"description": "carries an editable field alongside",
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", wPatch.Code, wPatch.Body.String())
		}
		var res struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wPatch.Body.Bytes(), &res)
		if res.Agent.Slug != "managed-test" {
			t.Errorf("slug must stay immutable, got %q", res.Agent.Slug)
		}
		// The directory is slug-derived; it must still exist under the old slug.
		if _, err := os.Stat(domain.AgentWorkspaceDir(env.workspaceDir, "crud-agents-ws", "managed-test")); err != nil {
			t.Errorf("expected slug-derived directory to persist: %v", err)
		}
	})

	t.Run("prompts identity and soul editable via PATCH after ready", func(t *testing.T) {
		agent, _ := env.store.Agents().BySlug(context.Background(), ws.ID, "managed-test")
		agentDir := domain.AgentWorkspaceDir(env.workspaceDir, "crud-agents-ws", "managed-test")
		_ = env.store.Agents().SetPromptState(context.Background(), ws.ID, agent.ID, domain.PromptsStatusReady, nil)

		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/crud-agents-ws/agents/managed-test", ownerToken, map[string]any{
			"identity": "# IDENTITY.md Custom",
			"soul":     "# SOUL.md Custom",
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on editing prompts, got %d: %s", wPatch.Code, wPatch.Body.String())
		}
		var res struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wPatch.Body.Bytes(), &res)
		if res.Agent.Identity != "# IDENTITY.md Custom" || res.Agent.Soul != "# SOUL.md Custom" {
			t.Errorf("expected identity/soul updated, got %+v", res.Agent)
		}
		// PATCH rewrites the workspace files for whichever field it carries.
		for name, want := range map[string]string{
			"IDENTITY.md": "# IDENTITY.md Custom",
			"SOUL.md":     "# SOUL.md Custom",
		} {
			got, err := os.ReadFile(filepath.Join(agentDir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if string(got) != want {
				t.Errorf("%s = %q, want %q", name, string(got), want)
			}
		}
	})

	t.Run("bootstrap is not client-editable", func(t *testing.T) {
		agentDir := domain.AgentWorkspaceDir(env.workspaceDir, "crud-agents-ws", "managed-test")
		existing, err := os.ReadFile(filepath.Join(agentDir, "BOOTSTRAP.md"))
		if err != nil {
			t.Fatalf("read BOOTSTRAP.md before PATCH: %v", err)
		}

		// The generator owns BOOTSTRAP.md; a client-sent bootstrap field must
		// be ignored (only generator runs write the file).
		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/crud-agents-ws/agents/managed-test", ownerToken, map[string]any{
			"bootstrap":   "client-injected bootstrap",
			"description": "payload also carries an editable field",
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", wPatch.Code, wPatch.Body.String())
		}

		var res struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wPatch.Body.Bytes(), &res)
		if res.Agent.Bootstrap == "client-injected bootstrap" {
			t.Errorf("bootstrap must not be client-editable, got %q", res.Agent.Bootstrap)
		}
		got, err := os.ReadFile(filepath.Join(agentDir, "BOOTSTRAP.md"))
		if err != nil {
			t.Fatalf("read BOOTSTRAP.md after PATCH: %v", err)
		}
		if string(got) != string(existing) {
			t.Errorf("BOOTSTRAP.md changed on PATCH: %q -> %q", string(existing), string(got))
		}
	})

	t.Run("delete removes the workspace directory", func(t *testing.T) {
		agentDir := domain.AgentWorkspaceDir(env.workspaceDir, "crud-agents-ws", "managed-test")
		if _, err := os.Stat(agentDir); err != nil {
			t.Fatalf("expected workspace dir on disk before delete: %v", err)
		}

		wDel := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/crud-agents-ws/agents/managed-test", ownerToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on delete, got %d: %s", wDel.Code, wDel.Body.String())
		}
		if _, err := os.Stat(agentDir); !os.IsNotExist(err) {
			t.Errorf("expected workspace dir removed after delete, stat err = %v", err)
		}
	})

	t.Run("anthropic provider requires max_tokens", func(t *testing.T) {
		// Create without max_tokens -> 400
		wMissing := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Claude Agent",
			"slug":        "claude-agent",
			"role":        "writer",
			"brief":       "Write documents",
			"provider_id": anthropicProvID,
			"model":       "claude-3-5-sonnet",
		})
		if wMissing.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when anthropic agent lacks max_tokens, got %d: %s", wMissing.Code, wMissing.Body.String())
		}

		// Create with max_tokens <= 0 -> 400
		wInvalidTokens := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Claude Agent 2",
			"slug":        "claude-agent-2",
			"role":        "writer",
			"brief":       "Write documents",
			"provider_id": anthropicProvID,
			"model":       "claude-3-5-sonnet",
			"max_tokens":  -10,
		})
		if wInvalidTokens.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when max_tokens is negative, got %d", wInvalidTokens.Code)
		}

		// Create with valid max_tokens -> 201
		wValid := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Claude Agent Valid",
			"slug":        "claude-agent-valid",
			"role":        "writer",
			"brief":       "Write documents",
			"provider_id": anthropicProvID,
			"model":       "claude-3-5-sonnet",
			"max_tokens":  4096,
		})
		if wValid.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created with valid max_tokens, got %d: %s", wValid.Code, wValid.Body.String())
		}
	})

	t.Run("avatar validation rejects non-JSON or objects exceeding 2KB", func(t *testing.T) {
		// Non-object JSON
		wBadJSON := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Avatar Test 1",
			"slug":        "avatar-test-1",
			"role":        "tester",
			"brief":       "brief",
			"provider_id": openAIProvID,
			"model":       "gpt-4o",
			"avatar":      "plain-string-not-object",
		})
		if wBadJSON.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on string avatar, got %d", wBadJSON.Code)
		}

		// Object exceeding 2KB
		largeMap := make(map[string]string)
		for i := 0; i < 200; i++ {
			largeMap[fmt.Sprintf("key_%d", i)] = "some long property string that fills up space quickly"
		}
		wLarge := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Avatar Large Test",
			"slug":        "avatar-large-test",
			"role":        "tester",
			"brief":       "brief",
			"provider_id": openAIProvID,
			"model":       "gpt-4o",
			"avatar":      largeMap,
		})
		if wLarge.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on large avatar > 2KB, got %d", wLarge.Code)
		}
	})

	t.Run("disabled capabilities are not referentially validated", func(t *testing.T) {
		// disabled_skills is ignored by the new server (tier activation
		// replaced the denylist); the create still succeeds.
		wUnknownSkill := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":            "Denylist Test",
			"slug":            "denylist-test",
			"role":            "tester",
			"brief":           "brief",
			"provider_id":     openAIProvID,
			"model":           "gpt-4o",
			"disabled_skills": []string{"code-review", "non-existent-skill"},
			"tools":           []string{"web.search", "non-existent-tool"},
			"disabled_mcps":   []string{"non-existent-mcp"},
		})
		if wUnknownSkill.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created with ignored disabled_skills, got %d: %s", wUnknownSkill.Code, wUnknownSkill.Body.String())
		}
		var res struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wUnknownSkill.Body.Bytes(), &res)
		if len(res.Agent.Tools) != 2 || len(res.Agent.EnabledMCPS) != 0 {
			t.Errorf("expected tools saved as provided and the legacy disabled_mcps denylist ignored: %+v", res.Agent)
		}
		if strings.Contains(wUnknownSkill.Body.String(), "disabled_skills") || strings.Contains(wUnknownSkill.Body.String(), "disabled_mcps") {
			t.Error("disabled_skills/disabled_mcps must not appear in agent responses")
		}
	})

	t.Run("enabled_mcps allowlist patch replaces the opt-in set", func(t *testing.T) {
		// Create carries enabled_mcps; the legacy disabled_mcps key in the
		// same payload is ignored like a managed field.
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":          "MCP Opt-in Test",
			"slug":          "mcp-opt-in-test",
			"role":          "tester",
			"brief":         "brief",
			"provider_id":   openAIProvID,
			"model":         "gpt-4o",
			"enabled_mcps":  []string{"srv-a", "srv-b"},
			"disabled_mcps": []string{"srv-z"},
		})
		if wCreate.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", wCreate.Code, wCreate.Body.String())
		}
		var created struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &created)
		if len(created.Agent.EnabledMCPS) != 2 || created.Agent.EnabledMCPS[0] != "srv-a" || created.Agent.EnabledMCPS[1] != "srv-b" {
			t.Errorf("expected enabled_mcps saved as provided, got %+v", created.Agent.EnabledMCPS)
		}

		// Patch replaces the allowlist, including with the empty array.
		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/crud-agents-ws/agents/mcp-opt-in-test", ownerToken, map[string]any{
			"enabled_mcps": []string{"srv-c"},
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on enabled_mcps patch, got %d: %s", wPatch.Code, wPatch.Body.String())
		}
		var patched struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wPatch.Body.Bytes(), &patched)
		if len(patched.Agent.EnabledMCPS) != 1 || patched.Agent.EnabledMCPS[0] != "srv-c" {
			t.Errorf("expected enabled_mcps replaced by the patch, got %+v", patched.Agent.EnabledMCPS)
		}

		wClear := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/crud-agents-ws/agents/mcp-opt-in-test", ownerToken, map[string]any{
			"enabled_mcps": []string{},
		})
		if wClear.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on empty enabled_mcps patch, got %d: %s", wClear.Code, wClear.Body.String())
		}
		var cleared struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wClear.Body.Bytes(), &cleared)
		if len(cleared.Agent.EnabledMCPS) != 0 {
			t.Errorf("expected empty enabled_mcps patch to clear the allowlist, got %+v", cleared.Agent.EnabledMCPS)
		}
	})

	t.Run("context_window validation and auto-fill", func(t *testing.T) {
		// Zero or negative context_window -> 400
		wZeroCW := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":           "Zero CW Test",
			"slug":           "zero-cw-test",
			"role":           "tester",
			"brief":          "brief",
			"provider_id":    openAIProvID,
			"model":          "gpt-4o",
			"context_window": 0,
		})
		if wZeroCW.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on context_window 0, got %d: %s", wZeroCW.Code, wZeroCW.Body.String())
		}

		wNegCW := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":           "Neg CW Test",
			"slug":           "neg-cw-test",
			"role":           "tester",
			"brief":          "brief",
			"provider_id":    openAIProvID,
			"model":          "gpt-4o",
			"context_window": -500,
		})
		if wNegCW.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on negative context_window, got %d: %s", wNegCW.Code, wNegCW.Body.String())
		}

		// Explicit override wins
		wOverride := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":           "Override CW Test",
			"slug":           "override-cw-test",
			"role":           "tester",
			"brief":          "brief",
			"provider_id":    openAIProvID,
			"model":          "gpt-4o",
			"context_window": 50000,
		})
		if wOverride.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on explicit context_window, got %d: %s", wOverride.Code, wOverride.Body.String())
		}
		var resOverride struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wOverride.Body.Bytes(), &resOverride)
		if resOverride.Agent.ContextWindow == nil || *resOverride.Agent.ContextWindow != 50000 {
			t.Errorf("expected context_window 50000, got %v", resOverride.Agent.ContextWindow)
		}
	})

	t.Run("cross-workspace provider reference is rejected with 400", func(t *testing.T) {
		wsOther, _, _, _ := createTestWorkspaceWithRoles(t, env, "other-ws-providers", "Other WS")
		_ = wsOther
		wProvOther := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/other-ws-providers/providers", ownerToken, map[string]any{
			"type": "openai",
			"name": "Other WS OpenAI",
		})
		var provOtherRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wProvOther.Body.Bytes(), &provOtherRes)

		wCross := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Cross Provider Agent",
			"slug":        "cross-prov-agent",
			"role":        "tester",
			"brief":       "brief",
			"provider_id": provOtherRes.Provider.ID,
			"model":       "gpt-4o",
		})
		if wCross.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on cross-workspace provider, got %d: %s", wCross.Code, wCross.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// 3. Regeneration Tests
// -----------------------------------------------------------------------------

func TestAgents_Regenerate(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	memberUser, _ := createTestUser(t, env, "member@example.com", "Member", "pwd")

	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, "regen-mem-ws", "Regen Mem WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	wProv := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/regen-mem-ws/providers", ownerToken, map[string]any{
		"type": "openai",
		"name": "OpenAI",
		"key":  "sk-key-1234",
	})
	var provRes struct {
		Provider handlers.ProviderResponse `json:"provider"`
	}
	_ = json.Unmarshal(wProv.Body.Bytes(), &provRes)

	wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/regen-mem-ws/agents", ownerToken, map[string]any{
		"name":        "Regen Agent",
		"slug":        "regen-agent",
		"role":        "tester",
		"brief":       "Generate tests",
		"provider_id": provRes.Provider.ID,
		"model":       "gpt-4o",
	})
	var createRes struct {
		Agent domain.Agent `json:"agent"`
	}
	_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
	agentID := createRes.Agent.ID

	t.Run("regenerate returns 409 while generating", func(t *testing.T) {
		// Park the agent in generating to simulate an in-flight generation.
		_ = env.store.Agents().SetPromptState(context.Background(), ws.ID, agentID, domain.PromptsStatusGenerating, nil)

		wRegen := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/regen-mem-ws/agents/regen-agent/regenerate", ownerToken, nil)
		if wRegen.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict when regenerating while generating, got %d: %s", wRegen.Code, wRegen.Body.String())
		}
	})

	t.Run("regenerate runs synchronously and returns the final state", func(t *testing.T) {
		// Set to ready in store
		_ = env.store.Agents().SetPromptState(context.Background(), ws.ID, agentID, domain.PromptsStatusReady, nil)

		wRegen := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/regen-mem-ws/agents/regen-agent/regenerate", ownerToken, nil)
		if wRegen.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on synchronous regenerate, got %d: %s", wRegen.Code, wRegen.Body.String())
		}
		var res struct {
			Agent domain.Agent `json:"agent"`
		}
		_ = json.Unmarshal(wRegen.Body.Bytes(), &res)
		if res.Agent.PromptsStatus != domain.PromptsStatusReady {
			t.Errorf("expected final prompts_status ready, got %q", res.Agent.PromptsStatus)
		}
	})
}

// -----------------------------------------------------------------------------
// 4. ListSessionEvents Handler Tests
// -----------------------------------------------------------------------------

func TestAgents_ListSessionEvents(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	ownerUser, ownerToken := createTestUser(t, env, "owner-hist@example.com", "Owner", "pwd")
	_ = ownerToken
	memberUser, memberToken := createTestUser(t, env, "member-hist@example.com", "Member", "pwd")
	noPermUser, noPermToken := createTestUser(t, env, "noperm-hist@example.com", "NoPerm", "pwd")
	otherWsUser, otherWsToken := createTestUser(t, env, "other-hist@example.com", "Other", "pwd")

	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, "hist-ws", "History WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	// Role without agents.read permission
	noPermRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "no-agents-read",
		Permissions: []string{domain.WorkspaceRead},
	}
	if err := env.store.Roles().Create(ctx, noPermRole); err != nil {
		t.Fatalf("failed to create no-perm role: %v", err)
	}
	addMember(t, env, ws.ID, noPermUser.ID, noPermRole.ID)

	// Second workspace for cross-tenant isolation testing
	otherWs, otherOwnerRole, _, _ := createTestWorkspaceWithRoles(t, env, "other-ws", "Other WS")
	addMember(t, env, otherWs.ID, otherWsUser.ID, otherOwnerRole.ID)

	// Create providers
	prov := &domain.ProviderConfig{
		WorkspaceID: ws.ID,
		Type:        "openai",
		Name:        "OpenAI WS",
		Enabled:     true,
	}
	if err := env.store.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	otherProv := &domain.ProviderConfig{
		WorkspaceID: otherWs.ID,
		Type:        "openai",
		Name:        "OpenAI Other",
		Enabled:     true,
	}
	if err := env.store.Providers().Create(ctx, otherProv); err != nil {
		t.Fatalf("failed to create other provider: %v", err)
	}

	// Create agents
	ag := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "analyst",
		Name:        "Data Analyst",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
	}
	if err := env.store.Agents().Create(ctx, ag); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	otherAg := &domain.Agent{
		WorkspaceID: otherWs.ID,
		Slug:        "other-analyst",
		Name:        "Other Analyst",
		ProviderID:  otherProv.ID,
		Model:       "gpt-4o",
	}
	if err := env.store.Agents().Create(ctx, otherAg); err != nil {
		t.Fatalf("failed to create other agent: %v", err)
	}

	// Seed session events via ADKSessionAdapter
	sessionID := "sess-hist-1"
	t1 := time.Date(2026, 9, 4, 10, 0, 1, 0, time.UTC)
	t2 := time.Date(2026, 9, 4, 10, 0, 2, 0, time.UTC)
	t3 := time.Date(2026, 9, 4, 10, 0, 3, 0, time.UTC)
	t4 := time.Date(2026, 9, 4, 10, 0, 4, 0, time.UTC)
	t5 := time.Date(2026, 9, 4, 10, 0, 5, 0, time.UTC)

	seededEvents := []*adk.SessionEvent[*schema.AgenticMessage]{
		{
			EventID:   "evt-1",
			TurnID:    "turn-1",
			Timestamp: t1,
			Message:   schema.UserAgenticMessage("Analyze report"),
		},
		{
			EventID:   "evt-2",
			TurnID:    "turn-1",
			Timestamp: t2,
			Kind:      adk.SessionEventSpanToolCallStart,
			Span: &adk.SpanEvent{
				Kind:      adk.SpanKindTool,
				StartedAt: t2,
				Tool: &adk.ToolSpanMeta{
					ToolUseID: "call-1",
					Name:      "web.search",
				},
			},
		},
		{
			EventID:   "evt-3",
			TurnID:    "turn-1",
			Timestamp: t3,
			Kind:      adk.SessionEventSpanToolCallEnd,
			Span: &adk.SpanEvent{
				Kind:    adk.SpanKindTool,
				EndedAt: t3,
				Tool: &adk.ToolSpanMeta{
					ToolUseID: "call-1",
					Name:      "web.search",
				},
			},
		},
		{
			EventID:          "evt-4",
			TurnID:           "turn-1",
			Timestamp:        t4,
			Kind:             adk.SessionEventMessagesReplaced,
			MessagesReplaced: &[]*schema.AgenticMessage{},
		},
		{
			EventID:   "evt-5",
			TurnID:    "turn-1",
			Timestamp: t5,
			Message: &schema.AgenticMessage{
				Role: schema.AgenticRoleTypeAssistant,
				ContentBlocks: []*schema.ContentBlock{
					schema.NewContentBlock(&schema.AssistantGenText{Text: "Analysis complete."}),
				},
			},
		},
	}

	adapter := agents.NewADKSessionAdapter(env.store.SessionEvents(), env.store.SessionCheckpoints(), ws.ID)
	if err := adapter.AppendEvents(ctx, sessionID, seededEvents); err != nil {
		t.Fatalf("failed to append seeded events: %v", err)
	}

	t.Run("200 with translated events", func(t *testing.T) {
		// Query by agent slug
		w := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events", ws.Slug, ag.Slug, sessionID), memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res agents.HistoryResult
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if len(res.Events) != 6 {
			t.Fatalf("expected 6 events, got %d", len(res.Events))
		}
		if res.Next != "" {
			t.Errorf("expected empty next cursor, got %q", res.Next)
		}

		// Verify event 1: user message
		e1 := res.Events[0]
		if e1.ID != "evt-1" || e1.TurnID != "turn-1" || e1.Kind != agents.TranscriptEventMessageCompleted {
			t.Errorf("unexpected event 1 metadata: %+v", e1)
		}
		if e1.Message == nil || e1.Message.Role != "user" || e1.Message.Content != "Analyze report" {
			t.Errorf("unexpected event 1 message payload: %+v", e1.Message)
		}

		// Verify event 2: tool call start
		e2 := res.Events[1]
		if e2.ID != "evt-2" || e2.TurnID != "turn-1" || e2.Kind != agents.TranscriptEventToolCallStarted {
			t.Errorf("unexpected event 2 metadata: %+v", e2)
		}
		if e2.ToolCall == nil || e2.ToolCall.CallID != "call-1" || e2.ToolCall.Name != "web.search" {
			t.Errorf("unexpected event 2 tool call payload: %+v", e2.ToolCall)
		}

		// Verify event 3: tool call end
		e3 := res.Events[2]
		if e3.ID != "evt-3" || e3.TurnID != "turn-1" || e3.Kind != agents.TranscriptEventToolCallFinished {
			t.Errorf("unexpected event 3 metadata: %+v", e3)
		}
		if e3.ToolResult == nil || e3.ToolResult.CallID != "call-1" || e3.ToolResult.Name != "web.search" {
			t.Errorf("unexpected event 3 tool result payload: %+v", e3.ToolResult)
		}

		// Verify event 4: compaction
		e4 := res.Events[3]
		if e4.ID != "evt-4" || e4.TurnID != "turn-1" || e4.Kind != agents.TranscriptEventContextCompacted {
			t.Errorf("unexpected event 4 metadata: %+v", e4)
		}
		if e4.Compaction == nil {
			t.Errorf("expected non-nil compaction payload")
		}

		// Verify event 5: assistant message
		e5 := res.Events[4]
		if e5.ID != "evt-5" || e5.TurnID != "turn-1" || e5.Kind != agents.TranscriptEventMessageCompleted {
			t.Errorf("unexpected event 5 metadata: %+v", e5)
		}
		if e5.Message == nil || e5.Message.Role != "assistant" || e5.Message.Content != "Analysis complete." {
			t.Errorf("unexpected event 5 message payload: %+v", e5.Message)
		}

		// Query by agent ID also resolves
		wByID := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events", ws.Slug, ag.ID, sessionID), memberToken, nil)
		if wByID.Code != http.StatusOK {
			t.Fatalf("expected 200 OK by agent ID, got %d: %s", wByID.Code, wByID.Body.String())
		}
		var resByID agents.HistoryResult
		if err := json.Unmarshal(wByID.Body.Bytes(), &resByID); err != nil {
			t.Fatalf("failed to unmarshal response by ID: %v", err)
		}
		if len(resByID.Events) != 6 {
			t.Errorf("expected 6 events by ID, got %d", len(resByID.Events))
		}
	})

	t.Run("limit and after query parameters honored with pagination cursor", func(t *testing.T) {
		// Page 1: limit=2
		w1 := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events?limit=2", ws.Slug, ag.Slug, sessionID), memberToken, nil)
		if w1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w1.Code, w1.Body.String())
		}
		var page1 agents.HistoryResult
		_ = json.Unmarshal(w1.Body.Bytes(), &page1)
		if len(page1.Events) != 2 {
			t.Fatalf("expected 2 events on page 1, got %d", len(page1.Events))
		}
		if page1.Events[0].ID != "evt-1" || page1.Events[1].ID != "evt-2" {
			t.Errorf("unexpected events on page 1: %+v", page1.Events)
		}
		if page1.Next != "evt-2" {
			t.Fatalf("expected next cursor 'evt-2', got %q", page1.Next)
		}

		// Page 2: limit=2&after=evt-2
		w2 := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events?limit=2&after=%s", ws.Slug, ag.Slug, sessionID, page1.Next), memberToken, nil)
		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
		}
		var page2 agents.HistoryResult
		_ = json.Unmarshal(w2.Body.Bytes(), &page2)
		if len(page2.Events) != 2 {
			t.Fatalf("expected 2 events on page 2, got %d", len(page2.Events))
		}
		if page2.Events[0].ID != "evt-3" || page2.Events[1].ID != "evt-4" {
			t.Errorf("unexpected events on page 2: %+v", page2.Events)
		}
		if page2.Next != "evt-4" {
			t.Fatalf("expected next cursor 'evt-4', got %q", page2.Next)
		}

		// Page 3: limit=2&after=evt-4
		w3 := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events?limit=2&after=%s", ws.Slug, ag.Slug, sessionID, page2.Next), memberToken, nil)
		if w3.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w3.Code, w3.Body.String())
		}
		var page3 agents.HistoryResult
		_ = json.Unmarshal(w3.Body.Bytes(), &page3)
		if len(page3.Events) != 2 {
			t.Fatalf("expected 2 events on page 3 (evt-5 + terminal turn_completed), got %d", len(page3.Events))
		}
		if page3.Events[0].ID != "evt-5" {
			t.Errorf("unexpected event on page 3: %+v", page3.Events[0])
		}
		if page3.Events[1].Kind != agents.TranscriptEventTurnCompleted {
			t.Errorf("expected terminal turn_completed on page 3: %+v", page3.Events[1])
		}
		if page3.Next != "" {
			t.Errorf("expected empty next cursor on final page, got %q", page3.Next)
		}

		// Out-of-range cursor yields empty page
		w4 := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events?limit=2&after=evt-5", ws.Slug, ag.Slug, sessionID), memberToken, nil)
		if w4.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w4.Code, w4.Body.String())
		}
		var page4 agents.HistoryResult
		_ = json.Unmarshal(w4.Body.Bytes(), &page4)
		if len(page4.Events) != 0 {
			t.Errorf("expected 0 events, got %d", len(page4.Events))
		}
		if page4.Next != "" {
			t.Errorf("expected empty next cursor, got %q", page4.Next)
		}

		// Invalid non-numeric limit -> 400 Bad Request
		wInvalid := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events?limit=abc", ws.Slug, ag.Slug, sessionID), memberToken, nil)
		if wInvalid.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for non-numeric limit, got %d: %s", wInvalid.Code, wInvalid.Body.String())
		}

		// Non-positive limit -> 400 Bad Request
		wZero := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events?limit=0", ws.Slug, ag.Slug, sessionID), memberToken, nil)
		if wZero.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for limit=0, got %d: %s", wZero.Code, wZero.Body.String())
		}

		// Limit exceeding 500 is capped at 500 and succeeds
		wLarge := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events?limit=1000", ws.Slug, ag.Slug, sessionID), memberToken, nil)
		if wLarge.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for large limit, got %d: %s", wLarge.Code, wLarge.Body.String())
		}
	})

	t.Run("403 Forbidden for user without agents.read permission", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events", ws.Slug, ag.Slug, sessionID), noPermToken, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("404 Not Found for cross-tenant agent access or non-existent agent", func(t *testing.T) {
		// Non-existent agent slug in workspace
		wNonExistent := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/non-existent-agent/sessions/%s/events", ws.Slug, sessionID), memberToken, nil)
		if wNonExistent.Code != http.StatusNotFound {
			t.Errorf("expected 404 for non-existent agent, got %d: %s", wNonExistent.Code, wNonExistent.Body.String())
		}

		// Agent from other workspace addressed via ws.Slug
		wCrossSlug := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events", ws.Slug, otherAg.Slug, sessionID), memberToken, nil)
		if wCrossSlug.Code != http.StatusNotFound {
			t.Errorf("expected 404 for cross-tenant agent slug, got %d: %s", wCrossSlug.Code, wCrossSlug.Body.String())
		}

		// Agent from other workspace addressed by ID
		wCrossID := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events", ws.Slug, otherAg.ID, sessionID), memberToken, nil)
		if wCrossID.Code != http.StatusNotFound {
			t.Errorf("expected 404 for cross-tenant agent ID, got %d: %s", wCrossID.Code, wCrossID.Body.String())
		}

		// User in other workspace addressing ws's agent
		wCrossTenantUser := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/%s/events", otherWs.Slug, ag.Slug, sessionID), otherWsToken, nil)
		if wCrossTenantUser.Code != http.StatusNotFound {
			t.Errorf("expected 404 for other workspace accessing agent, got %d: %s", wCrossTenantUser.Code, wCrossTenantUser.Body.String())
		}
	})

	t.Run("200 with empty events for unknown session", func(t *testing.T) {
		w := doRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/%s/agents/%s/sessions/unknown-sess-999/events", ws.Slug, ag.Slug), memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for unknown session, got %d: %s", w.Code, w.Body.String())
		}

		var res agents.HistoryResult
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if len(res.Events) != 0 {
			t.Errorf("expected 0 events, got %d", len(res.Events))
		}
		if res.Next != "" {
			t.Errorf("expected empty next cursor, got %q", res.Next)
		}
	})
}

// TestAgents_ComputedContextFields covers the response-only computed context
// fields (design D4): every agent payload carries the effective context window
// and the summarization trigger execution would arm, and neither is writable.
func TestAgents_ComputedContextFields(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "ctx-fields-ws", "Ctx Fields WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)

	wProv := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/ctx-fields-ws/providers", ownerToken, map[string]any{
		"type": "openai",
		"name": "OpenAI",
		"key":  "sk-test-key",
	})
	var provRes struct {
		Provider handlers.ProviderResponse `json:"provider"`
	}
	_ = json.Unmarshal(wProv.Body.Bytes(), &provRes)
	provID := provRes.Provider.ID

	type computedFields struct {
		ContextWindow              *int `json:"context_window"`
		EffectiveContextWindow     int  `json:"effective_context_window"`
		SummarizationTriggerTokens int  `json:"summarization_trigger_tokens"`
	}

	t.Run("stored window 50000 computes effective 50000 and trigger 37500", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/ctx-fields-ws/agents", ownerToken, map[string]any{
			"name":           "Ctx Agent",
			"slug":           "ctx-agent",
			"role":           "tester",
			"brief":          "brief",
			"provider_id":    provID,
			"model":          "gpt-4o",
			"context_window": 50000,
		})
		if wCreate.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", wCreate.Code, wCreate.Body.String())
		}
		var createRes struct {
			Agent computedFields `json:"agent"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
		if createRes.Agent.ContextWindow == nil || *createRes.Agent.ContextWindow != 50000 {
			t.Errorf("expected stored context_window 50000, got %v", createRes.Agent.ContextWindow)
		}
		if createRes.Agent.EffectiveContextWindow != 50000 || createRes.Agent.SummarizationTriggerTokens != 37500 {
			t.Errorf("computed fields = effective %d trigger %d, want 50000/37500",
				createRes.Agent.EffectiveContextWindow, createRes.Agent.SummarizationTriggerTokens)
		}

		// Detail and roster responses carry the same computed fields.
		wGet := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/ctx-fields-ws/agents/ctx-agent", ownerToken, nil)
		if wGet.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on get agent, got %d: %s", wGet.Code, wGet.Body.String())
		}
		var getRes struct {
			Agent computedFields `json:"agent"`
		}
		_ = json.Unmarshal(wGet.Body.Bytes(), &getRes)
		if getRes.Agent.EffectiveContextWindow != 50000 || getRes.Agent.SummarizationTriggerTokens != 37500 {
			t.Errorf("get computed fields = effective %d trigger %d, want 50000/37500",
				getRes.Agent.EffectiveContextWindow, getRes.Agent.SummarizationTriggerTokens)
		}

		wList := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/ctx-fields-ws/agents", ownerToken, nil)
		if wList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list agents, got %d: %s", wList.Code, wList.Body.String())
		}
		var listRes struct {
			Agents []map[string]any `json:"agents"`
		}
		_ = json.Unmarshal(wList.Body.Bytes(), &listRes)
		found := false
		for _, entry := range listRes.Agents {
			if entry["slug"] == "ctx-agent" {
				found = true
				if entry["effective_context_window"] != float64(50000) || entry["summarization_trigger_tokens"] != float64(37500) {
					t.Errorf("list entry computed fields = %v/%v, want 50000/37500",
						entry["effective_context_window"], entry["summarization_trigger_tokens"])
				}
			}
		}
		if !found {
			t.Fatal("ctx-agent missing from list response")
		}
	})

	t.Run("computed fields on requests are ignored, responses recompute from stored", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/ctx-fields-ws/agents", ownerToken, map[string]any{
			"name":                         "Ignored Computed",
			"slug":                         "ignored-computed",
			"role":                         "tester",
			"brief":                        "brief",
			"provider_id":                  provID,
			"model":                        "gpt-4o",
			"context_window":               50000,
			"effective_context_window":     999,
			"summarization_trigger_tokens": 999,
		})
		if wCreate.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", wCreate.Code, wCreate.Body.String())
		}
		var createRes struct {
			Agent computedFields `json:"agent"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
		// The response recomputes from the stored window; 999 is never stored
		// nor echoed.
		if createRes.Agent.EffectiveContextWindow != 50000 || createRes.Agent.SummarizationTriggerTokens != 37500 {
			t.Errorf("create response computed fields = effective %d trigger %d, want recomputed 50000/37500",
				createRes.Agent.EffectiveContextWindow, createRes.Agent.SummarizationTriggerTokens)
		}
		stored, err := env.store.Agents().BySlug(ctx, ws.ID, "ignored-computed")
		if err != nil {
			t.Fatalf("load stored agent: %v", err)
		}
		if stored.ContextWindow == nil || *stored.ContextWindow != 50000 {
			t.Errorf("stored context_window = %v, want 50000", stored.ContextWindow)
		}

		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/ctx-fields-ws/agents/ctx-agent", ownerToken, map[string]any{
			"description":                  "carries an editable field alongside",
			"effective_context_window":     999,
			"summarization_trigger_tokens": 999,
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on patch, got %d: %s", wPatch.Code, wPatch.Body.String())
		}
		var patchRes struct {
			Agent computedFields `json:"agent"`
		}
		_ = json.Unmarshal(wPatch.Body.Bytes(), &patchRes)
		if patchRes.Agent.EffectiveContextWindow != 50000 || patchRes.Agent.SummarizationTriggerTokens != 37500 {
			t.Errorf("patch response computed fields = effective %d trigger %d, want recomputed 50000/37500",
				patchRes.Agent.EffectiveContextWindow, patchRes.Agent.SummarizationTriggerTokens)
		}
		storedAfter, err := env.store.Agents().BySlug(ctx, ws.ID, "ctx-agent")
		if err != nil {
			t.Fatalf("load stored agent after patch: %v", err)
		}
		if storedAfter.ContextWindow == nil || *storedAfter.ContextWindow != 50000 {
			t.Errorf("stored context_window after patch = %v, want 50000", storedAfter.ContextWindow)
		}
	})

	t.Run("unset window falls back to the default budget", func(t *testing.T) {
		ag := &domain.Agent{
			WorkspaceID: ws.ID,
			Slug:        "unset-cw",
			Name:        "Unset CW",
			ProviderID:  provID,
			Model:       "gpt-4o",
		}
		if err := env.store.Agents().Create(ctx, ag); err != nil {
			t.Fatalf("create agent: %v", err)
		}

		wGet := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/ctx-fields-ws/agents/unset-cw", ownerToken, nil)
		if wGet.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", wGet.Code, wGet.Body.String())
		}
		var res struct {
			Agent computedFields `json:"agent"`
		}
		_ = json.Unmarshal(wGet.Body.Bytes(), &res)
		if res.Agent.ContextWindow != nil {
			t.Errorf("context_window = %v, want unset", res.Agent.ContextWindow)
		}
		if res.Agent.EffectiveContextWindow != 200000 || res.Agent.SummarizationTriggerTokens != 150000 {
			t.Errorf("computed fields = effective %d trigger %d, want 200000/150000",
				res.Agent.EffectiveContextWindow, res.Agent.SummarizationTriggerTokens)
		}
	})
}
