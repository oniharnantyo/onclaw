package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
)

// -----------------------------------------------------------------------------
// 1. Permission Matrix Tests
// -----------------------------------------------------------------------------

func TestProviders_PermissionMatrix(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	adminUser, adminToken := createTestUser(t, env, "admin@example.com", "Admin", "pwd")
	memberUser, memberToken := createTestUser(t, env, "member@example.com", "Member", "pwd")
	nonMemberUser, nonMemberToken := createTestUser(t, env, "nonmember@example.com", "Non Member", "pwd")
	_ = nonMemberUser

	ws, ownerRole, adminRole, memberRole := createTestWorkspaceWithRoles(t, env, "perm-ws", "Perm WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, adminUser.ID, adminRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	t.Run("non-member gets 404 across all provider endpoints", func(t *testing.T) {
		endpoints := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/v1/workspaces/perm-ws/providers", nil},
			{http.MethodPost, "/api/v1/workspaces/perm-ws/providers", map[string]string{"type": "openai", "name": "OpenAI"}},
			{http.MethodPatch, "/api/v1/workspaces/perm-ws/providers/fake-id", map[string]string{"name": "Renamed"}},
			{http.MethodDelete, "/api/v1/workspaces/perm-ws/providers/fake-id", nil},
			{http.MethodPost, "/api/v1/workspaces/perm-ws/providers/fake-id/verify", nil},
		}

		for _, ep := range endpoints {
			w := doRequest(env.router, ep.method, ep.path, nonMemberToken, ep.body)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s expected 404 for non-member, got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
			}
		}
	})

	t.Run("member has providers.read but not providers.write", func(t *testing.T) {
		// GET -> 200 OK
		w := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/perm-ws/providers", memberToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for member GET providers, got %d: %s", w.Code, w.Body.String())
		}

		// POST -> 403 Forbidden
		wPost := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/perm-ws/providers", memberToken, map[string]string{
			"type": "openai",
			"name": "Member OpenAI",
		})
		if wPost.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member POST, got %d: %s", wPost.Code, wPost.Body.String())
		}

		// PATCH -> 403 Forbidden
		wPatch := doRequest(env.router, http.MethodPatch, "/api/v1/workspaces/perm-ws/providers/fake-id", memberToken, map[string]string{
			"name": "Renamed",
		})
		if wPatch.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member PATCH, got %d: %s", wPatch.Code, wPatch.Body.String())
		}

		// DELETE -> 403 Forbidden
		wDelete := doRequest(env.router, http.MethodDelete, "/api/v1/workspaces/perm-ws/providers/fake-id", memberToken, nil)
		if wDelete.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member DELETE, got %d: %s", wDelete.Code, wDelete.Body.String())
		}

		// VERIFY -> 403 Forbidden
		wVerify := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/perm-ws/providers/fake-id/verify", memberToken, nil)
		if wVerify.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member verify, got %d: %s", wVerify.Code, wVerify.Body.String())
		}
	})

	t.Run("admin and owner have both providers.read and providers.write", func(t *testing.T) {
		// Admin creates config
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/perm-ws/providers", adminToken, map[string]any{
			"type": "openai",
			"name": "Admin OpenAI",
			"key":  "sk-admin-test-1234",
		})
		if wCreate.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for admin POST, got %d: %s", wCreate.Code, wCreate.Body.String())
		}
		var createdRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createdRes)
		provID := createdRes.Provider.ID

		// Owner updates config
		wPatch := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/perm-ws/providers/%s", provID), ownerToken, map[string]any{
			"name": "Owner Updated OpenAI",
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for owner PATCH, got %d: %s", wPatch.Code, wPatch.Body.String())
		}

		// Admin deletes config
		wDelete := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/perm-ws/providers/%s", provID), adminToken, nil)
		if wDelete.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content for admin DELETE, got %d: %s", wDelete.Code, wDelete.Body.String())
		}
	})
}

// -----------------------------------------------------------------------------
// 2. CRUD and Validation Tests
// -----------------------------------------------------------------------------

