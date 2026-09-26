package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// requireTestPermission mirrors the router's RequirePermission guard for the
// handlers-level test router: the resolved role must carry the permission.
func requireTestPermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := handlers.CurrentRole(c)
		if !ok || role == nil || !domain.HasPermission(role.Permissions, permission) {
			handlers.AbortForbidden(c, "insufficient permissions")
			return
		}
		c.Next()
	}
}

// newVerifyDraftRouter wires the draft-verify endpoint against a fake store
// with the workspace and role bound, mirroring the production middleware
// chain for this route (workspace context + providers.write guard).
func newVerifyDraftRouter(t *testing.T, role *domain.Role) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{Slug: "verify-draft-ws", Name: "Verify Draft WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	h := handlers.NewProviderHandlers(st.Providers(), st.Agents(), []byte("01234567890123456789012345678901"), providers.NewRegistry(), nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.RoleContextKey, role)
		c.Next()
	}, requireTestPermission(domain.ProvidersWrite))
	r.POST("/providers/verify-draft", h.VerifyDraft)
	return r, st, ws
}

func doVerifyDraftJSON(r *gin.Engine, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/verify-draft", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// newDraftMockProvider probes like the row-verify mock: only the
// "valid-mock-key" bearer authenticates; anything else gets a 401.
func newDraftMockProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer valid-mock-key" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "mock-model"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Invalid credentials from mock server"}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// newKeylessMockProvider serves 200 to any GET without auth: an endpoint that
// genuinely needs no API key, for keyless-capable verify probes.
func newKeylessMockProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": [{"id": "m"}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// seedDraftProvider stores a provider config directly, with the key encrypted
// under the same workspace-scoped envelope the handler decrypts with. The
// provider type is a parameter: key-requiring types (e.g. "openai") exercise
// stored-key resolution; "openai-compatible" is keyless-capable.
func seedDraftProvider(t *testing.T, st store.Store, wsID, providerType, name, baseURL, plaintextKey string) *domain.ProviderConfig {
	t.Helper()
	p := &domain.ProviderConfig{
		WorkspaceID: wsID,
		Type:        providerType,
		Name:        name,
		BaseURL:     baseURL,
		Enabled:     true,
	}
	if plaintextKey != "" {
		envelope, err := secrets.Encrypt([]byte("01234567890123456789012345678901"), []byte(wsID), []byte(plaintextKey))
		if err != nil {
			t.Fatalf("failed to encrypt seed key: %v", err)
		}
		p.KeyCiphertext = envelope
		p.KeyHint = domain.GenerateKeyHint(plaintextKey)
	}
	if err := st.Providers().Create(context.Background(), p); err != nil {
		t.Fatalf("failed to seed provider: %v", err)
	}
	return p
}

func decodeVerifyDraftResult(t *testing.T, w *httptest.ResponseRecorder) (ok bool, errMsg string) {
	t.Helper()
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode verify-draft response: %v", err)
	}
	return res.OK, res.Error
}

// TestProviders_VerifyDraft_TypedKey covers the create-dialog case: the typed
// key is probed against the submitted type and base URL; provider-side
// failures come back as 200 {ok: false, error}, never a 5xx.
func TestProviders_VerifyDraft_TypedKey(t *testing.T) {
	mock := newDraftMockProvider(t)
	r, _, _ := newVerifyDraftRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})

	t.Run("valid typed key probes the provider and returns ok: true", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai-compatible","base_url":"`+mock.URL+`","key":"valid-mock-key"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if !ok || errMsg != "" {
			t.Errorf("expected {ok: true}, got ok=%v error=%q", ok, errMsg)
		}
	})

	t.Run("provider-side auth failure returns 200 ok: false with the error", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai-compatible","base_url":"`+mock.URL+`","key":"wrong-key"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (provider failure is ok: false, not a 5xx), body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if ok {
			t.Errorf("expected ok: false for rejected key")
		}
		if !strings.Contains(errMsg, "Invalid credentials from mock server") {
			t.Errorf("expected the provider error message, got %q", errMsg)
		}
	})
}

