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
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// catalogHintFixtureJSON carries the community-catalog provider ids the
// provider handler tests resolve against (mirrors real models.dev entries).
const catalogHintFixtureJSON = `{
  "zai-coding-plan": {
    "id": "zai-coding-plan",
    "name": "Z.ai Coding Plan",
    "models": {
      "glm-5.3-flash": {
        "id": "glm-5.3-flash",
        "name": "GLM-5.3-Flash",
        "reasoning": true,
        "tool_call": true,
        "limit": {"context": 200000, "output": 131072},
        "modalities": {"input": ["text", "image", "video", "pdf"], "output": ["text"]}
      },
      "glm-5.3": {
        "id": "glm-5.3",
        "name": "GLM-5.3",
        "attachment": false,
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]}
      }
    }
  }
}`

// newProviderHintRouter wires the provider endpoints against a fake store and
// a catalog service backed by the fixture server, with the workspace bound.
func newProviderHintRouter(t *testing.T) (*gin.Engine, *domain.Workspace, *httptest.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	catServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(catalogHintFixtureJSON))
	}))
	t.Cleanup(catServer.Close)

	mc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   t.TempDir(),
		CatalogURL: catServer.URL,
		TTL:        24 * time.Hour,
		Client:     catServer.Client(),
	})

	st := storefake.New()
	ws := &domain.Workspace{Slug: "prov-hint-ws", Name: "Provider Hint WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	h := handlers.NewProviderHandlers(st.Providers(), st.Agents(), []byte("01234567890123456789012345678901"), providers.NewRegistry(), mc, st.ToolSettings())

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.POST("/providers", h.CreateProvider)
	r.PATCH("/providers/:id", h.PatchProvider)
	r.GET("/providers/:id/models", h.GetProviderModels)
	r.POST("/providers/models/preview", h.ModelsPreview)
	return r, ws, catServer
}