func TestProviders_CRUD_And_Validation(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "crud-ws", "CRUD WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)

	t.Run("create openai config with key", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "openai",
			"name": "Acme prod",
			"key":  "sk-proj-super-secret-key-1234",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if res.Provider.Type != "openai" || res.Provider.Name != "Acme prod" {
			t.Errorf("unexpected provider response: %+v", res.Provider)
		}
		if !res.Provider.KeySet {
			t.Errorf("expected key_set to be true")
		}
		if res.Provider.KeyHint != "1234" {
			t.Errorf("expected key_hint 1234, got %q", res.Provider.KeyHint)
		}
		if !res.Provider.Enabled {
			t.Errorf("expected enabled to default to true")
		}
	})

	t.Run("create config without key", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "anthropic",
			"name": "Anthropic Keyless",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Provider.KeySet {
			t.Errorf("expected key_set to be false")
		}
		if res.Provider.KeyHint != "" {
			t.Errorf("expected empty key_hint, got %q", res.Provider.KeyHint)
		}
	})

	t.Run("unknown type is rejected with 400 invalid_request naming valid types", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "azure-openai",
			"name": "Azure",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}

		var errEnv server.ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != server.CodeInvalidRequest {
			t.Errorf("expected code invalid_request, got %q", errEnv.Error.Code)
		}
		if !strings.Contains(errEnv.Error.Message, "openai") || !strings.Contains(errEnv.Error.Message, "anthropic") {
			t.Errorf("expected error message to list valid types, got %q", errEnv.Error.Message)
		}
	})

	t.Run("empty name is rejected with 400", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "openai",
			"name": "   ",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on empty name, got %d", w.Code)
		}
	})

	t.Run("compatible type requires base_url", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "openai-compatible",
			"name": "Local vLLM",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when compatible type lacks base_url, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid base_url scheme is rejected", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type":     "openai-compatible",
			"name":     "FTP LLM",
			"base_url": "ftp://proxy.example.com",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for ftp scheme, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("compatible type with valid http/https base_url succeeds", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type":     "openai-compatible",
			"name":     "Local vLLM",
			"base_url": "http://127.0.0.1:8000/v1",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Provider.BaseURL != "http://127.0.0.1:8000/v1" {
			t.Errorf("expected base_url http://127.0.0.1:8000/v1, got %q", res.Provider.BaseURL)
		}
	})

	t.Run("named type accepts base_url override", func(t *testing.T) {
		w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type":     "openai",
			"name":     "OpenAI Custom Proxy",
			"base_url": "https://openai.acme-proxy.com",
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Provider.BaseURL != "https://openai.acme-proxy.com" {
			t.Errorf("expected overridden base_url, got %q", res.Provider.BaseURL)
		}
	})

	t.Run("duplicate types in same workspace are allowed", func(t *testing.T) {
		w1 := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "openrouter",
			"name": "OpenRouter Prod",
		})
		w2 := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "openrouter",
			"name": "OpenRouter Staging",
		})
		if w1.Code != http.StatusCreated || w2.Code != http.StatusCreated {
			t.Fatalf("expected both duplicate types to be created, got %d and %d", w1.Code, w2.Code)
		}
	})

	t.Run("PATCH preserves stored key when key omitted", func(t *testing.T) {
		// Create config with key
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "openai",
			"name": "Original Name",
			"key":  "sk-secret-keep-this-5555",
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
		pID := createRes.Provider.ID

		// PATCH name and enabled only
		enabledFalse := false
		wPatch := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/crud-ws/providers/%s", pID), ownerToken, map[string]any{
			"name":    "Renamed Provider",
			"enabled": enabledFalse,
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on PATCH, got %d: %s", wPatch.Code, wPatch.Body.String())
		}

		var patchRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wPatch.Body.Bytes(), &patchRes)
		if patchRes.Provider.Name != "Renamed Provider" || patchRes.Provider.Enabled != false {
			t.Errorf("unexpected updated values: %+v", patchRes.Provider)
		}
		if !patchRes.Provider.KeySet {
			t.Errorf("expected key_set to remain true")
		}
		if patchRes.Provider.KeyHint != "5555" {
			t.Errorf("expected key_hint 5555 to be preserved, got %q", patchRes.Provider.KeyHint)
		}
	})

	t.Run("PATCH with empty string key is rejected with 400", func(t *testing.T) {
		// Create config
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "openai",
			"name": "Key to not unset",
			"key":  "sk-something-1234",
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
		pID := createRes.Provider.ID

		wPatch := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/crud-ws/providers/%s", pID), ownerToken, map[string]any{
			"key": "",
		})
		if wPatch.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on empty key patch, got %d: %s", wPatch.Code, wPatch.Body.String())
		}
	})

	t.Run("PATCH updates key when new key is provided", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "openai",
			"name": "Key to rotate",
			"key":  "sk-initial-key-1111",
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
		pID := createRes.Provider.ID

		wPatch := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/crud-ws/providers/%s", pID), ownerToken, map[string]any{
			"key": "sk-replacement-key-9999",
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on key PATCH, got %d: %s", wPatch.Code, wPatch.Body.String())
		}
		var patchRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wPatch.Body.Bytes(), &patchRes)
		if patchRes.Provider.KeyHint != "9999" {
			t.Errorf("expected updated key_hint 9999, got %q", patchRes.Provider.KeyHint)
		}
	})

	t.Run("DELETE provider", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "gemini",
			"name": "Gemini to Delete",
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
		pID := createRes.Provider.ID

		// Delete
		wDel := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/crud-ws/providers/%s", pID), ownerToken, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content, got %d", wDel.Code)
		}

		// Subsequent PATCH should 404
		wPatch := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/crud-ws/providers/%s", pID), ownerToken, map[string]any{"name": "x"})
		if wPatch.Code != http.StatusNotFound {
			t.Errorf("expected 404 on deleted provider PATCH, got %d", wPatch.Code)
		}

		// Cross-tenant delete: Workspace B cannot delete Workspace A's config
		wsB, ownerRoleB, _, _ := createTestWorkspaceWithRoles(t, env, "tenant-b", "Tenant B")
		userB, tokenB := createTestUser(t, env, "owner-b@example.com", "Owner B", "pwd")
		addMember(t, env, wsB.ID, userB.ID, ownerRoleB.ID)

		wCreateA := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/crud-ws/providers", ownerToken, map[string]any{
			"type": "gemini",
			"name": "Gemini A",
		})
		var createResA struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreateA.Body.Bytes(), &createResA)

		wDelCross := doRequest(env.router, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/tenant-b/providers/%s", createResA.Provider.ID), tokenB, nil)
		if wDelCross.Code != http.StatusNotFound {
			t.Errorf("expected 404 on cross-tenant delete, got %d", wDelCross.Code)
		}
	})
}

