package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// patchTool PATCHes one tool's workspace setting and returns the recorder.
func patchTool(t *testing.T, r *gin.Engine, key string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal patch payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/acme/tools/"+key, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// searchEntry decodes one entry of the web.search config view returned by a
// tools response body.
type searchEntry struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Provider    string         `json:"provider"`
	APIKey      *string        `json:"api_key"`
	APIKeyHint  string         `json:"api_key_hint"`
	BaseURL     *string        `json:"base_url"`
	ExtraFields map[string]any `json:"-"`
}

func searchConfigEntries(t *testing.T, body []byte) []searchEntry {
	t.Helper()
	var res struct {
		Tool struct {
			Config struct {
				Entries []searchEntry `json:"entries"`
			} `json:"config"`
		} `json:"tool"`
		Tools []struct {
			Key    string `json:"key"`
			Config struct {
				Entries []searchEntry `json:"entries"`
			} `json:"config"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	entries := res.Tool.Config.Entries
	if entries == nil {
		for _, tool := range res.Tools {
			if tool.Key == "web.search" {
				entries = tool.Config.Entries
			}
		}
	}
	return entries
}

// toolError is the decoded error envelope of a failed tool save.
type toolError struct {
	Error struct {
		Message string        `json:"message"`
		Details []ErrorDetail `json:"details"`
	} `json:"error"`
}

// ErrorDetail mirrors the handler's per-field error detail.
type ErrorDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// decodeToolError decodes a 422 error envelope from a response body.
func decodeToolError(t *testing.T, body []byte) toolError {
	t.Helper()
	var te toolError
	if err := json.Unmarshal(body, &te); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	return te
}

func TestTools_EnableWithoutConfigIs422(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// Enabling web.search with zero provider entries is 422: at least one
	// fully valid entry is required (design.md D9).
	rec := patchTool(t, r, "web.search", map[string]any{"enabled": true})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	te := decodeToolError(t, rec.Body.Bytes())
	if !strings.Contains(te.Error.Message, "at least one fully configured provider entry") {
		t.Errorf("422 must name the enable-gating requirement: %s", rec.Body.String())
	}

	// The failed save stored nothing: the list view reports web.search as
	// not configured with an empty stack.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/tools", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list after failed save: expected 200, got %d", rec.Code)
	}
	var view struct {
		Tools []struct {
			Key        string         `json:"key"`
			Configured bool           `json:"configured"`
			Config     map[string]any `json:"config"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, tool := range view.Tools {
		if tool.Key != "web.search" {
			continue
		}
		if tool.Configured {
			t.Error("web.search must not be configured after a failed enable")
		}
		if entries, _ := tool.Config["entries"].([]any); len(entries) != 0 {
			t.Errorf("web.search stack must be empty after a failed enable, got %v", tool.Config["entries"])
		}
	}
}

func TestTools_EntryMissingCredentialIs422(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// A new entry (no stored id to keep a credential by) for a key-requiring
	// provider without an api_key is 422 naming the offending entry.
	rec := patchTool(t, r, "web.search", map[string]any{
		"enabled": true,
		"config": map[string]any{
			"entries": []any{
				map[string]any{"name": "Tavily 1", "provider": "tavily"},
			},
		},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	te := decodeToolError(t, rec.Body.Bytes())
	want := `entry "Tavily 1": api_key is required for provider "tavily"`
	if te.Error.Message != want {
		t.Errorf("422 message = %q, want %q", te.Error.Message, want)
	}
	var located bool
	for _, d := range te.Error.Details {
		if d.Field == "entries[0].api_key" && d.Message == want {
			located = true
		}
	}
	if !located {
		t.Errorf("422 details must locate entries[0].api_key naming the entry: %s", rec.Body.String())
	}
}

func TestTools_DuplicateEntryNameIs422(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// Two entries whose names collide case-insensitively are 422 naming the
	// duplicate, even though both carry credentials.
	rec := patchTool(t, r, "web.search", map[string]any{
		"enabled": true,
		"config": map[string]any{
			"entries": []any{
				map[string]any{"name": "Tavily 1", "provider": "tavily", "api_key": "tvly-key-a"},
				map[string]any{"name": "tavily 1", "provider": "tavily", "api_key": "tvly-key-b"},
			},
		},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `duplicate entry name`) {
		t.Errorf("422 must name the duplicate: %s", rec.Body.String())
	}
}

func TestTools_EntryCapIs422(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// The platform sanity cap is 20 entries; 21 valid entries are 422 even
	// though each entry is individually well-formed.
	entries := make([]any, 0, 21)
	for i := 0; i < 21; i++ {
		entries = append(entries, map[string]any{
			"name":     fmt.Sprintf("Tavily %d", i+1),
			"provider": "tavily",
			"api_key":  fmt.Sprintf("tvly-key-%02d", i+1),
		})
	}
	rec := patchTool(t, r, "web.search", map[string]any{
		"enabled": true,
		"config":  map[string]any{"entries": entries},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "at most 20 provider entries (21 configured)") {
		t.Errorf("422 must name the cap: %s", rec.Body.String())
	}
}

func TestTools_SecretIsHintedNotEchoed(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// Configure web.search with a provider stack — the response must hint,
	// not echo, with the hint nested inside each entry.
	rec := patchTool(t, r, "web.search", map[string]any{
		"enabled": true,
		"config": map[string]any{
			"entries": []any{
				map[string]any{"name": "Tavily 1", "provider": "tavily", "api_key": "tvly-secret-1234"},
				map[string]any{"name": "Exa 1", "provider": "exa", "api_key": "exa-secret-abcd"},
			},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "tvly-secret-1234") || strings.Contains(rec.Body.String(), "exa-secret-abcd") {
		t.Error("secret must never be echoed")
	}
	entries := searchConfigEntries(t, rec.Body.Bytes())
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries in response, got %d: %s", len(entries), rec.Body.String())
	}
	for i, want := range []struct {
		name, provider, hint string
	}{
		{"Tavily 1", "tavily", "1234"},
		{"Exa 1", "exa", "abcd"},
	} {
		got := entries[i]
		if got.Name != want.name || got.Provider != want.provider {
			t.Errorf("entry %d = %+v, want name %q provider %q", i, got, want.name, want.provider)
		}
		if got.APIKey != nil {
			t.Errorf("entry %d must not carry an api_key field, got %v", i, *got.APIKey)
		}
		if got.APIKeyHint != want.hint {
			t.Errorf("entry %d hint = %q, want %q", i, got.APIKeyHint, want.hint)
		}
		if got.ID == "" {
			t.Errorf("entry %d must carry the server-assigned id", i)
		}
	}

	// Round-trip: echoing the read shape back (ids, empty credentials) keeps
	// the stored secrets — the hints survive unchanged.
	echoed := make([]any, 0, len(entries))
	for _, entry := range entries {
		echoed = append(echoed, map[string]any{
			"id":           entry.ID,
			"name":         entry.Name,
			"provider":     entry.Provider,
			"api_key":      "",
			"api_key_hint": entry.APIKeyHint,
		})
	}
	rec = patchTool(t, r, "web.search", map[string]any{
		"enabled": true,
		"config":  map[string]any{"entries": echoed},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("echo save: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "tvly-secret-1234") || strings.Contains(rec.Body.String(), "exa-secret-abcd") {
		t.Error("echoed save must not surface the stored secrets")
	}
	entries = searchConfigEntries(t, rec.Body.Bytes())
	if len(entries) != 2 {
		t.Fatalf("echo save: expected 2 entries, got %d", len(entries))
	}
	for i, want := range []string{"1234", "abcd"} {
		if entries[i].APIKeyHint != want {
			t.Errorf("echo save: entry %d hint = %q, want %q (stored secret lost)", i, entries[i].APIKeyHint, want)
		}
		if entries[i].APIKey != nil {
			t.Errorf("echo save: entry %d must not carry an api_key field", i)
		}
	}
}

// toolsToggleView decodes the per-tool key/enabled/toggleable state of a
// tools list response.
type toolsToggleView struct {
	Key        string `json:"key"`
	Enabled    bool   `json:"enabled"`
	Toggleable bool   `json:"toggleable"`
}

func listToolsToggleView(t *testing.T, r *gin.Engine) map[string]toolsToggleView {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/tools", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list tools: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tools []toolsToggleView `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	byKey := make(map[string]toolsToggleView, len(body.Tools))
	for _, tool := range body.Tools {
		byKey[tool.Key] = tool
	}
	return byKey
}

func TestTools_ListMarksAlwaysOnNonToggleable(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	byKey := listToolsToggleView(t, r)
	for _, key := range []string{"channel.post", "channel.history", "session.close"} {
		tool, present := byKey[key]
		if !present {
			t.Fatalf("%s missing from list", key)
		}
		if tool.Toggleable {
			t.Errorf("%s must not be toggleable", key)
		}
		if !tool.Enabled {
			t.Errorf("%s must default to enabled", key)
		}
	}
	for _, key := range []string{"ls", "web.search"} {
		if !byKey[key].Toggleable {
			t.Errorf("%s must be toggleable", key)
		}
	}
}

func TestTools_PatchEnabledOnAlwaysOnIs422(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// An enabled patch on an always-on tool is rejected, never ignored
	// (always-on-channel-tools D4).
	rec := patchTool(t, r, "channel.post", map[string]any{"enabled": false})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	te := decodeToolError(t, rec.Body.Bytes())
	if !strings.Contains(te.Error.Message, "always active") {
		t.Errorf("422 must explain the always-on policy: %s", rec.Body.String())
	}

	// The rejected patch stored nothing and the list still reads the tool
	// enabled and non-toggleable.
	byKey := listToolsToggleView(t, r)
	tool := byKey["channel.post"]
	if !tool.Enabled {
		t.Error("channel.post must read enabled after the rejected patch")
	}
	if tool.Toggleable {
		t.Error("channel.post must read non-toggleable after the rejected patch")
	}
}

func TestTools_PatchConfigOnlyOnAlwaysOnSucceeds(t *testing.T) {
	r, _, _ := newToolsTestEnv(t)

	// Config-only patches keep today's behavior: the upsert stores an
	// enabled=true row (config is nilled for non-configurable tools).
	rec := patchTool(t, r, "channel.post", map[string]any{"config": map[string]any{}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Errorf("config-only patch response must read enabled: %s", rec.Body.String())
	}
}
