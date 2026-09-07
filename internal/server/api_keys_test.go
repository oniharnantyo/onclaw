package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
)

func apiKeyAuthHeader(plaintext string) string {
	return "Bearer " + plaintext
}

func createOwnerAndWorkspaceForKeys(t *testing.T, env *testEnv) (*domain.User, *domain.Workspace, string) {
	t.Helper()
	user, token := createTestUser(t, env, "keys-owner@example.com", "Keys Owner", "password-123")
	ws, ownerRole, _, _ := createTestWorkspaceWithRoles(t, env, "keys-ws", "Keys WS")
	addMember(t, env, ws.ID, user.ID, ownerRole.ID)
	return user, ws, token
}

func TestAPIKeyLifecycleViaNativeEndpoints(t *testing.T) {
	env := setupTestEnv(t)
	user, ws, token := createOwnerAndWorkspaceForKeys(t, env)

	// Create: plaintext returned exactly once, prefixed oc_ws_.
	var created struct {
		Key    string `json:"key"`
		APIKey struct {
			ID        string `json:"id"`
			KeyPrefix string `json:"key_prefix"`
			KeySuffix string `json:"key_suffix"`
		} `json:"api_key"`
	}
	body, _ := json.Marshal(map[string]string{"name": "ci-key"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/keys-ws/api-keys", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create api key: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !strings.HasPrefix(created.Key, "oc_ws_") {
		t.Fatalf("plaintext key = %q, want oc_ws_ prefix", created.Key)
	}
	if !strings.HasSuffix(created.Key, created.APIKey.KeySuffix) {
		t.Fatalf("suffix %q does not match plaintext %q", created.APIKey.KeySuffix, created.Key)
	}

	// List: display fields only, never the plaintext or hash.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/keys-ws/api-keys", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list api keys: status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), created.Key) {
		t.Fatal("list response leaks plaintext key")
	}

	// Revoke.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/keys-ws/api-keys/"+created.APIKey.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke api key: status = %d", rec.Code)
	}

	// Lookup by hash still finds it, but revoked.
	key, err := env.store.APIKeys().LookupByHash(t.Context(), domain.HashAPIKey(created.Key))
	if err != nil {
		t.Fatalf("lookup by hash: %v", err)
	}
	if !key.IsRevoked() {
		t.Fatal("revoked key should report IsRevoked")
	}
	if key.WorkspaceID != ws.ID || key.CreatedBy != user.ID {
		t.Fatalf("key scope = ws %q user %q, want %q/%q", key.WorkspaceID, key.CreatedBy, ws.ID, user.ID)
	}
}