// -----------------------------------------------------------------------------
// 3. Key Secrecy on the Wire Tests
// -----------------------------------------------------------------------------

func TestProviders_KeySecrecy(t *testing.T) {
	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "secret-ws", "Secret WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)

	secretKey := "sk-SUPER-SECRET-KEY-1234567890-ABCDEF"

	// 1. Create response check
	wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/secret-ws/providers", ownerToken, map[string]any{
		"type": "openai",
		"name": "Secret Provider",
		"key":  secretKey,
	})
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", wCreate.Code, wCreate.Body.String())
	}

	bodyCreate := wCreate.Body.String()
	if strings.Contains(bodyCreate, secretKey) {
		t.Fatalf("CREATE response leaked plaintext key!")
	}
	if strings.Contains(bodyCreate, secrets.Version1Prefix+":") {
		t.Fatalf("CREATE response leaked ciphertext envelope!")
	}

	var createRes struct {
		Provider handlers.ProviderResponse `json:"provider"`
	}
	_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
	pID := createRes.Provider.ID

	// 2. List response check
	wList := doRequest(env.router, http.MethodGet, "/api/v1/workspaces/secret-ws/providers", ownerToken, nil)
	bodyList := wList.Body.String()
	if strings.Contains(bodyList, secretKey) {
		t.Fatalf("LIST response leaked plaintext key!")
	}
	if strings.Contains(bodyList, secrets.Version1Prefix+":") {
		t.Fatalf("LIST response leaked ciphertext envelope!")
	}

	// 3. PATCH response check
	wPatch := doRequest(env.router, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/secret-ws/providers/%s", pID), ownerToken, map[string]any{
		"name": "Updated Secret Provider",
	})
	bodyPatch := wPatch.Body.String()
	if strings.Contains(bodyPatch, secretKey) {
		t.Fatalf("PATCH response leaked plaintext key!")
	}
	if strings.Contains(bodyPatch, secrets.Version1Prefix+":") {
		t.Fatalf("PATCH response leaked ciphertext envelope!")
	}
}

// -----------------------------------------------------------------------------
// 4. Verify Endpoint Tests
// -----------------------------------------------------------------------------

