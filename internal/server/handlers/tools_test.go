package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// newToolsTestEnv builds a gin engine with the tools handlers and a seeded
// workspace, using the fake store.
func newToolsTestEnv(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(nil, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	svc := agents.NewToolSettingsService(st.ToolSettings(), []byte("01234567890123456789012345678901"))
	h := handlers.NewToolSettingsHandlers(svc)

	r := gin.New()
	group := r.Group("/api/v1/workspaces/:ws")
	group.Use(func(c *gin.Context) {
		resolved, err := st.Workspaces().BySlug(nil, c.Param("ws"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Next()
	})
	group.GET("/tools", h.ListTools)
	group.PATCH("/tools/:key", h.PatchTool)
	return r, st, ws
}

func TestTools_ListReturnsCatalog(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/tools", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tools []struct {
			Key         string `json:"key"`
			Enabled     bool   `json:"enabled"`
			Configured  bool   `json:"configured"`
			IconKey     string `json:"icon_key"`
			DisplayName string `json:"display_name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byKey := map[string]bool{}
	for _, tool := range body.Tools {
		byKey[tool.Key] = true
		if !tool.Enabled {
			t.Errorf("tool %s must default to enabled", tool.Key)
		}
	}
	for _, want := range []string{"ls", "web.search", "browser", "execute", "web.fetch", "read_file"} {
		if !byKey[want] {
			t.Errorf("expected %s in catalog response", want)
		}
	}
	if byKey["browser.navigate"] {
		t.Error("catalog must expose the browser alias, not legacy members")
	}
}

func TestTools_PatchToggleAndConfigRoundTrip(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// Toggle a non-configurable tool.
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"enabled": false})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/tools/ls", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// The toggle is visible in the list.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/tools", nil))
	if !strings.Contains(rec.Body.String(), `"key":"ls"`) {
		t.Fatalf("ls missing from list: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Error("disabled toggle not reflected")
	}
}

func TestTools_PatchUnknownKeyIs400(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"enabled": false})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/tools/not.a.tool", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown tool, got %d", rec.Code)
	}
}

func TestTools_EnableWithoutConfigIs422(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// Enabling web.search with the tavily provider but no key is 422 and
	// names the missing field.
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{
		"enabled": true,
		"config":  map[string]any{"provider": "tavily"},
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/tools/web.search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "api_key") {
		t.Errorf("422 must name the missing field: %s", rec.Body.String())
	}
}

func TestTools_SecretIsHintedNotEchoed(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// Configure web.search with an API key — the response must hint, not echo.
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{
		"enabled": true,
		"config":  map[string]any{"provider": "tavily", "api_key": "tvly-secret-1234"},
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/tools/web.search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "tvly-secret-1234") {
		t.Error("secret must never be echoed")
	}
	if !strings.Contains(rec.Body.String(), "1234") {
		t.Errorf("expected last-4 hint in response: %s", rec.Body.String())
	}
}