// v1Probe mounts a probe route behind the /v1 API-key middleware and returns
// the resolved key scoped values, exercising authentication directly.
func v1Probe(t *testing.T, env *testEnv, authHeader string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	mw := server.NewV1Middlewares(env.store.APIKeys())
	r.Use(mw.APIKeyAuthRequired())
	var resolved string
	r.GET("/v1/models", func(c *gin.Context) {
		key := handlers.MustCurrentAPIKey(c)
		resolved = key.WorkspaceID + "/" + key.CreatedBy
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec, resolved
}

func TestV1APIKeyAuth(t *testing.T) {
	env := setupTestEnv(t)
	user, ws, _ := createOwnerAndWorkspaceForKeys(t, env)

	plaintext, _, err := services.NewAPIKeyService(env.store.APIKeys()).Create(t.Context(), ws.ID, "probe", user.ID)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	// Valid key resolves tenant scope.
	rec, resolved := v1Probe(t, env, apiKeyAuthHeader(plaintext))
	if rec.Code != http.StatusOK || resolved != ws.ID+"/"+user.ID {
		t.Fatalf("valid key: status = %d resolved = %q, want 200 %q", rec.Code, resolved, ws.ID+"/"+user.ID)
	}

	// Missing header.
	rec, _ = v1Probe(t, env, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing header: status = %d, want 401", rec.Code)
	}

	// Unknown key.
	rec, _ = v1Probe(t, env, apiKeyAuthHeader("oc_ws_totally-unknown-key-value"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key: status = %d, want 401", rec.Code)
	}

	// JWT must not authenticate /v1.
	_, jwt := createTestUser(t, env, "jwt@example.com", "JWT User", "password-123")
	rec, _ = v1Probe(t, env, "Bearer "+jwt)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("jwt: status = %d, want 401", rec.Code)
	}

	var envelope struct {
		Error struct {
			Type string  `json:"type"`
			Param *string `json:"param"`
			Code  *string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error.Type != handlers.V1ErrorTypeInvalidRequest || envelope.Error.Code == nil || *envelope.Error.Code != handlers.CodeInvalidAPIKey {
		t.Fatalf("error envelope = %+v, want invalid_request_error/invalid_api_key", envelope.Error)
	}

	// Revoked key stops authenticating immediately.
	if err := env.store.APIKeys().Revoke(t.Context(), ws.ID, mustListFirstKeyID(t, env, ws.ID)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	rec, _ = v1Probe(t, env, apiKeyAuthHeader(plaintext))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key: status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyExchange(t *testing.T) {
	env := setupTestEnv(t)

	owner, ownerToken := createTestUser(t, env, "keys-owner@example.com", "Keys Owner", "password-123")
	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, "keys-ws", "Keys WS")
	addMember(t, env, ws.ID, owner.ID, ownerRole.ID)

	// A plain Member (no workspace.write) exchanges a key.
	member, memberToken := createTestUser(t, env, "chat-member@example.com", "Chat Member", "password-123")
	addMember(t, env, ws.ID, member.ID, memberRole.ID)

	exchangePath := "/api/v1/workspaces/keys-ws/api-keys/exchange"
	var exchanged struct {
		Key    string `json:"key"`
		APIKey struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			CreatedBy string `json:"created_by"`
		} `json:"api_key"`
	}
	req := httptest.NewRequest(http.MethodPost, exchangePath, nil)
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("member exchange: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &exchanged); err != nil {
		t.Fatalf("decode exchange response: %v", err)
	}
	if !strings.HasPrefix(exchanged.Key, "oc_ws_") {
		t.Fatalf("plaintext key = %q, want oc_ws_ prefix", exchanged.Key)
	}
	if exchanged.APIKey.CreatedBy != member.ID {
		t.Fatalf("created_by = %q, want member %q", exchanged.APIKey.CreatedBy, member.ID)
	}

	// The exchanged key authenticates /v1 with the workspace as tenant scope.
	rec, resolved := v1Probe(t, env, apiKeyAuthHeader(exchanged.Key))
	if rec.Code != http.StatusOK || resolved != ws.ID+"/"+member.ID {
		t.Fatalf("exchanged key on /v1: status = %d resolved = %q, want 200 %q", rec.Code, resolved, ws.ID+"/"+member.ID)
	}

	// Revoking it through workspace settings stops /v1 authentication.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/keys-ws/api-keys/"+exchanged.APIKey.ID, nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke exchanged key: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec, _ = v1Probe(t, env, apiKeyAuthHeader(exchanged.Key))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked exchanged key on /v1: status = %d, want 401", rec.Code)
	}

	// Non-member is rejected without revealing workspace existence: the error
	// shape matches another member-route's non-member response for the same slug.
	_, nonMemberToken := createTestUser(t, env, "non-member@example.com", "Non Member", "password-123")
	req = httptest.NewRequest(http.MethodPost, exchangePath, nil)
	req.Header.Set("Authorization", "Bearer "+nonMemberToken)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-member exchange: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/keys-ws/tools", nil)
	req.Header.Set("Authorization", "Bearer "+nonMemberToken)
	other := httptest.NewRecorder()
	env.router.ServeHTTP(other, req)
	if other.Code != rec.Code {
		t.Fatalf("non-member status %d differs from other member route %d", rec.Code, other.Code)
	}

	// Unauthenticated is rejected as 401.
	req = httptest.NewRequest(http.MethodPost, exchangePath, nil)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated exchange: status = %d, want 401", rec.Code)
	}
}

func mustListFirstKeyID(t *testing.T, env *testEnv, wsID string) string {
	t.Helper()
	keys, err := env.store.APIKeys().List(t.Context(), wsID)
	if err != nil || len(keys) == 0 {
		t.Fatalf("list keys: %v (%d keys)", err, len(keys))
	}
	return keys[0].ID
}