func TestProviders_Verify(t *testing.T) {
	// Set up mock provider HTTP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "Bearer valid-mock-key" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "mock-model"}]}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Invalid credentials from mock server"}}`))
	}))
	defer mockServer.Close()

	env := setupTestEnv(t)
	ownerUser, ownerToken := createTestUser(t, env, "owner@example.com", "Owner", "pwd")
	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "verify-ws", "Verify WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)

	t.Run("verify keyless config returns 400 invalid_request", func(t *testing.T) {
		// A keyless config of a KEY-REQUIRING type: still 400. Keyless-capable
		// types ("openai-compatible") verify with an empty key instead.
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/verify-ws/providers", ownerToken, map[string]any{
			"type":     "openai",
			"name":     "Keyless Config",
			"base_url": mockServer.URL,
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)

		wVerify := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/verify-ws/providers/%s/verify", createRes.Provider.ID), ownerToken, nil)
		if wVerify.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for keyless verify, got %d: %s", wVerify.Code, wVerify.Body.String())
		}
		var errEnv server.ErrorEnvelope
		_ = json.Unmarshal(wVerify.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != server.CodeInvalidRequest {
			t.Errorf("expected invalid_request code, got %q", errEnv.Error.Code)
		}
	})

	t.Run("verify valid key against mock server returns 200 ok: true", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/verify-ws/providers", ownerToken, map[string]any{
			"type":     "openai-compatible",
			"name":     "Valid Config",
			"base_url": mockServer.URL,
			"key":      "valid-mock-key",
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)

		wVerify := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/verify-ws/providers/%s/verify", createRes.Provider.ID), ownerToken, nil)
		if wVerify.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for verify, got %d: %s", wVerify.Code, wVerify.Body.String())
		}
		var verifyRes struct {
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(wVerify.Body.Bytes(), &verifyRes); err != nil {
			t.Fatalf("failed to decode verify response: %v", err)
		}
		if !verifyRes.OK || verifyRes.Error != "" {
			t.Errorf("expected {ok: true}, got %+v", verifyRes)
		}
	})

	t.Run("verify invalid key returns 200 ok: false with provider error message", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/verify-ws/providers", ownerToken, map[string]any{
			"type":     "openai-compatible",
			"name":     "Invalid Key Config",
			"base_url": mockServer.URL,
			"key":      "wrong-key",
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)

		wVerify := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/verify-ws/providers/%s/verify", createRes.Provider.ID), ownerToken, nil)
		if wVerify.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for verify, got %d: %s", wVerify.Code, wVerify.Body.String())
		}
		var verifyRes struct {
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}
		_ = json.Unmarshal(wVerify.Body.Bytes(), &verifyRes)
		if verifyRes.OK {
			t.Errorf("expected ok: false, got true")
		}
		if !strings.Contains(verifyRes.Error, "Invalid credentials from mock server") {
			t.Errorf("expected error message to contain mock server message, got %q", verifyRes.Error)
		}
	})

	t.Run("verify unreachable base_url returns 200 ok: false", func(t *testing.T) {
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/verify-ws/providers", ownerToken, map[string]any{
			"type":     "openai-compatible",
			"name":     "Unreachable",
			"base_url": "http://127.0.0.1:1",
			"key":      "any-key",
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)

		wVerify := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/verify-ws/providers/%s/verify", createRes.Provider.ID), ownerToken, nil)
		if wVerify.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for verify, got %d: %s", wVerify.Code, wVerify.Body.String())
		}
		var verifyRes struct {
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}
		_ = json.Unmarshal(wVerify.Body.Bytes(), &verifyRes)
		if verifyRes.OK {
			t.Errorf("expected ok: false on unreachable URL")
		}
		if verifyRes.Error == "" {
			t.Errorf("expected descriptive error string")
		}
	})
}

// -----------------------------------------------------------------------------
// 5. AAD Tenant Binding and Decryption Failure Tests
// -----------------------------------------------------------------------------

