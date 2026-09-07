package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
)

func TestWorkspaces_AtomicBirth(t *testing.T) {
	env := setupTestEnv(t)
	user, token := createTestUser(t, env, "birth-creator@example.com", "Birth Creator", "password123")
	_ = user

	t.Run("atomic birth success with provider and starter agent", func(t *testing.T) {
		secretKey := "sk-birth-secret-api-key-9999"

		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{
			"name":     "Birth WS",
			"slug":     "birth-ws",
			"timezone": "Asia/Jakarta",
			"provider": map[string]any{
				"type": "openai",
				"name": "Birth OpenAI",
				"key":  secretKey,
			},
			"starter_agent": map[string]any{
				"name":        "Starter Bot",
				"slug":        "starter-bot",
				"role":        "onboarding-guide",
				"description": "Guides new users",
				"brief":       "Welcome users and answer basic onboarding questions",
				"model":       "gpt-4o",
			},
		})

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for atomic birth, got %d: %s", w.Code, w.Body.String())
		}

		bodyStr := w.Body.String()
		if strings.Contains(bodyStr, secretKey) {
			t.Fatalf("atomic birth response leaked plaintext key!")
		}

		var res struct {
			Workspace    domain.Workspace          `json:"workspace"`
			Role         domain.Role               `json:"role"`
			Member       domain.Member             `json:"member"`
			Provider     handlers.ProviderResponse `json:"provider"`
			StarterAgent domain.Agent              `json:"starter_agent"`
			Agent        domain.Agent              `json:"agent"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode birth response: %v", err)
		}

		if res.Workspace.Slug != "birth-ws" {
			t.Errorf("expected workspace slug birth-ws, got %s", res.Workspace.Slug)
		}
		if res.Provider.Type != "openai" || !res.Provider.KeySet || res.Provider.KeyHint != "9999" {
			t.Errorf("unexpected provider in birth response: %+v", res.Provider)
		}
		if res.StarterAgent.Slug != "starter-bot" || res.StarterAgent.PromptsStatus != domain.PromptsStatusReady {
			t.Errorf("unexpected starter agent in birth response: %+v", res.StarterAgent)
		}
		expectedDir := domain.AgentWorkspaceDir(env.workspaceDir, "birth-ws", "starter-bot")
		if _, err := os.Stat(expectedDir); err != nil {
			t.Errorf("expected agent workspace directory on disk: %v", err)
		}
		// Birth seeds the base prompt and the synchronous generation writes the
		// three documents before the ready transition.
		for _, name := range []string{"AGENTS.md", "IDENTITY.md", "SOUL.md", "BOOTSTRAP.md"} {
			if _, err := os.Stat(filepath.Join(expectedDir, name)); err != nil {
				t.Errorf("expected %s in starter agent workspace dir: %v", name, err)
			}
		}
		// The birth response composes the prompt documents from those files.
		if res.StarterAgent.Identity != "# Identity\nStub identity" ||
			res.StarterAgent.Soul != "# Soul\nStub soul" ||
			res.StarterAgent.Bootstrap != promptdocs.BootstrapTemplate {
			t.Errorf("expected identity/soul/bootstrap composed from workspace files, got identity=%q soul=%q bootstrap=%q",
				res.StarterAgent.Identity, res.StarterAgent.Soul, res.StarterAgent.Bootstrap)
		}
		if res.StarterAgent.ProviderID != res.Provider.ID {
			t.Errorf("expected starter agent provider_id to match created provider ID %q, got %q", res.Provider.ID, res.StarterAgent.ProviderID)
		}

		// Verify database rows in store
		wsInStore, err := env.store.Workspaces().BySlug(context.Background(), "birth-ws")
		if err != nil || wsInStore == nil {
			t.Fatalf("workspace not found in store: %v", err)
		}

		provInStore, err := env.store.Providers().ByID(context.Background(), wsInStore.ID, res.Provider.ID)
		if err != nil || provInStore == nil {
			t.Fatalf("provider not found in store: %v", err)
		}

		agentInStore, err := env.store.Agents().BySlug(context.Background(), wsInStore.ID, "starter-bot")
		if err != nil || agentInStore == nil {
			t.Fatalf("starter agent not found in store: %v", err)
		}
	})

	t.Run("starter agent validation failure aborts the entire birth (no workspace created)", func(t *testing.T) {
		// Anthropic provider without max_tokens in starter agent -> 400
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{
			"name":     "Aborted Anthropic WS",
			"slug":     "aborted-anthropic-ws",
			"timezone": "UTC",
			"provider": map[string]any{
				"type": "anthropic",
				"name": "Anthropic Prod",
				"key":  "sk-ant-test-key",
			},
			"starter_agent": map[string]any{
				"name":  "Claude Starter",
				"slug":  "claude-starter",
				"role":  "assistant",
				"brief": "Assist users",
				"model": "claude-3-5-sonnet",
				// Missing required max_tokens for anthropic!
			},
		})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid starter agent, got %d: %s", w.Code, w.Body.String())
		}

		// Verify NO workspace row was created in store
		_, err := env.store.Workspaces().BySlug(context.Background(), "aborted-anthropic-ws")
		if err == nil {
			t.Fatalf("expected workspace 'aborted-anthropic-ws' to NOT exist in store after abort")
		}
	})

	t.Run("invalid starter agent slug aborts the entire birth", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{
			"name":     "Aborted Bad Slug WS",
			"slug":     "aborted-bad-slug-ws",
			"timezone": "UTC",
			"provider": map[string]any{
				"type": "openai",
				"name": "OpenAI",
			},
			"starter_agent": map[string]any{
				"name":  "Bad Bot",
				"slug":  "INVALID_SLUG!!",
				"role":  "helper",
				"brief": "some brief",
				"model": "gpt-4o",
			},
		})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid agent slug, got %d: %s", w.Code, w.Body.String())
		}

		// Verify NO workspace row was created in store
		_, err := env.store.Workspaces().BySlug(context.Background(), "aborted-bad-slug-ws")
		if err == nil {
			t.Fatalf("expected workspace 'aborted-bad-slug-ws' to NOT exist in store after abort")
		}
	})

	t.Run("provider validation failure aborts the entire birth", func(t *testing.T) {
		// Compatible type without base_url -> 400
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces", token, map[string]any{
			"name":     "Aborted Provider WS",
			"slug":     "aborted-prov-ws",
			"timezone": "UTC",
			"provider": map[string]any{
				"type": "openai-compatible",
				"name": "Local LLM",
				// Missing required base_url!
			},
			"starter_agent": map[string]any{
				"name":  "Local Bot",
				"slug":  "local-bot",
				"role":  "helper",
				"brief": "some brief",
				"model": "local-model",
			},
		})

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid provider config, got %d: %s", w.Code, w.Body.String())
		}

		// Verify NO workspace row was created in store
		_, err := env.store.Workspaces().BySlug(context.Background(), "aborted-prov-ws")
		if err == nil {
			t.Fatalf("expected workspace 'aborted-prov-ws' to NOT exist in store after abort")
		}
	})
}
