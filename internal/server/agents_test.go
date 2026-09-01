package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
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
		}

		for _, ep := range endpoints {
			w := doRequest(env.router, ep.method, ep.path, nonMemberToken, ep.body)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s expected 404 for non-member, got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
			}
		}
	})

	t.Run("member has agents.read and memory access, but not agents.write", func(t *testing.T) {
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

		// GET memory -> 200 OK
		wMemGet := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/agents-perm-ws/agents/atlas/memory", memberToken, nil)
		if wMemGet.Code != http.StatusOK {
			t.Errorf("expected 200 OK on get memory for member, got %d", wMemGet.Code)
		}

		// DELETE memory -> 204 No Content
		wMemDel := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/agents-perm-ws/agents/atlas/memory", memberToken, nil)
		if wMemDel.Code != http.StatusNoContent {
			t.Errorf("expected 204 No Content on delete memory for member, got %d", wMemDel.Code)
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

	// Create workspace skills
	_ = env.store.WorkspaceSkills().Create(context.Background(), &domain.WorkspaceSkill{
		WorkspaceID: ws.ID,
		Name:        "code-review",
		Description: "Review pull requests",
		Body:        "# Code Review Guidelines",
		Enabled:     true,
	})
	_ = env.store.WorkspaceSkills().Create(context.Background(), &domain.WorkspaceSkill{
		WorkspaceID: ws.ID,
		Name:        "disabled-skill",
		Description: "Disabled custom skill",
		Body:        "# Disabled",
		Enabled:     false,
	})

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
		// Creation seeds the base prompt; generation writes the three documents
		// before the ready transition.
		for _, name := range []string{"AGENTS.md", "IDENTITY.md", "SOUL.md", "BOOTSTRAP.md"} {
			if _, err := os.Stat(filepath.Join(expectedDir, name)); err != nil {
				t.Errorf("expected %s in agent workspace dir: %v", name, err)
			}
		}
		// Generation runs synchronously with a stubbed model factory, so the
		// create response already carries the final prompt state.
		if agent.PromptsStatus != domain.PromptsStatusReady {
			t.Errorf("expected prompts_status %q, got %q", domain.PromptsStatusReady, agent.PromptsStatus)
		}
		if len(agent.Tools) != 0 || len(agent.Skills) != 0 || len(agent.MCP) != 0 {
			t.Errorf("expected empty capability arrays, got tools=%v skills=%v mcp=%v", agent.Tools, agent.Skills, agent.MCP)
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
		if res.Agent.Identity != "# Identity\nStub identity" || res.Agent.Soul != "# Soul\nStub soul" || res.Agent.Bootstrap != "# BOOTSTRAP.md - Birth Sequence\nStub bootstrap" {
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

	t.Run("skills array validation checks workspace skills", func(t *testing.T) {
		// Unknown skill name rejected with 400
		wUnknownSkill := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Skills Test 1",
			"slug":        "skills-test-1",
			"role":        "tester",
			"brief":       "brief",
			"provider_id": openAIProvID,
			"model":       "gpt-4o",
			"skills":      []string{"code-review", "non-existent-skill"},
		})
		if wUnknownSkill.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on unknown skill, got %d: %s", wUnknownSkill.Code, wUnknownSkill.Body.String())
		}

		// Disabled skill accepted
		wDisabledSkill := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-agents-ws/agents", ownerToken, map[string]any{
			"name":        "Skills Test 2",
			"slug":        "skills-test-2",
			"role":        "tester",
			"brief":       "brief",
			"provider_id": openAIProvID,
			"model":       "gpt-4o",
			"skills":      []string{"code-review", "disabled-skill"},
		})
		if wDisabledSkill.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created with disabled skill, got %d: %s", wDisabledSkill.Code, wDisabledSkill.Body.String())
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
// 3. Regeneration & Memory Endpoint Tests
// -----------------------------------------------------------------------------

func TestAgents_Regenerate_And_Memories(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	memberUser, memberToken := createTestUser(t, env, "member@example.com", "Member", "pwd")

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

	t.Run("memory view and reset are membership-gated for own memory", func(t *testing.T) {
		// GET non-existent memory -> returns 200 with empty content
		wGetEmpty := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/regen-mem-ws/agents/regen-agent/memory", memberToken, nil)
		if wGetEmpty.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for empty memory, got %d: %s", wGetEmpty.Code, wGetEmpty.Body.String())
		}
		var emptyRes struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(wGetEmpty.Body.Bytes(), &emptyRes)
		if emptyRes.Content != "" {
			t.Errorf("expected empty content, got %q", emptyRes.Content)
		}

		// Insert runtime-owned memory for memberUser
		_ = env.store.AgentUserMemories().Upsert(context.Background(), &domain.AgentUserMemory{
			WorkspaceID: ws.ID,
			AgentID:     agentID,
			UserID:      memberUser.ID,
			Content:     "User prefers Go over Python and likes dark mode.",
		})

		// GET existing memory for memberUser
		wGet := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/regen-mem-ws/agents/regen-agent/memory", memberToken, nil)
		if wGet.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for existing memory, got %d: %s", wGet.Code, wGet.Body.String())
		}
		var memRes struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(wGet.Body.Bytes(), &memRes)
		if memRes.Content != "User prefers Go over Python and likes dark mode." {
			t.Errorf("expected stored memory content, got %q", memRes.Content)
		}

		// Owner views their own memory -> empty (isolated per-user)
		wOwnerGet := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/regen-mem-ws/agents/regen-agent/memory", ownerToken, nil)
		if wOwnerGet.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for owner memory, got %d", wOwnerGet.Code)
		}
		var ownerMemRes struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(wOwnerGet.Body.Bytes(), &ownerMemRes)
		if ownerMemRes.Content != "" {
			t.Errorf("expected empty memory for owner, got %q", ownerMemRes.Content)
		}

		// Member resets (DELETEs) own memory
		wDel := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/regen-mem-ws/agents/regen-agent/memory", memberToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on memory reset, got %d", wDel.Code)
		}

		// Subsequent GET returns empty content
		wGetAfterReset := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/regen-mem-ws/agents/regen-agent/memory", memberToken, nil)
		if wGetAfterReset.Code != http.StatusOK {
			t.Fatalf("expected 200 OK after reset, got %d", wGetAfterReset.Code)
		}
		var afterRes struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(wGetAfterReset.Body.Bytes(), &afterRes)
		if afterRes.Content != "" {
			t.Errorf("expected empty content after reset, got %q", afterRes.Content)
		}
	})

	t.Run("deleting agent cascades to per-user memories", func(t *testing.T) {
		// Set memory
		_ = env.store.AgentUserMemories().Upsert(context.Background(), &domain.AgentUserMemory{
			WorkspaceID: ws.ID,
			AgentID:     agentID,
			UserID:      memberUser.ID,
			Content:     "Some persistent context",
		})

		// Delete agent
		wDelAgent := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/regen-mem-ws/agents/regen-agent", ownerToken, nil)
		if wDelAgent.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on agent delete, got %d", wDelAgent.Code)
		}

		// Verify memories are gone
		_, err := env.store.AgentUserMemories().Get(context.Background(), ws.ID, agentID, memberUser.ID)
		if err == nil {
			t.Fatalf("expected memory to be cascade deleted")
		}
	})
}