func TestProviders_AAD_TenantBinding_And_DecryptionFailure(t *testing.T) {
	env := setupTestEnv(t)
	ownerUserA, ownerTokenA := createTestUser(t, env, "owner-a@example.com", "Owner A", "pwd")
	wsA, ownerRoleA, _, _ := createTestWorkspaceWithRoles(t, env, "ws-a", "WS A")
	addMember(t, env, wsA.ID, ownerUserA.ID, ownerRoleA.ID)

	ownerUserB, ownerTokenB := createTestUser(t, env, "owner-b@example.com", "Owner B", "pwd")
	wsB, ownerRoleB, _, _ := createTestWorkspaceWithRoles(t, env, "ws-b", "WS B")
	addMember(t, env, wsB.ID, ownerUserB.ID, ownerRoleB.ID)

	t.Run("cross-tenant ciphertext replay fails with undecryptable error envelope", func(t *testing.T) {
		// Workspace A creates config with key
		wCreateA := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/ws-a/providers", ownerTokenA, map[string]any{
			"type": "openai",
			"name": "OpenAI in WS A",
			"key":  "sk-workspace-a-secret-key-1234",
		})
		var createResA struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreateA.Body.Bytes(), &createResA)

		// Read raw stored entity from store
		pA, err := env.store.Providers().ByID(context.Background(), wsA.ID, createResA.Provider.ID)
		if err != nil {
			t.Fatalf("failed to retrieve stored provider from A: %v", err)
		}

		// Replay the exact ciphertext envelope into a new config row belonging to Workspace B
		pB := &domain.ProviderConfig{
			WorkspaceID:   wsB.ID,
			Type:          "openai",
			Name:          "Replayed Config in B",
			KeyCiphertext: pA.KeyCiphertext, // Encrypted with wsA.ID as AAD
			KeyHint:       pA.KeyHint,
			Enabled:       true,
		}
		if err := env.store.Providers().Create(context.Background(), pB); err != nil {
			t.Fatalf("failed to create provider in B: %v", err)
		}

		// Verify in Workspace B should fail decryption (AAD mismatch: wsB.ID != wsA.ID)
		wVerifyB := doRequest(env.router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/ws-b/providers/%s/verify", pB.ID), ownerTokenB, nil)
		if wVerifyB.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on undecryptable secret, got %d: %s", wVerifyB.Code, wVerifyB.Body.String())
		}
		var errEnv server.ErrorEnvelope
		_ = json.Unmarshal(wVerifyB.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != server.CodeUndecryptable {
			t.Errorf("expected error code %q, got %q", server.CodeUndecryptable, errEnv.Error.Code)
		}
	})

	t.Run("master key changed renders keys undecryptable but non-key edits and deletes work", func(t *testing.T) {
		// Create config in WS A with original server instance
		wCreate := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/ws-a/providers", ownerTokenA, map[string]any{
			"type": "openai",
			"name": "Pre-Rotation Config",
			"key":  "sk-pre-rotation-key-5678",
		})
		var createRes struct {
			Provider handlers.ProviderResponse `json:"provider"`
		}
		_ = json.Unmarshal(wCreate.Body.Bytes(), &createRes)
		pID := createRes.Provider.ID

		// Simulate server restart with a DIFFERENT encryption key
		rotatedKey := []byte("rotated-key-32-bytes-long-123456")
		rotatedRouter := server.NewRouter(server.RouterOptions{
			Store:         env.store,
			Storage:       env.storage,
			Issuer:        env.issuer,
			Auth:          env.service,
			EncryptionKey: rotatedKey,
		})

		// 1. Verify returns undecryptable error envelope
		wVerify := doRequest(rotatedRouter, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/ws-a/providers/%s/verify", pID), ownerTokenA, nil)
		if wVerify.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on verify with rotated key, got %d: %s", wVerify.Code, wVerify.Body.String())
		}
		var errEnv server.ErrorEnvelope
		_ = json.Unmarshal(wVerify.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != server.CodeUndecryptable {
			t.Errorf("expected code undecryptable, got %q", errEnv.Error.Code)
		}

		// 2. Renaming/toggling via PATCH still succeeds
		wPatch := doRequest(rotatedRouter, http.MethodPatch, fmt.Sprintf("/api/v1/workspaces/ws-a/providers/%s", pID), ownerTokenA, map[string]any{
			"name": "Renamed Without Key",
		})
		if wPatch.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on PATCH despite undecryptable key, got %d: %s", wPatch.Code, wPatch.Body.String())
		}

		// 3. Deletion still succeeds
		wDel := doRequest(rotatedRouter, http.MethodDelete, fmt.Sprintf("/api/v1/workspaces/ws-a/providers/%s", pID), ownerTokenA, nil)
		if wDel.Code != http.StatusNoContent {
			t.Fatalf("expected 204 No Content on DELETE, got %d", wDel.Code)
		}
	})
}
