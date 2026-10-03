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
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// newRowVerifyRouter wires the stored-config verify endpoint against a fake
// store with the workspace and role bound, mirroring the production middleware
// chain for this route (workspace context + providers.write guard) — the same
// wiring newVerifyDraftRouter uses, with the row route.
func newRowVerifyRouter(t *testing.T, role *domain.Role) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{Slug: "row-verify-ws", Name: "Row Verify WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	h := handlers.NewProviderHandlers(st.Providers(), st.Agents(), []byte("01234567890123456789012345678901"), providers.NewRegistry(), nil, st.ToolSettings())

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.RoleContextKey, role)
		c.Next()
	}, requireTestPermission(domain.ProvidersWrite))
	r.POST("/providers/:id/verify", h.VerifyProvider)
	return r, st, ws
}

func doRowVerifyJSON(r *gin.Engine, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/"+id+"/verify", nil)
	r.ServeHTTP(w, req)
	return w
}

// decodeRowVerifyErrorCode decodes the error envelope's code from a failed
// verify response (e.g. "invalid_request" for a keyless key-requiring row).
func decodeRowVerifyErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var res struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode verify error envelope: %v", err)
	}
	return res.Error.Code
}

// newSystemoneMockProvider plays the TypeSafe /v1/systemone decision
// endpoint: only the "valid-systemone-key" bearer authenticates, and success
// answers with the decision body shape (an answers map) the probe requires.
func newSystemoneMockProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer valid-systemone-key" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"answers": {"needs_memory": {"noul": 0.83}}, "usage": {"input": 3, "output": 1}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Invalid credentials from mock server"}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// TestProviders_Verify_Keyless covers the stored-config verify endpoint's
// type-conditional key rule (design D2): a keyless config of a keyless-capable
// type verifies with an empty key (decrypt skipped), a 401 from an endpoint
// that does want auth stays a soft ok: false (design D5), and a keyless config
// of a key-requiring type is still 400 invalid_request.
func TestProviders_Verify_Keyless(t *testing.T) {
	authMock := newDraftMockProvider(t)
	openMock := newKeylessMockProvider(t)
	systemoneMock := newSystemoneMockProvider(t)

	t.Run("stored keyless openai-compatible config verifies keyless", func(t *testing.T) {
		r, st, ws := newRowVerifyRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
		seeded := seedDraftProvider(t, st, ws.ID, "openai-compatible", "Keyless Config", openMock.URL, "")

		w := doRowVerifyJSON(r, seeded.ID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if !ok || errMsg != "" {
			t.Errorf("expected {ok: true} for a keyless probe, got ok=%v error=%q", ok, errMsg)
		}
	})

	t.Run("stored keyless config against an auth-wanting endpoint is a soft ok: false", func(t *testing.T) {
		r, st, ws := newRowVerifyRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
		seeded := seedDraftProvider(t, st, ws.ID, "openai-compatible", "Keyless Config", authMock.URL, "")

		w := doRowVerifyJSON(r, seeded.ID)
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

	t.Run("stored keyless key-requiring config is 400 invalid_request", func(t *testing.T) {
		r, st, ws := newRowVerifyRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
		seeded := seedDraftProvider(t, st, ws.ID, "openai", "Keyless Key-Requiring Config", openMock.URL, "")

		w := doRowVerifyJSON(r, seeded.ID)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
		}
		if code := decodeRowVerifyErrorCode(t, w); code != "invalid_request" {
			t.Errorf("expected invalid_request code, got %q", code)
		}
	})

	t.Run("stored keyless typesafe config is 400 invalid_request", func(t *testing.T) {
		r, st, ws := newRowVerifyRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
		// The decision type requires a key like the named language types:
		// verifying a keyless decision config is invalid input, not a probe.
		seeded := seedDraftProvider(t, st, ws.ID, "typesafe", "Keyless Decision Config", systemoneMock.URL, "")

		w := doRowVerifyJSON(r, seeded.ID)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
		}
		if code := decodeRowVerifyErrorCode(t, w); code != "invalid_request" {
			t.Errorf("expected invalid_request code, got %q", code)
		}
	})

	t.Run("stored key-set openai config still verifies with its key", func(t *testing.T) {
		r, st, ws := newRowVerifyRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
		// The auth mock answers 200 only to the valid-mock-key bearer, so
		// ok: true proves the stored key was decrypted and sent.
		seeded := seedDraftProvider(t, st, ws.ID, "openai", "Key Set Config", authMock.URL, "valid-mock-key")

		w := doRowVerifyJSON(r, seeded.ID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if !ok || errMsg != "" {
			t.Errorf("expected {ok: true} from the stored key, got ok=%v error=%q", ok, errMsg)
		}
	})
}

// TestProviders_Verify_TypesafeProbe covers the decision provider's verify
// lane end-to-end through the stored-config endpoint: the probe POSTs the
// minimal one-noul-question request to the systemone endpoint with the
// decrypted stored key, a decision body answers ok: true, and a provider-side
// auth failure stays a soft ok: false — never a server 5xx.
func TestProviders_Verify_TypesafeProbe(t *testing.T) {
	systemoneMock := newSystemoneMockProvider(t)

	t.Run("stored typesafe config with a key verifies against the systemone endpoint", func(t *testing.T) {
		r, st, ws := newRowVerifyRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
		// The systemone mock answers 200 with an answers body only to the
		// valid-systemone-key bearer, so ok: true proves the probe shape and
		// the decrypted key both reached the endpoint.
		seeded := seedDraftProvider(t, st, ws.ID, "typesafe", "TypeSafe Production", systemoneMock.URL, "valid-systemone-key")

		w := doRowVerifyJSON(r, seeded.ID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if !ok || errMsg != "" {
			t.Errorf("expected {ok: true} for the decision probe, got ok=%v error=%q", ok, errMsg)
		}
	})

	t.Run("rejected decision key is a soft ok: false", func(t *testing.T) {
		r, st, ws := newRowVerifyRouter(t, &domain.Role{Name: domain.RoleOwner, Permissions: domain.OwnerPermissions})
		seeded := seedDraftProvider(t, st, ws.ID, "typesafe", "TypeSafe Bad Key", systemoneMock.URL, "wrong-systemone-key")

		w := doRowVerifyJSON(r, seeded.ID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (provider failure is ok: false, not a 5xx), body = %s", w.Code, w.Body.String())
		}
		ok, errMsg := decodeVerifyDraftResult(t, w)
		if ok {
			t.Errorf("expected ok: false for a rejected decision key")
		}
		if !strings.Contains(errMsg, "Invalid credentials from mock server") {
			t.Errorf("expected the provider error message, got %q", errMsg)
		}
	})
}
