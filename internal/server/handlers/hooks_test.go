package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	agenthooks "github.com/oniharnantyo/onclaw/internal/agents/hooks"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// staticToolValues is a fixed workspace-visible toolset for match-count
// assertions (the real adapter is wired at the composition root).
type staticToolValues struct {
	tools []string
}

func (s staticToolValues) VisibleToolNames(context.Context, string) ([]string, error) {
	return s.tools, nil
}

// hookTestErrorDetail mirrors the handler's per-field error detail.
type hookTestErrorDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type hookTestError struct {
	Error struct {
		Code    string                `json:"code"`
		Message string                `json:"message"`
		Details []hookTestErrorDetail `json:"details"`
	} `json:"error"`
}

func decodeHookError(t *testing.T, body []byte) hookTestError {
	t.Helper()
	var e hookTestError
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	return e
}

// hookView is the decoded read view shared by every hooks response.
type hookView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Event       string `json:"event"`
	HandlerType string `json:"handler_type"`
	Matcher     string `json:"matcher"`
	If          string `json:"if,omitempty"`
	Config      struct {
		URL     string `json:"url"`
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Env     []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"env"`
	} `json:"config"`
	TimeoutMS int    `json:"timeout_ms"`
	Enabled   bool   `json:"enabled"`
	Source    string `json:"source"`
	Version   int    `json:"version"`
	Key       string `json:"key"`
	Position  int    `json:"position"`
}

type hookSaveResponse struct {
	Hook       hookView `json:"hook"`
	MatchCount struct {
		Matched int `json:"matched"`
		Of      int `json:"of"`
	} `json:"match_count"`
}

type hookListResponse struct {
	Instance  []hookView `json:"instance"`
	Hooks     []hookView `json:"hooks"`
	Workspace []hookView `json:"workspace"`
	Agent     []hookView `json:"agent"`
	Builtin   []hookView `json:"builtin"`
	Managed   []hookView `json:"managed"`
}

const testEncKey = "01234567890123456789012345678901"

// newHooksTestEnv builds a gin engine with the hooks handlers over the fake
// store: workspace-scoped routes under /api/v1/workspaces/:ws (with a
// workspace-context stub standing in for the auth middleware) and the admin
// instance surface under /api/v1/admin.
func newHooksTestEnv(t *testing.T, commandEnabled bool) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(nil, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	key := []byte(testEncKey)
	registry := agenthooks.NewRegistry(
		agenthooks.WithEncryptionKey(key),
		// The dry-run tests point webhooks at httptest loopback servers;
		// production registries keep the SSRF guard on.
		agenthooks.WithHTTPAllowPrivate(true),
		agenthooks.WithCommandEnabled(commandEnabled),
	)
	toolValues := staticToolValues{tools: []string{
		"web.fetch", "web.search", "shell.run",
		"browser.snapshot", "browser.click",
		"mcp__github__create_issue",
	}}
	h := handlers.NewHookHandlers(st.Hooks(), st.Agents(), st.WorkspaceMCPServers(), registry, toolValues, key, commandEnabled)

	r := gin.New()

	wsGroup := r.Group("/api/v1/workspaces/:ws")
	wsGroup.Use(func(c *gin.Context) {
		resolved, err := st.Workspaces().BySlug(nil, c.Param("ws"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Next()
	})
	{
		wsGroup.GET("/hooks", h.ListWorkspaceHooks)
		wsGroup.POST("/hooks", h.CreateWorkspaceHook)
		wsGroup.POST("/hooks/reorder", h.ReorderWorkspaceHooks)
		wsGroup.POST("/hooks/test", h.TestWorkspaceHook)
		wsGroup.GET("/hooks/executions", h.ListWorkspaceHookExecutions)
		wsGroup.GET("/hooks/:id/executions", h.ListWorkspaceHookExecutions)
		wsGroup.PATCH("/hooks/:id", h.PatchWorkspaceHook)
		wsGroup.DELETE("/hooks/:id", h.DeleteWorkspaceHook)

		wsGroup.GET("/agents/:agent/hooks", h.ListAgentLevelHooks)
		wsGroup.POST("/agents/:agent/hooks", h.CreateAgentHook)
		wsGroup.POST("/agents/:agent/hooks/reorder", h.ReorderAgentHooks)
		wsGroup.PATCH("/agents/:agent/hooks/:id", h.PatchAgentHook)
		wsGroup.DELETE("/agents/:agent/hooks/:id", h.DeleteAgentHook)
	}

	admin := r.Group("/api/v1/admin")
	{
		admin.GET("/hooks", h.AdminListHooks)
		admin.POST("/hooks", h.AdminCreateHook)
		admin.POST("/hooks/reorder", h.AdminReorderHooks)
		admin.PATCH("/hooks/:id", h.AdminPatchHook)
		admin.DELETE("/hooks/:id", h.AdminDeleteHook)
	}
	return r, st, ws
}

func doHookJSON(t *testing.T, r *gin.Engine, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if payload == nil {
		reader = strings.NewReader("")
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// httpHookPayload builds an http-handler create body pointing at ts.URL.
func httpHookPayload(ts *httptest.Server, headerValue string) map[string]any {
	return map[string]any{
		"name":         "policy gate",
		"event":        "pre_tool_use",
		"matcher":      "web.fetch",
		"handler_type": "http",
		"config": map[string]any{
			"url": ts.URL + "/hook",
			"headers": []any{
				map[string]any{"name": "Authorization", "value": headerValue},
			},
		},
	}
}

func TestHooks_WorkspaceCRUDMatchCountAndSecretMasking(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create: 201 with the match count (1 of the 6 visible tools) and a
	// masked secret.
	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", httpHookPayload(ts, "Bearer tok-abcd"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "tok-abcd") || strings.Contains(rec.Body.String(), "v1:") {
		t.Fatalf("create response leaked the secret: %s", rec.Body.String())
	}
	var saved hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if saved.Hook.ID == "" {
		t.Fatal("create response carries no hook id")
	}
	if saved.MatchCount.Matched != 1 || saved.MatchCount.Of != 6 {
		t.Errorf("match count = %d of %d, want 1 of 6", saved.MatchCount.Matched, saved.MatchCount.Of)
	}
	if got := saved.Hook.Config.Headers[0].Value; got != "abcd" {
		t.Errorf("masked header value = %q, want the last-4 hint %q", got, "abcd")
	}
	id := saved.Hook.ID

	// List: read-only empty instance section plus the workspace hook.
	rec = doHookJSON(t, r, http.MethodGet, "/api/v1/workspaces/acme/hooks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list hookListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Instance) != 0 || len(list.Hooks) != 1 {
		t.Fatalf("list = %d instance / %d hooks, want 0 / 1", len(list.Instance), len(list.Hooks))
	}

	// Patch with the hint echoed back: the stored secret must survive —
	// proven by a dry run whose webhook receives the ORIGINAL header value.
	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/hooks/"+id, map[string]any{
		"name": "policy gate renamed",
		"config": map[string]any{
			"url": ts.URL + "/hook",
			"headers": []any{
				map[string]any{"name": "Authorization", "value": "abcd"},
			},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("hint-echo patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "tok-abcd") {
		t.Fatalf("patch response leaked the secret: %s", rec.Body.String())
	}

	var received string
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received = req.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok")) // non-JSON 2xx = allow
	}))
	defer echo.Close()
	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/hooks/"+id, map[string]any{
		"config": map[string]any{
			"url": echo.URL,
			"headers": []any{
				map[string]any{"name": "Authorization", "value": "abcd"},
			},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("url patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "policy gate renamed",
		"event":        "pre_tool_use",
		"matcher":      "web.fetch",
		"handler_type": "http",
		"id":           id,
		"config": map[string]any{
			"url": echo.URL,
			"headers": []any{
				map[string]any{"name": "Authorization", "value": "abcd"},
			},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("dry run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if received != "Bearer tok-abcd" {
		t.Errorf("webhook received Authorization %q, want the kept stored secret", received)
	}

	// Replacing the value re-seals: the new hint shows, the old secret is gone.
	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/hooks/"+id, map[string]any{
		"config": map[string]any{
			"url": echo.URL,
			"headers": []any{
				map[string]any{"name": "Authorization", "value": "Bearer fresh-4321"},
			},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("replace patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "fresh-4321") || strings.Contains(rec.Body.String(), "tok-abcd") {
		t.Fatalf("replace patch leaked a secret: %s", rec.Body.String())
	}
	var patched hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if got := patched.Hook.Config.Headers[0].Value; got != "4321" {
		t.Errorf("replaced header hint = %q, want %q", got, "4321")
	}

	// Unknown id: 404.
	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/hooks/nope", map[string]any{"name": "x"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("patch unknown id: expected 404, got %d", rec.Code)
	}

	// Delete: 204, then 404 on further patch.
	rec = doHookJSON(t, r, http.MethodDelete, "/api/v1/workspaces/acme/hooks/"+id, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete: expected 204, got %d", rec.Code)
	}
	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/hooks/"+id, map[string]any{"name": "x"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("patch deleted id: expected 404, got %d", rec.Code)
	}
}

func TestHooks_CreateValidation422FieldDetails(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
		"name":         "broken",
		"event":        "pre_tool_use",
		"matcher":      "(",
		"handler_type": "http",
		"config":       map[string]any{"url": "https://example.com/hook"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	e := decodeHookError(t, rec.Body.Bytes())
	if len(e.Error.Details) == 0 || e.Error.Details[0].Field != "matcher" {
		t.Errorf("422 details must field matcher, got %+v", e.Error.Details)
	}

	// Unknown event is a fielded 422 too.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
		"name":         "broken",
		"event":        "run_paused",
		"handler_type": "http",
		"config":       map[string]any{"url": "https://example.com/hook"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown event: expected 422, got %d", rec.Code)
	}

	// No row is persisted by a failed save.
	rec = doHookJSON(t, r, http.MethodGet, "/api/v1/workspaces/acme/hooks", nil)
	var list hookListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Hooks) != 0 {
		t.Errorf("failed saves must not persist rows, got %d", len(list.Hooks))
	}
}

func TestHooks_DuplicateNameIs409(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	payload := map[string]any{
		"name":         "dup",
		"event":        "run_finished",
		"handler_type": "http",
		"config":       map[string]any{"url": "https://example.com/hook"},
	}
	if rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", payload); rec.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", payload)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHooks_CommandDisabledIs422OnSave(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, false) // kill switch off

	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
		"name":         "script gate",
		"event":        "pre_tool_use",
		"handler_type": "command",
		"config":       map[string]any{"command": "/bin/echo"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	e := decodeHookError(t, rec.Body.Bytes())
	if len(e.Error.Details) == 0 || e.Error.Details[0].Field != "handler_type" {
		t.Errorf("422 details must field handler_type, got %+v", e.Error.Details)
	}
}

func TestHooks_ReorderSetsListOrder(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	create := func(name string) string {
		rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
			"name":         name,
			"event":        "run_finished",
			"handler_type": "http",
			"config":       map[string]any{"url": "https://example.com/hook"},
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d: %s", name, rec.Code, rec.Body.String())
		}
		var saved hookSaveResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return saved.Hook.ID
	}
	first, second := create("first"), create("second")

	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/reorder", map[string]any{"ids": []string{second, first}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reorder: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doHookJSON(t, r, http.MethodGet, "/api/v1/workspaces/acme/hooks", nil)
	var list hookListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Hooks) != 2 || list.Hooks[0].Name != "second" || list.Hooks[1].Name != "first" {
		t.Fatalf("order = [%s, %s], want [second first]", list.Hooks[0].Name, list.Hooks[1].Name)
	}
}

func TestHooks_ExecutionHistorySurvivesDeletion(t *testing.T) {
	r, st, ws := newHooksTestEnv(t, true)
	ctx := context.Background()

	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
		"name":         "notify",
		"event":        "run_finished",
		"handler_type": "http",
		"config":       map[string]any{"url": "https://example.com/hook"},
	})
	var saved hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id := saved.Hook.ID

	record := func() {
		err := st.Hooks().RecordHookExecution(ctx, &domain.HookExecution{
			HookID:      &id,
			HookName:    "notify",
			HookLevel:   domain.HookLevelWorkspace,
			WorkspaceID: ws.ID,
			Event:       domain.HookEventRunFinished,
			Decision:    "allow",
			DurationMS:  3,
			Origin:      "user",
			CreatedAt:   time.Now(),
		})
		if err != nil {
			t.Fatalf("record execution: %v", err)
		}
	}
	record()

	rec = doHookJSON(t, r, http.MethodGet, "/api/v1/workspaces/acme/hooks/"+id+"/executions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("executions: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Executions []domain.HookExecution `json:"executions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode executions: %v", err)
	}
	if len(list.Executions) != 1 {
		t.Fatalf("executions = %d, want 1", len(list.Executions))
	}

	// History survives deletion (D16): the endpoint must not 404 and the
	// records remain listed under the denormalized name.
	if rec := doHookJSON(t, r, http.MethodDelete, "/api/v1/workspaces/acme/hooks/"+id, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
	rec = doHookJSON(t, r, http.MethodGet, "/api/v1/workspaces/acme/hooks/executions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("executions after delete: expected 200, got %d", rec.Code)
	}
	list.Executions = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode executions after delete: %v", err)
	}
	if len(list.Executions) != 1 || list.Executions[0].HookName != "notify" {
		t.Fatalf("history did not survive deletion: %+v", list.Executions)
	}
}

