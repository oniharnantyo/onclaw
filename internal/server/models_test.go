package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	storagefake "github.com/oniharnantyo/onclaw/internal/storage/fake"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestModels_Endpoints_And_DeleteInUse(t *testing.T) {
	// Mock models.dev catalog server
	mockCatalogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		catalogJSON := `{
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"models": {
					"gpt-5": {
						"id": "gpt-5",
						"name": "GPT-5 Preview",
						"temperature": true,
						"reasoning_options": [
							{"type": "effort", "values": ["minimal", "low", "medium", "high"]}
						]
					}
				}
			}
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(catalogJSON))
	}))
	defer mockCatalogServer.Close()

	// Mock live provider server (e.g. OpenAI /v1/models) that also answers
	// chat completions so the synchronous create-time prompt generation
	// succeeds against it.
	mockLiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "chat/completions") {
			args, _ := json.Marshal(map[string]string{
				"identity":  "# Identity\nBound agent identity.",
				"soul":      "# Soul\nBound agent soul.",
				"bootstrap": "# BOOTSTRAP.md - Birth Sequence\nIntroduce yourself.",
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_prompts","type":"function","function":{"name":"submit_prompts","arguments":%q}}]},"finish_reason":"tool_calls"}]}`, string(args))))
			return
		}
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "Bearer valid-live-key" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"data": [
						{"id": "gpt-5", "object": "model"},
						{"id": "custom-live-model", "object": "model"}
					]
				}`))
				return
			}
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Invalid API key"}}`))
	}))
	defer mockLiveServer.Close()

	st := storefake.New()
	stor := storagefake.New()
	encKey := []byte("01234567890123456789012345678901")
	providerReg := providers.NewRegistry()
	catalogSvc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   t.TempDir(),
		CatalogURL: mockCatalogServer.URL,
		Registry:   providerReg,
	})

	testEnv := setupTestEnv(t)
	testEnv.store = st
	testEnv.storage = stor
	testEnv.router = server.NewRouter(server.RouterOptions{
		Store:         st,
		Storage:       stor,
		Issuer:        testEnv.issuer,
		Auth:          testEnv.service,
		EncryptionKey: encKey,
		Providers:     providerReg,
		ModelCatalog:  catalogSvc,
	})

	ownerUser, ownerToken := createTestUser(t, testEnv, "owner@example.com", "Owner", "pwd")
	memberUser, memberToken := createTestUser(t, testEnv, "member@example.com", "Member", "pwd")
	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, testEnv, "models-ws", "Models WS")
	addMember(t, testEnv, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, testEnv, ws.ID, memberUser.ID, memberRole.ID)

	// Create a provider with live mock credentials
	wCreateProv := doRequest(testEnv.router, http.MethodPost, "/api/v1/workspaces/models-ws/providers", ownerToken, map[string]any{
		"type":     "openai-compatible",
		"name":     "Live Mock Provider",
		"base_url": mockLiveServer.URL,
		"key":      "valid-live-key",
	})
	if wCreateProv.Code != http.StatusCreated {
		t.Fatalf("failed to create provider: %s", wCreateProv.Body.String())
	}
	var provRes struct {
		Provider handlers.ProviderResponse `json:"provider"`
	}
	_ = json.Unmarshal(wCreateProv.Body.Bytes(), &provRes)
	liveProvID := provRes.Provider.ID

	t.Run("GET /workspaces/:ws/providers/:id/models resolves live models", func(t *testing.T) {
		w := doRequest(testEnv.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/models-ws/providers/%s/models", liveProvID), memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for provider models, got %d: %s", w.Code, w.Body.String())
		}

		var res domain.ModelsResult
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode models result: %v", err)
		}

		if res.Source != domain.ModelSourceLive {
			t.Errorf("expected source %q, got %q", domain.ModelSourceLive, res.Source)
		}
		if len(res.Models) != 2 {
			t.Fatalf("expected 2 models from live mock, got %d", len(res.Models))
		}
	})

	t.Run("GET /workspaces/:ws/providers/:id/models falls back to catalog on live failure", func(t *testing.T) {
		// Create provider with invalid key
		wCreateBad := doRequest(testEnv.router, http.MethodPost, "/api/v1/workspaces/models-ws/providers", ownerToken, map[string]any{
			"type": "openai",
			"name": "Failing Live Provider",
			"key":  "invalid-key-will-fail-live",
		})
		var badProvRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreateBad.Body.Bytes(), &badProvRes)

		w := doRequest(testEnv.router, http.MethodGet, fmt.Sprintf("/api/v1/workspaces/models-ws/providers/%s/models", badProvRes.Provider.ID), memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on catalog fallback, got %d: %s", w.Code, w.Body.String())
		}

		var res domain.ModelsResult
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Source != domain.ModelSourceCatalog {
			t.Errorf("expected source catalog, got %q", res.Source)
		}
		if len(res.Models) != 1 || res.Models[0].ID != "gpt-5" {
			t.Errorf("expected catalog model gpt-5, got %+v", res.Models)
		}
		if len(res.Models[0].Efforts) != 4 {
			t.Errorf("expected 4 effort values for gpt-5 from mock catalog, got %v", res.Models[0].Efforts)
		}
	})

	t.Run("POST /api/v1/providers/models-preview resolves models without storing credentials", func(t *testing.T) {
		w := doRequest(testEnv.router, http.MethodPost, "/api/v1/providers/models-preview", memberToken, map[string]any{
			"type":     "openai-compatible",
			"base_url": mockLiveServer.URL,
			"key":      "valid-live-key",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on preview, got %d: %s", w.Code, w.Body.String())
		}

		var res domain.ModelsResult
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Source != domain.ModelSourceLive {
			t.Errorf("expected live source on preview, got %q", res.Source)
		}
		if len(res.Models) != 2 {
			t.Errorf("expected 2 models, got %d", len(res.Models))
		}
	})

	t.Run("POST /api/v1/providers/models-preview rejects unknown type with 400", func(t *testing.T) {
		w := doRequest(testEnv.router, http.MethodPost, "/api/v1/providers/models-preview", memberToken, map[string]any{
			"type": "azure-openai",
			"key":  "some-key",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on unknown type, got %d: %s", w.Code, w.Body.String())
		}

		var errEnv server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != server.CodeInvalidRequest {
			t.Errorf("expected code invalid_request, got %q", errEnv.Error.Code)
		}
		if !strings.Contains(errEnv.Error.Message, "openai") {
			t.Errorf("expected error message to list valid types, got %q", errEnv.Error.Message)
		}
	})

	t.Run("DeleteProvider is blocked with 409 conflict when referenced by agent", func(t *testing.T) {
		// Create agent referencing liveProvID
		wCreateAgent := doRequest(testEnv.router, http.MethodPost, "/api/v1/workspaces/models-ws/agents", ownerToken, map[string]any{
			"name":        "Bound Agent",
			"slug":        "bound-agent",
			"role":        "worker",
			"brief":       "test brief",
			"provider_id": liveProvID,
			"model":       "custom-live-model",
		})
		if wCreateAgent.Code != http.StatusCreated {
			t.Fatalf("failed to create agent: %s", wCreateAgent.Body.String())
		}

		// Attempt to delete provider
		wDelProv := doRequest(testEnv.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/models-ws/providers/%s", liveProvID), ownerToken, nil)
		if wDelProv.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict when deleting provider in use, got %d: %s", wDelProv.Code, wDelProv.Body.String())
		}

		var errEnv server.ErrorEnvelope
		_ = json.Unmarshal(wDelProv.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != server.CodeConflict {
			t.Errorf("expected conflict code, got %q", errEnv.Error.Code)
		}

		// Delete agent first
		wDelAgent := doRequest(testEnv.router, http.MethodDelete, "/api/v1/workspaces/models-ws/agents/bound-agent", ownerToken, nil)
		if wDelAgent.Code != http.StatusNoContent {
			t.Fatalf("failed to delete agent: %s", wDelAgent.Body.String())
		}

		// Now provider deletion succeeds
		wDelProv2 := doRequest(testEnv.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/models-ws/providers/%s", liveProvID), ownerToken, nil)
		if wDelProv2.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on deleting unreferenced provider, got %d: %s", wDelProv2.Code, wDelProv2.Body.String())
		}
	})
}