func doProviderJSON(r *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// TestProviderHandlers_CatalogProviderValidation covers create/patch
// validation: known community-catalog ids are accepted and persisted, unknown
// ids are rejected, and the response echoes the stored hint plus the read-only
// host suggestion (empty for canonically mapped types).
func TestProviderHandlers_CatalogProviderValidation(t *testing.T) {
	r, _, _ := newProviderHintRouter(t)

	// 1. Known id accepted; response echoes hint and host suggestion.
	w := doProviderJSON(r, http.MethodPost, "/providers", `{"type":"openai-compatible","name":"Zai Gateway","base_url":"https://api.z.ai/api/paas/v4","catalog_provider":"zai-coding-plan"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create with known hint: status = %d, body = %s", w.Code, w.Body.String())
	}
	var created struct {
		Provider struct {
			ID                       string `json:"id"`
			CatalogProvider          string `json:"catalog_provider"`
			SuggestedCatalogProvider string `json:"suggested_catalog_provider"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to unmarshal create response: %v", err)
	}
	if created.Provider.CatalogProvider != "zai-coding-plan" {
		t.Errorf("catalog_provider echo = %q, want zai-coding-plan", created.Provider.CatalogProvider)
	}
	if created.Provider.SuggestedCatalogProvider != "zai-coding-plan" {
		t.Errorf("suggested_catalog_provider = %q, want zai-coding-plan (api.z.ai host)", created.Provider.SuggestedCatalogProvider)
	}

	// 2. Unknown id rejected.
	w = doProviderJSON(r, http.MethodPost, "/providers", `{"type":"openai-compatible","name":"Bad Gateway","base_url":"https://gw.example.com/v1","catalog_provider":"no-such-provider-id"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create with unknown hint: status = %d, want 400, body = %s", w.Code, w.Body.String())
	}

	// 3. Canonical types accept a hint but never suggest one.
	w = doProviderJSON(r, http.MethodPost, "/providers", `{"type":"openai","name":"OpenAI Direct","catalog_provider":"zai-coding-plan"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create canonical type with hint: status = %d, body = %s", w.Code, w.Body.String())
	}
	var canonical struct {
		Provider struct {
			SuggestedCatalogProvider string `json:"suggested_catalog_provider"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &canonical); err != nil {
		t.Fatalf("failed to unmarshal canonical create response: %v", err)
	}
	if canonical.Provider.SuggestedCatalogProvider != "" {
		t.Errorf("suggested_catalog_provider for canonical type = %q, want empty", canonical.Provider.SuggestedCatalogProvider)
	}

	// 4. Patch replaces the hint; empty string clears it.
	w = doProviderJSON(r, http.MethodPatch, "/providers/"+created.Provider.ID, `{"catalog_provider":"zai-coding-plan"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch hint: status = %d, body = %s", w.Code, w.Body.String())
	}
	w = doProviderJSON(r, http.MethodPatch, "/providers/"+created.Provider.ID, `{"catalog_provider":""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear hint: status = %d, body = %s", w.Code, w.Body.String())
	}
	var cleared struct {
		Provider struct {
			CatalogProvider string `json:"catalog_provider"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cleared); err != nil {
		t.Fatalf("failed to unmarshal clear response: %v", err)
	}
	if cleared.Provider.CatalogProvider != "" {
		t.Errorf("catalog_provider after clear = %q, want empty", cleared.Provider.CatalogProvider)
	}

	// 5. Patch with an unknown id is rejected too.
	w = doProviderJSON(r, http.MethodPatch, "/providers/"+created.Provider.ID, `{"catalog_provider":"bogus-id"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("patch unknown hint: status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}

// TestProviderHandlers_CatalogFetchFailureFailsOpen covers validation on a
// dead catalog: the create is accepted, because a stale hint only ever
// resolves unknown downstream.
func TestProviderHandlers_CatalogFetchFailureFailsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	deadCatalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer deadCatalog.Close()

	mc := services.NewModelCatalog(services.ModelCatalogOptions{
		CacheDir:   t.TempDir(),
		CatalogURL: deadCatalog.URL,
		TTL:        24 * time.Hour,
		Client:     deadCatalog.Client(),
	})

	st := storefake.New()
	ws := &domain.Workspace{Slug: "prov-failopen-ws", Name: "Provider Failopen WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	h := handlers.NewProviderHandlers(st.Providers(), st.Agents(), []byte("01234567890123456789012345678901"), providers.NewRegistry(), mc, st.ToolSettings())
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.POST("/providers", h.CreateProvider)

	w := doProviderJSON(r, http.MethodPost, "/providers", `{"type":"openai-compatible","name":"Any Gateway","base_url":"https://gw.example.com/v1","catalog_provider":"maybe-stale-id"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create on catalog fetch failure: status = %d, want 201 (fail open), body = %s", w.Code, w.Body.String())
	}
}

// TestProviderHandlers_ModelsUseCatalogHint covers hint consumption: a
// compatible provider with a stored hint resolves catalog models
// (GetProviderModels), and ModelsPreview accepts catalog_provider.
func TestProviderHandlers_ModelsUseCatalogHint(t *testing.T) {
	r, _, catServer := newProviderHintRouter(t)

	// Seed a compatible provider with the hint stored through the handler so
	// the request/persist round-trip is exercised.
	w := doProviderJSON(r, http.MethodPost, "/providers", `{"type":"openai-compatible","name":"Zai Gateway","base_url":"`+catServer.URL+`","catalog_provider":"zai-coding-plan"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed provider: status = %d, body = %s", w.Code, w.Body.String())
	}
	var created struct {
		Provider struct {
			ID string `json:"id"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to unmarshal seed response: %v", err)
	}

	// The stored base URL serves no OpenAI-shaped model list, so tier 1 fails
	// and tier 2 must come from the hint.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/"+created.Provider.ID+"/models", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provider models: status = %d, body = %s", w.Code, w.Body.String())
	}
	var modelsRes struct {
		Source string `json:"source"`
		Models []struct {
			ID         string `json:"id"`
			ImageInput bool   `json:"image_input"`
			PDFInput   bool   `json:"pdf_input"`
		} `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &modelsRes); err != nil {
		t.Fatalf("failed to unmarshal models response: %v", err)
	}
	if modelsRes.Source != "catalog" {
		t.Fatalf("models source = %q, want catalog (hint-driven tier 2), body = %s", modelsRes.Source, w.Body.String())
	}
	var flash *struct {
		ID         string `json:"id"`
		ImageInput bool   `json:"image_input"`
		PDFInput   bool   `json:"pdf_input"`
	}
	for i := range modelsRes.Models {
		if modelsRes.Models[i].ID == "glm-5.3-flash" {
			flash = &modelsRes.Models[i]
			break
		}
	}
	if flash == nil {
		t.Fatalf("models = %+v, want glm-5.3-flash from the hinted catalog", modelsRes.Models)
	}
	if !flash.ImageInput || !flash.PDFInput {
		t.Errorf("glm-5.3-flash capability fields = image:%v pdf:%v, want true/true", flash.ImageInput, flash.PDFInput)
	}

	// ModelsPreview feeds catalog_provider into the preview credential.
	w = doProviderJSON(r, http.MethodPost, "/providers/models/preview", `{"type":"openai-compatible","base_url":"http://127.0.0.1:1/v1","catalog_provider":"zai-coding-plan"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("models preview: status = %d, body = %s", w.Code, w.Body.String())
	}
	var previewRes struct {
		Source string `json:"source"`
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &previewRes); err != nil {
		t.Fatalf("failed to unmarshal preview response: %v", err)
	}
	if previewRes.Source != "catalog" {
		t.Fatalf("preview source = %q, want catalog, models = %+v", previewRes.Source, previewRes.Models)
	}
	foundPreview := false
	for _, m := range previewRes.Models {
		if m.ID == "glm-5.3-flash" {
			foundPreview = true
			break
		}
	}
	if !foundPreview {
		t.Fatalf("preview models = %+v, want glm-5.3-flash from the hinted catalog", previewRes.Models)
	}
}

// newProviderLifecycleRouter wires provider create/list/delete against a fake
// store (the settings sub-store rides it) with the workspace bound.
func newProviderLifecycleRouter(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{Slug: "prov-lifecycle-ws", Name: "Provider Lifecycle WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	h := handlers.NewProviderHandlers(st.Providers(), st.Agents(), []byte("01234567890123456789012345678901"), providers.NewRegistry(), nil, st.ToolSettings())

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.POST("/providers", h.CreateProvider)
	r.GET("/providers", h.ListProviders)
	r.DELETE("/providers/:id", h.DeleteProvider)
	return r, st, ws
}

// TestProviderHandlers_DeleteDecisionReference covers the decision-seam delete
// integrity (design D5): a delete is refused with 409 while the workspace
// memory settings record pins the config as decision_provider_id, and
// succeeds once the pair is cleared. A reference naming a different config
// never blocks.
func TestProviderHandlers_DeleteDecisionReference(t *testing.T) {
	ctx := context.Background()

	t.Run("delete blocked with 409 while the decision configuration references the config", func(t *testing.T) {
		r, st, ws := newProviderLifecycleRouter(t)

		w := doProviderJSON(r, http.MethodPost, "/providers", `{"type":"typesafe","name":"TypeSafe Production","key":"ts-key-123"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create typesafe provider: status = %d, body = %s", w.Code, w.Body.String())
		}
		var created struct {
			Provider struct {
				ID     string `json:"id"`
				Type   string `json:"type"`
				KeySet bool   `json:"key_set"`
			} `json:"provider"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("failed to unmarshal create response: %v", err)
		}
		if created.Provider.Type != "typesafe" || !created.Provider.KeySet {
			t.Fatalf("created provider = %+v, want a typesafe config with key_set", created.Provider)
		}

		// Pin the config as the workspace's decision backend on the memory
		// settings record (the same record key the settings handler uses).
		if err := st.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
			WorkspaceID: ws.ID,
			ToolKey:     "memory",
			Enabled:     true,
			Config:      map[string]any{"decision_provider_id": created.Provider.ID, "decision_model": "jev-latest"},
		}); err != nil {
			t.Fatalf("failed to seed memory settings: %v", err)
		}

		w = doProviderJSON(r, http.MethodDelete, "/providers/"+created.Provider.ID, "")
		if w.Code != http.StatusConflict {
			t.Fatalf("delete decision-referenced provider: status = %d, want 409, body = %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "cannot delete provider referenced by the workspace memory decision configuration") {
			t.Errorf("delete refusal = %s, want it to name the memory decision configuration", w.Body.String())
		}

		// The config and its key remain intact: still listed, still key_set.
		w = doProviderJSON(r, http.MethodGet, "/providers", "")
		if w.Code != http.StatusOK {
			t.Fatalf("list after refusal: status = %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), `"key_set":true`) {
			t.Errorf("provider list after refusal = %s, want the referenced config intact with its key", w.Body.String())
		}
	})

	t.Run("a reference naming another config does not block the delete", func(t *testing.T) {
		r, st, ws := newProviderLifecycleRouter(t)

		seed := func(name string) string {
			w := doProviderJSON(r, http.MethodPost, "/providers", `{"type":"typesafe","name":"`+name+`","key":"k"}`)
			if w.Code != http.StatusCreated {
				t.Fatalf("create %s: status = %d, body = %s", name, w.Code, w.Body.String())
			}
			return jsonGetID(t, w)
		}
		referenced := seed("Referenced")
		victim := seed("Victim")

		if err := st.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
			WorkspaceID: ws.ID,
			ToolKey:     "memory",
			Enabled:     true,
			Config:      map[string]any{"decision_provider_id": referenced, "decision_model": "jev-latest"},
		}); err != nil {
			t.Fatalf("failed to seed memory settings: %v", err)
		}

		w := doProviderJSON(r, http.MethodDelete, "/providers/"+victim, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete unrelated provider: status = %d, want 204, body = %s", w.Code, w.Body.String())
		}
	})

	t.Run("delete succeeds once the decision pair is cleared", func(t *testing.T) {
		r, st, ws := newProviderLifecycleRouter(t)

		w := doProviderJSON(r, http.MethodPost, "/providers", `{"type":"typesafe","name":"TypeSafe Production","key":"ts-key-123"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create typesafe provider: status = %d, body = %s", w.Code, w.Body.String())
		}
		id := jsonGetID(t, w)

		if err := st.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
			WorkspaceID: ws.ID,
			ToolKey:     "memory",
			Enabled:     true,
			Config:      map[string]any{"decision_provider_id": id, "decision_model": "jev-latest"},
		}); err != nil {
			t.Fatalf("failed to seed memory settings: %v", err)
		}

		// Clear the pair (the settings handler's omitted-pair semantics).
		if err := st.ToolSettings().Upsert(ctx, &domain.WorkspaceToolSetting{
			WorkspaceID: ws.ID,
			ToolKey:     "memory",
			Enabled:     true,
			Config:      map[string]any{},
		}); err != nil {
			t.Fatalf("failed to clear memory settings: %v", err)
		}

		w = doProviderJSON(r, http.MethodDelete, "/providers/"+id, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete after clear: status = %d, want 204, body = %s", w.Code, w.Body.String())
		}

		// Subsequent listing no longer carries the deleted config.
		w = doProviderJSON(r, http.MethodGet, "/providers", "")
		if strings.Contains(w.Body.String(), id) {
			t.Errorf("provider list after delete = %s, want the deleted config gone", w.Body.String())
		}
	})
}

// jsonGetID pulls the created provider id out of a create response.
func jsonGetID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var res struct {
		Provider struct {
			ID string `json:"id"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal provider id: %v", err)
	}
	return res.Provider.ID
}

// TestProviderHandlers_TypesafeResolvesNoModels covers the decision-class
// model exclusion: model resolution through a typesafe config yields the
// "none" source with an empty list — decision providers never surface as
// model choices.
func TestProviderHandlers_TypesafeResolvesNoModels(t *testing.T) {
	r, _, _ := newProviderHintRouter(t)

	w := doProviderJSON(r, http.MethodPost, "/providers", `{"type":"typesafe","name":"TypeSafe Production","key":"ts-key-123"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create typesafe provider: status = %d, body = %s", w.Code, w.Body.String())
	}
	id := jsonGetID(t, w)
	if id == "" {
		t.Fatalf("created provider carries no id")
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/"+id+"/models", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("typesafe models: status = %d, body = %s", w.Code, w.Body.String())
	}
	var res struct {
		Source string `json:"source"`
		Models []any  `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal models response: %v", err)
	}
	if res.Source != "none" || len(res.Models) != 0 {
		t.Errorf("typesafe resolution = source %q with %d models, want source none with none", res.Source, len(res.Models))
	}
}