// TestProviders_VerifyDraft_StoredKey covers the edit-dialog case: key blank
// plus provider_id of a key-set config verifies the stored key against the
// SUBMITTED type and base URL (not the stored ones). The config is seeded as
// "openai" — a key-requiring type — because stored-key resolution applies
// only to those (a keyless-capable type skips resolution per design D2).
func TestProviders_VerifyDraft_StoredKey(t *testing.T) {
	mock := newDraftMockProvider(t)
	r, st, ws := newVerifyDraftRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})

	// Stored config points at a dead address with the valid mock key: a
	// success can only come from the submitted base_url + the stored key.
	seeded := seedDraftProvider(t, st, ws.ID, "openai", "Stored Key Config", "http://127.0.0.1:1", "valid-mock-key")

	w := doVerifyDraftJSON(r, `{"type":"openai","base_url":"`+mock.URL+`","provider_id":"`+seeded.ID+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
	ok, errMsg := decodeVerifyDraftResult(t, w)
	if !ok || errMsg != "" {
		t.Errorf("expected {ok: true} from stored key + submitted base_url, got ok=%v error=%q", ok, errMsg)
	}

	// Cross-tenant guard: a provider_id from another workspace must not
	// resolve (same treatment the row verify gives a missing provider).
	otherWS := &domain.Workspace{Slug: "verify-draft-other-ws", Name: "Verify Draft Other WS"}
	if err := st.Workspaces().Create(context.Background(), otherWS); err != nil {
		t.Fatalf("failed to create other workspace: %v", err)
	}
	foreign := seedDraftProvider(t, st, otherWS.ID, "openai", "Foreign Config", mock.URL, "valid-mock-key")

	w = doVerifyDraftJSON(r, `{"type":"openai","base_url":"`+mock.URL+`","provider_id":"`+foreign.ID+`"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant provider_id: status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
}

// TestProviders_VerifyDraft_NoCredential covers the 400 cases for a
// key-REQUIRING draft type ("openai"): no key and no config, a blank key with
// no config, and a keyless config. Keyless-capable types never hit these
// paths (they verify with an empty key — see TestProviders_VerifyDraft_Keyless).
func TestProviders_VerifyDraft_NoCredential(t *testing.T) {
	mock := newDraftMockProvider(t)
	r, st, ws := newVerifyDraftRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
	keyless := seedDraftProvider(t, st, ws.ID, "openai", "Keyless Config", mock.URL, "")

	t.Run("no key and no provider_id is 400", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai","base_url":"`+mock.URL+`"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("blank key and no provider_id is 400", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai","base_url":"`+mock.URL+`","key":"   "}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("keyless config is 400", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai","base_url":"`+mock.URL+`","provider_id":"`+keyless.ID+`"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown provider_id is 404 like the row verify", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai","base_url":"`+mock.URL+`","provider_id":"no-such-id"}`)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404, body = %s", w.Code, w.Body.String())
		}
	})
}

// TestProviders_VerifyDraft_Keyless covers keyless-capable draft types
// ("openai-compatible"): no key verifies immediately with an empty key
// (design D2), stored-key resolution never runs, and a 401 from an endpoint
// that does want auth stays a soft ok: false (design D5).
func TestProviders_VerifyDraft_Keyless(t *testing.T) {
	authMock := newDraftMockProvider(t)
	openMock := newKeylessMockProvider(t)
	r, st, ws := newVerifyDraftRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})

	// A KEY-SET config: if the draft ever resolved its stored key, the probe
	// against the auth mock would succeed — ok: false proves it was skipped.
	keySet := seedDraftProvider(t, st, ws.ID, "openai", "Key Set Config", authMock.URL, "valid-mock-key")

	t.Run("keyless draft with no key and no provider_id probes keyless", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai-compatible","base_url":"`+openMock.URL+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if !ok || errMsg != "" {
			t.Errorf("expected {ok: true} for a keyless probe, got ok=%v error=%q", ok, errMsg)
		}
	})

	t.Run("whitespace-only key is an empty key on a keyless type", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai-compatible","base_url":"`+openMock.URL+`","key":"   "}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (blank key must not 400 on a keyless type), body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if !ok || errMsg != "" {
			t.Errorf("expected the trimmed-empty key to probe keyless, got ok=%v error=%q", ok, errMsg)
		}
	})

	t.Run("401 from an auth-wanting endpoint is a soft ok: false", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai-compatible","base_url":"`+authMock.URL+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (provider failure is ok: false, not a 5xx), body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if ok {
			t.Errorf("expected ok: false for a keyless probe against an auth-wanting endpoint")
		}
		if !strings.Contains(errMsg, "Invalid credentials from mock server") {
			t.Errorf("expected the provider error message, got %q", errMsg)
		}
	})

	t.Run("keyless draft never consults a stored config even with provider_id", func(t *testing.T) {
		w := doVerifyDraftJSON(r, `{"type":"openai-compatible","base_url":"`+authMock.URL+`","provider_id":"`+keySet.ID+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if ok {
			t.Errorf("expected ok: false — resolving the stored key would have authenticated and contradicted the keyless type")
		}
		if !strings.Contains(errMsg, "Invalid credentials from mock server") {
			t.Errorf("expected the provider error message, got %q", errMsg)
		}
	})
}