func TestHooks_TestDryRunCommandDecisionTable(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	// Exit 0: allow with the exit code in the detail.
	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "echo gate",
		"event":        "pre_tool_use",
		"handler_type": "command",
		"config":       map[string]any{"command": "/bin/echo", "args": []string{"{}"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("echo dry run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Decision   string         `json:"decision"`
		Reason     string         `json:"reason"`
		DurationMS int64          `json:"duration_ms"`
		Detail     map[string]any `json:"detail"`
		Error      string         `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode dry run: %v", err)
	}
	if result.Decision != "allow" || result.Error != "" {
		t.Errorf("echo dry run = %q / %q, want allow / no error", result.Decision, result.Error)
	}
	if result.Detail["exit_code"] != float64(0) {
		t.Errorf("echo exit_code = %v, want 0", result.Detail["exit_code"])
	}
	if result.DurationMS < 0 {
		t.Errorf("duration_ms = %d, want >= 0", result.DurationMS)
	}

	// Exit 2 with stderr: block with the (trimmed) stderr reason.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "script gate",
		"event":        "pre_tool_use",
		"handler_type": "command",
		"config":       map[string]any{"command": "/bin/sh", "args": []string{"-c", "echo policy says no >&2; exit 2"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("exit-2 dry run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result = struct {
		Decision   string         `json:"decision"`
		Reason     string         `json:"reason"`
		DurationMS int64          `json:"duration_ms"`
		Detail     map[string]any `json:"detail"`
		Error      string         `json:"error"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode exit-2 dry run: %v", err)
	}
	if result.Decision != "block" || result.Reason != "policy says no" {
		t.Errorf("exit-2 dry run = %q / %q, want block / \"policy says no\"", result.Decision, result.Reason)
	}
}

func TestHooks_TestDryRunHTTPBlockNonJSONAndOverrides(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	// 2xx decision body: honored, and the synthetic event (overrides +
	// unique delivery id) is what actually reaches the webhook.
	var seenBody map[string]any
	var seenDelivery string
	blocker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw := make([]byte, 4096)
		n, _ := req.Body.Read(raw)
		seenBody = map[string]any{}
		_ = json.Unmarshal(raw[:n], &seenBody)
		seenDelivery = req.Header.Get("X-Onclaw-Delivery")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"block","reason":"not on my watch"}`))
	}))
	defer blocker.Close()

	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "web gate",
		"event":        "post_tool_use", // overridden below
		"handler_type": "http",
		"config":       map[string]any{"url": blocker.URL},
		"overrides": map[string]any{
			"event":     "pre_tool_use",
			"tool_name": "web.fetch",
			"tool_args": `{"query":"x"}`,
			"origin":    "cron",
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("http dry run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Decision string         `json:"decision"`
		Reason   string         `json:"reason"`
		Detail   map[string]any `json:"detail"`
		Error    string         `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode http dry run: %v", err)
	}
	if result.Decision != "block" || result.Reason != "not on my watch" {
		t.Errorf("http dry run = %q / %q, want block / reason", result.Decision, result.Reason)
	}
	if result.Detail["http_status"] != float64(200) {
		t.Errorf("http_status = %v, want 200", result.Detail["http_status"])
	}
	if seenDelivery == "" {
		t.Error("dry run must send a unique delivery id header")
	}
	if seenBody["event"] != "pre_tool_use" || seenBody["origin"] != "cron" {
		t.Errorf("webhook event overrides ignored: event %v origin %v", seenBody["event"], seenBody["origin"])
	}
	if tool, ok := seenBody["tool"].(map[string]any); !ok || tool["name"] != "web.fetch" {
		t.Errorf("webhook tool override ignored: %v", seenBody["tool"])
	}

	// Non-JSON 2xx body means allow.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	}))
	defer plain.Close()
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "web gate",
		"event":        "post_tool_use",
		"handler_type": "http",
		"config":       map[string]any{"url": plain.URL},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("non-JSON dry run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result = struct {
		Decision string         `json:"decision"`
		Reason   string         `json:"reason"`
		Detail   map[string]any `json:"detail"`
		Error    string         `json:"error"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode non-JSON dry run: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("non-JSON 2xx dry run = %q, want allow", result.Decision)
	}

	// A failing endpoint is a failure result naming the error, still 200.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	deadURL := dead.URL
	dead.Close()
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "web gate",
		"event":        "post_tool_use",
		"handler_type": "http",
		"config":       map[string]any{"url": deadURL},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("dead-endpoint dry run: expected 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode dead-endpoint dry run: %v", err)
	}
	if result.Decision != "failure" || result.Error == "" {
		t.Errorf("dead-endpoint dry run = %q / %q, want failure with an error", result.Decision, result.Error)
	}

	// The payload itself is still validated (prompt hooks demand a narrowing
	// matcher) before any execution.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "evaluator",
		"event":        "pre_tool_use",
		"handler_type": "prompt",
		"config":       map[string]any{"provider": "openai", "model": "gpt-4"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("match-all prompt dry run: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHooks_TestDryRunWritesNoAuditRow(t *testing.T) {
	r, st, ws := newHooksTestEnv(t, true)
	ctx := context.Background()

	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
		"name":         "echo gate",
		"event":        "pre_tool_use",
		"handler_type": "command",
		"config":       map[string]any{"command": "/bin/echo"},
	})
	var saved hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	executions := func() int {
		rows, err := st.Hooks().ListHookExecutions(ctx, ws.ID, &saved.Hook.ID, 0)
		if err != nil {
			t.Fatalf("list executions: %v", err)
		}
		return len(rows)
	}
	before := executions()

	// Several dry runs (allow and block alike) must not record anything.
	for _, cfg := range []map[string]any{
		{"command": "/bin/echo", "args": []string{"{}"}},
		{"command": "/bin/sh", "args": []string{"-c", "exit 2"}},
	} {
		rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
			"name":         "echo gate",
			"event":        "pre_tool_use",
			"handler_type": "command",
			"config":       cfg,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("dry run: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	if after := executions(); after != before {
		t.Errorf("dry runs changed the audit log: %d -> %d executions", before, after)
	}
}

func seedHookTestAgent(t *testing.T, st store.Store, ws *domain.Workspace, slug, providerID string) *domain.Agent {
	t.Helper()
	if providerID == "" {
		prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "Seed Provider"}
		if err := st.Providers().Create(nil, prov); err != nil {
			t.Fatalf("seed provider: %v", err)
		}
		providerID = prov.ID
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: slug, Name: slug, ProviderID: providerID, Model: "gpt-4"}
	if err := st.Agents().Create(nil, agent); err != nil {
		t.Fatalf("seed agent %s: %v", slug, err)
	}
	return agent
}

func TestHooks_AgentLevelCRUDAndPrivacy(t *testing.T) {
	r, st, ws := newHooksTestEnv(t, true)
	atlas := seedHookTestAgent(t, st, ws, "atlas", "")
	seedHookTestAgent(t, st, ws, "beacon", "")

	// The modal view: three sections, all empty initially.
	rec := doHookJSON(t, r, http.MethodGet, "/api/v1/workspaces/acme/agents/atlas/hooks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list hookListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode agent list: %v", err)
	}
	if list.Instance == nil || list.Workspace == nil || list.Agent == nil {
		t.Fatalf("agent list must carry all three sections: %+v", list)
	}
	if len(list.Instance)+len(list.Workspace)+len(list.Agent) != 0 {
		t.Fatalf("expected empty sections, got %d/%d/%d", len(list.Instance), len(list.Workspace), len(list.Agent))
	}

	// Create an agent-private command hook.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/hooks", map[string]any{
		"name":         "atlas gate",
		"event":        "pre_tool_use",
		"matcher":      "shell.*",
		"handler_type": "command",
		"config":       map[string]any{"command": "/bin/echo"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var saved hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode agent create: %v", err)
	}
	if saved.MatchCount.Matched != 1 || saved.MatchCount.Of != 6 {
		t.Errorf("agent match count = %d of %d, want 1 of 6", saved.MatchCount.Matched, saved.MatchCount.Of)
	}
	if saved.Hook.Matcher != "shell.*" {
		t.Errorf("agent matcher echo = %q, want %q", saved.Hook.Matcher, "shell.*")
	}

	// The hook is private to atlas: beacon's list stays empty (D13).
	rec = doHookJSON(t, r, http.MethodGet, "/api/v1/workspaces/acme/agents/beacon/hooks", nil)
	list = hookListResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode beacon list: %v", err)
	}
	if len(list.Agent) != 0 {
		t.Errorf("agent hooks leaked across agents: %+v", list.Agent)
	}

	// Patch and delete by id under the agent scope.
	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/agents/atlas/hooks/"+saved.Hook.ID, map[string]any{"enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("agent patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/agents/atlas/hooks/nope", map[string]any{"enabled": false})
	if rec.Code != http.StatusNotFound {
		t.Errorf("agent patch unknown id: expected 404, got %d", rec.Code)
	}
	rec = doHookJSON(t, r, http.MethodDelete, "/api/v1/workspaces/acme/agents/atlas/hooks/"+saved.Hook.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("agent delete: expected 204, got %d", rec.Code)
	}
	if err := st.Agents().Delete(nil, ws.ID, atlas.ID); err != nil {
		t.Fatalf("cleanup agent: %v", err)
	}
}

func TestHooks_AdminManagedCRUDAndBuiltinListing(t *testing.T) {
	r, st, _ := newHooksTestEnv(t, true)

	// Empty listing to start.
	rec := doHookJSON(t, r, http.MethodGet, "/api/v1/admin/hooks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: expected 200, got %d", rec.Code)
	}
	var list hookListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode admin list: %v", err)
	}
	if len(list.Builtin)+len(list.Managed) != 0 && (list.Hooks != nil || list.Agent != nil) {
		t.Fatalf("admin list shape wrong: %+v", list)
	}

	// Create a managed row: source and version are server-owned.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/admin/hooks", map[string]any{
		"key":          "org-policy-gate",
		"name":         "Org Policy Gate",
		"event":        "pre_tool_use",
		"handler_type": "http",
		"config":       map[string]any{"url": "https://policy.example.com/hook"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var saved hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode admin create: %v", err)
	}
	if saved.Hook.Source != "managed" || saved.Hook.Version != 1 || saved.Hook.Key != "org-policy-gate" {
		t.Errorf("managed row = source %q version %d key %q", saved.Hook.Source, saved.Hook.Version, saved.Hook.Key)
	}

	// Duplicate key: 409.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/admin/hooks", map[string]any{
		"key": "org-policy-gate", "name": "Dup", "event": "run_finished", "handler_type": "http",
		"config": map[string]any{"url": "https://x.example.com"},
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate key: expected 409, got %d", rec.Code)
	}

	// Patch the managed row.
	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/admin/hooks/"+saved.Hook.ID, map[string]any{"name": "Renamed Gate"})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Seed a builtin row through the sync pipeline port; writes must reject.
	builtin := &domain.InstanceHook{
		Key: "shipped-guard", Source: domain.HookSourceBuiltin, Version: 1,
		HookBase: domain.HookBase{
			Name: "Shipped Guard", Event: domain.HookEventRunFinished,
			Matcher:     "",
			HandlerType: domain.HookHandlerHTTP, Config: json.RawMessage(`{"url":"https://shipped.example.com"}`),
			TimeoutMS: domain.DefaultHookTimeoutMS, Enabled: true,
		},
	}
	if err := st.Hooks().UpsertBuiltinHook(nil, builtin); err != nil {
		t.Fatalf("seed builtin: %v", err)
	}

	rec = doHookJSON(t, r, http.MethodGet, "/api/v1/admin/hooks", nil)
	list = hookListResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode admin list 2: %v", err)
	}
	if len(list.Builtin) != 1 || len(list.Managed) != 1 {
		t.Fatalf("admin list = %d builtin / %d managed, want 1 / 1", len(list.Builtin), len(list.Managed))
	}
	if list.Builtin[0].Key != "shipped-guard" {
		t.Errorf("builtin listing carries the wrong row: %+v", list.Builtin[0])
	}

	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/admin/hooks/"+builtin.ID, map[string]any{"name": "Hijack"})
	if rec.Code != http.StatusConflict {
		t.Errorf("builtin patch: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doHookJSON(t, r, http.MethodDelete, "/api/v1/admin/hooks/"+builtin.ID, nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("builtin delete: expected 409, got %d", rec.Code)
	}

	// Managed delete works; unknown ids 404.
	rec = doHookJSON(t, r, http.MethodDelete, "/api/v1/admin/hooks/"+saved.Hook.ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("managed delete: expected 204, got %d", rec.Code)
	}
	rec = doHookJSON(t, r, http.MethodDelete, "/api/v1/admin/hooks/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("admin delete unknown: expected 404, got %d", rec.Code)
	}
}

func TestHooks_AdminInstanceValidation422(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	// mcp_tool hooks skip the MCP existence check at the instance level
	// (workspace-unscoped), but the rest of validation still applies — here a
	// regex-tier matcher string that does not compile, with the renamed
	// Claude Code config keys server/tool.
	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/admin/hooks", map[string]any{
		"key":          "bad-regex",
		"name":         "Bad Regex",
		"event":        "pre_tool_use",
		"matcher":      "[",
		"handler_type": "mcp_tool",
		"config":       map[string]any{"server": "any", "tool": "query"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("admin validation: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	e := decodeHookError(t, rec.Body.Bytes())
	if len(e.Error.Details) == 0 || e.Error.Details[0].Field != "matcher" {
		t.Errorf("admin 422 details must field matcher, got %+v", e.Error.Details)
	}
}

// TestHooks_StringMatcherTiersAndMatchCounts exercises the Claude Code
// matcher string's three tiers at save time: absent/"" and "*" are match-all,
// a "|" list counts exact and trailing-".*" family entries, and anything else
// is an unanchored regex. Counts run over the six statically visible tools.
func TestHooks_StringMatcherTiersAndMatchCounts(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	cases := []struct {
		label       string
		matcher     string
		wantMatched int
	}{
		{"all absent", "", 6},
		{"all star", "*", 6},
		{"list tier", "web.fetch|shell.*", 2},
		{"regex tier", `^web\.`, 2},
	}
	for _, tc := range cases {
		rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
			"name":         tc.label,
			"event":        "pre_tool_use",
			"matcher":      tc.matcher,
			"handler_type": "http",
			"config":       map[string]any{"url": "https://example.com/hook"},
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s: expected 201, got %d: %s", tc.label, rec.Code, rec.Body.String())
		}
		var saved hookSaveResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
			t.Fatalf("%s: decode create: %v", tc.label, err)
		}
		if saved.MatchCount.Matched != tc.wantMatched || saved.MatchCount.Of != 6 {
			t.Errorf("%s: match count = %d of %d, want %d of 6", tc.label, saved.MatchCount.Matched, saved.MatchCount.Of, tc.wantMatched)
		}
		if saved.Hook.Matcher != tc.matcher {
			t.Errorf("%s: stored matcher = %q, want %q", tc.label, saved.Hook.Matcher, tc.matcher)
		}
	}
}

// TestHooks_IfConditionRoundTrip pins the if wire field: it survives the
// create response verbatim, a PATCH with an explicit "" clears it, and a
// later PATCH sets it again.
func TestHooks_IfConditionRoundTrip(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
		"name":         "if gate",
		"event":        "pre_tool_use",
		"matcher":      "web.fetch",
		"if":           "web.fetch(query)",
		"handler_type": "http",
		"config":       map[string]any{"url": "https://example.com/hook"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var saved hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if saved.Hook.If != "web.fetch(query)" {
		t.Errorf("stored if = %q, want %q", saved.Hook.If, "web.fetch(query)")
	}
	id := saved.Hook.ID

	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/hooks/"+id, map[string]any{"if": ""})
	if rec.Code != http.StatusOK {
		t.Fatalf("clear patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var cleared hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("decode clear patch: %v", err)
	}
	if cleared.Hook.If != "" {
		t.Errorf("cleared if = %q, want empty", cleared.Hook.If)
	}

	rec = doHookJSON(t, r, http.MethodPatch, "/api/v1/workspaces/acme/hooks/"+id, map[string]any{"if": "web.fetch(id)"})
	if rec.Code != http.StatusOK {
		t.Fatalf("set patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var reset hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &reset); err != nil {
		t.Fatalf("decode set patch: %v", err)
	}
	if reset.Hook.If != "web.fetch(id)" {
		t.Errorf("reset if = %q, want %q", reset.Hook.If, "web.fetch(id)")
	}
}

// TestHooks_MalformedMatcherAndIfAre422 checks that domain validation of the
// string matcher tiers and the if condition surfaces as fielded 422s.
func TestHooks_MalformedMatcherAndIfAre422(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	cases := []struct {
		label     string
		payload   map[string]any
		wantField string
	}{
		{"matcher falls to the regex tier and does not compile", map[string]any{
			"name": "bad matcher", "event": "pre_tool_use", "matcher": "((",
			"handler_type": "http", "config": map[string]any{"url": "https://example.com/hook"},
		}, "matcher"},
		{"if without ToolName(pattern) syntax", map[string]any{
			"name": "bad if", "event": "pre_tool_use", "matcher": "web.fetch", "if": "nonsense",
			"handler_type": "http", "config": map[string]any{"url": "https://example.com/hook"},
		}, "if"},
		{"if with an un-compilable pattern", map[string]any{
			"name": "bad if pattern", "event": "post_tool_use", "matcher": "web.fetch", "if": "web.fetch([)",
			"handler_type": "http", "config": map[string]any{"url": "https://example.com/hook"},
		}, "if"},
		{"if on a non-tool event", map[string]any{
			"name": "if wrong event", "event": "run_finished", "if": "web.fetch(x)",
			"handler_type": "http", "config": map[string]any{"url": "https://example.com/hook"},
		}, "if"},
	}
	for _, tc := range cases {
		rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", tc.payload)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: expected 422, got %d: %s", tc.label, rec.Code, rec.Body.String())
			continue
		}
		e := decodeHookError(t, rec.Body.Bytes())
		if len(e.Error.Details) == 0 || e.Error.Details[0].Field != tc.wantField {
			t.Errorf("%s: 422 details must field %s, got %+v", tc.label, tc.wantField, e.Error.Details)
		}
	}

	// No row from any rejected save.
	rec := doHookJSON(t, r, http.MethodGet, "/api/v1/workspaces/acme/hooks", nil)
	var list hookListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Hooks) != 0 {
		t.Errorf("failed saves must not persist rows, got %d", len(list.Hooks))
	}
}

// TestHooks_ScriptSaveValidationAndDryRunConsole pins the script handler's
// REST surface (D22): a syntax-error script saves as a fielded 422 naming
// config.script with the first error's line/column in editor coordinates, a
// valid script saves with an EMPTY (match-all) matcher — script hooks carry
// no matcher requirement at all, unlike prompt hooks — and the dry-run
// surfaces the captured console output beside the decision while a
// non-script handler reports no console_lines.
func TestHooks_ScriptSaveValidationAndDryRunConsole(t *testing.T) {
	r, _, _ := newHooksTestEnv(t, true)

	// A syntax error compiles to a fielded 422 without executing anything.
	rec := doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
		"name":         "broken script",
		"event":        "pre_tool_use",
		"handler_type": "script",
		"config":       map[string]any{"script": "(function(input){\nconst bad = ;\n})"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("script save: expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	e := decodeHookError(t, rec.Body.Bytes())
	if len(e.Error.Details) == 0 || e.Error.Details[0].Field != "config.script" {
		t.Fatalf("422 details must field config.script, got %+v", e.Error.Details)
	}
	if msg := e.Error.Details[0].Message; !strings.Contains(msg, "line") || !strings.Contains(msg, "column") {
		t.Errorf("script 422 message must name line and column, got %q", msg)
	}

	// A valid script with an empty (match-all) matcher saves — the D22
	// exemption from the prompt handler's matcher requirement — and the
	// match count reads the empty matcher as selecting every visible tool.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks", map[string]any{
		"name":         "script gate",
		"event":        "pre_tool_use",
		"matcher":      "",
		"handler_type": "script",
		"config":       map[string]any{"script": "(function(input){ return null; })"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid script save: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var saved hookSaveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode script create: %v", err)
	}
	if saved.MatchCount.Matched != 6 || saved.MatchCount.Of != 6 {
		t.Errorf("match-all script match count = %d of %d, want 6 of 6", saved.MatchCount.Matched, saved.MatchCount.Of)
	}

	// Dry run: the block return wins and the captured console output rides
	// the response detail.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "script gate",
		"event":        "pre_tool_use",
		"handler_type": "script",
		"config":       map[string]any{"script": `(function(input){ console.log("a"); return { decision: "block", reason: "script says no" }; })`},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("script dry run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Decision string         `json:"decision"`
		Reason   string         `json:"reason"`
		Detail   map[string]any `json:"detail"`
		Error    string         `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode script dry run: %v", err)
	}
	if result.Decision != "block" || result.Reason != "script says no" || result.Error != "" {
		t.Errorf("script dry run = %q / %q / %q, want block / reason / no error", result.Decision, result.Reason, result.Error)
	}
	if lines, ok := result.Detail["console_lines"].([]any); !ok || len(lines) != 1 || lines[0] != "a" {
		t.Errorf("script dry run console_lines = %v, want [\"a\"]", result.Detail["console_lines"])
	}

	// A command dry run carries no console_lines — the key is script-only.
	rec = doHookJSON(t, r, http.MethodPost, "/api/v1/workspaces/acme/hooks/test", map[string]any{
		"name":         "echo gate",
		"event":        "pre_tool_use",
		"handler_type": "command",
		"config":       map[string]any{"command": "/bin/echo", "args": []string{"{}"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("command dry run: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result = struct {
		Decision string         `json:"decision"`
		Reason   string         `json:"reason"`
		Detail   map[string]any `json:"detail"`
		Error    string         `json:"error"`
	}{} // fresh map: unmarshal would otherwise merge into the script run's detail
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode command dry run: %v", err)
	}
	if _, present := result.Detail["console_lines"]; present {
		t.Errorf("command dry run must not carry console_lines: %v", result.Detail)
	}
}