// TestProviders_VerifyDraft_MemberForbidden: the Member role holds
// providers.read but not providers.write, so verify-draft is 403.
func TestProviders_VerifyDraft_MemberForbidden(t *testing.T) {
	mock := newDraftMockProvider(t)
	r, _, _ := newVerifyDraftRouter(t, &domain.Role{Name: domain.RoleMember, Permissions: domain.MemberPermissions})

	w := doVerifyDraftJSON(r, `{"type":"openai-compatible","base_url":"`+mock.URL+`","key":"valid-mock-key"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for member role, body = %s", w.Code, w.Body.String())
	}
}

// TestProviders_VerifyDraft_PersistsNothing asserts the probe is pure: the
// stored configs are byte-identical before and after both a successful and a
// failing draft verification.
func TestProviders_VerifyDraft_PersistsNothing(t *testing.T) {
	mock := newDraftMockProvider(t)
	r, st, ws := newVerifyDraftRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
	// "openai" (key-requiring) so the provider_id body below exercises the
	// stored-key resolution path.
	seeded := seedDraftProvider(t, st, ws.ID, "openai", "Untouched Config", "http://127.0.0.1:1", "valid-mock-key")

	snapshot := func(t *testing.T) ([]domain.ProviderConfig, *domain.ProviderConfig) {
		t.Helper()
		list, err := st.Providers().ListForWorkspace(context.Background(), ws.ID)
		if err != nil {
			t.Fatalf("failed to list providers: %v", err)
		}
		byID, err := st.Providers().ByID(context.Background(), ws.ID, seeded.ID)
		if err != nil {
			t.Fatalf("failed to read seeded provider: %v", err)
		}
		return list, byID
	}

	beforeList, beforeRow := snapshot(t)

	// A successful typed-key verify and a failing stored-key verify (the
	// submitted dead base_url) — success and failure must both write nothing.
	for _, body := range []string{
		`{"type":"openai-compatible","base_url":"` + mock.URL + `","key":"valid-mock-key"}`,
		`{"type":"openai","base_url":"http://127.0.0.1:1","provider_id":"` + seeded.ID + `"}`,
	} {
		w := doVerifyDraftJSON(r, body)
		if w.Code != http.StatusOK {
			t.Fatalf("verify-draft %s: status = %d, want 200, body = %s", body, w.Code, w.Body.String())
		}
	}

	afterList, afterRow := snapshot(t)

	if len(afterList) != len(beforeList) {
		t.Errorf("provider count changed: before = %d, after = %d", len(beforeList), len(afterList))
	}
	for i := range beforeList {
		found := false
		for j := range afterList {
			if afterList[j].ID == beforeList[i].ID {
				found = true
				if afterList[j].UpdatedAt != beforeList[i].UpdatedAt ||
					afterList[j].KeyCiphertext != beforeList[i].KeyCiphertext ||
					afterList[j].KeyHint != beforeList[i].KeyHint ||
					afterList[j].BaseURL != beforeList[i].BaseURL ||
					afterList[j].CatalogProvider != beforeList[i].CatalogProvider {
					t.Errorf("provider %s mutated by draft verify: before = %+v, after = %+v", beforeList[i].ID, beforeList[i], afterList[j])
				}
				break
			}
		}
		if !found {
			t.Errorf("provider %s missing after draft verify", beforeList[i].ID)
		}
	}
	if afterRow.KeyCiphertext != beforeRow.KeyCiphertext || afterRow.KeyHint != beforeRow.KeyHint || afterRow.UpdatedAt != beforeRow.UpdatedAt {
		t.Errorf("seeded row mutated by draft verify: before = %+v, after = %+v", beforeRow, afterRow)
	}
}
