package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// newAgentModalitiesHarness seeds a workspace with two compatible providers
// (one hinted to the capable zai-coding-plan catalog entry, one unhinted) and
// an agent per model, served by a catalog fixture with real models.dev shapes:
// glm-5.3-flash carries input modalities text/image/video/pdf, glm-5.3 is
// text-only.
func newAgentModalitiesHarness(t *testing.T) (*gin.Engine, map[string]*domain.Agent) {
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
	ws := &domain.Workspace{Slug: "agent-mod-ws", Name: "Agent Modalities WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	hinted := &domain.ProviderConfig{
		WorkspaceID:     ws.ID,
		Type:            "openai-compatible",
		Name:            "Zai Gateway",
		BaseURL:         "https://api.z.ai/api/paas/v4",
		CatalogProvider: "zai-coding-plan",
		Enabled:         true,
	}
	if err := st.Providers().Create(ctx, hinted); err != nil {
		t.Fatalf("failed to create hinted provider: %v", err)
	}
	unhinted := &domain.ProviderConfig{
		WorkspaceID: ws.ID,
		Type:        "openai-compatible",
		Name:        "Unknown Gateway",
		BaseURL:     "https://llm.corp.example/v1",
		Enabled:     true,
	}
	if err := st.Providers().Create(ctx, unhinted); err != nil {
		t.Fatalf("failed to create unhinted provider: %v", err)
	}

	makeAgent := func(slug, providerID, model string) *domain.Agent {
		a := &domain.Agent{
			WorkspaceID: ws.ID,
			Slug:        slug,
			Name:        slug,
			Role:        "Tester",
			Brief:       "Test agent",
			ProviderID:  providerID,
			Model:       model,
		}
		if err := st.Agents().Create(ctx, a); err != nil {
			t.Fatalf("failed to create agent %s: %v", slug, err)
		}
		return a
	}
	agents := map[string]*domain.Agent{
		"vision":   makeAgent("vision-agent", hinted.ID, "glm-5.3-flash"),
		"textonly": makeAgent("textonly-agent", hinted.ID, "glm-5.3"),
		"unknown":  makeAgent("unknown-agent", unhinted.ID, "glm-5.3-flash"),
	}

	h := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), st.AgentSessions(), []byte("01234567890123456789012345678901"), providers.NewRegistry(), mc, nil, t.TempDir(), nil, nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.GET("/agents", h.ListAgents)
	r.GET("/agents/:agent", h.GetAgent)

	return r, agents
}

// TestAgents_InputModalitiesPayload covers the agent payload's input-modality
// capability field: JSON shape {"input_modalities":{"image":..,"pdf":..}},
// supported on the capable model, unsupported on the same gateway's text-only
// model, and unknown/unknown without a hint — never failing the read.
func TestAgents_InputModalitiesPayload(t *testing.T) {
	r, _ := newAgentModalitiesHarness(t)

	get := func(slug string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/agents/"+slug, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /agents/%s: status = %d, body = %s", slug, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to unmarshal response for %s: %v", slug, err)
		}
		agent, ok := body["agent"].(map[string]any)
		if !ok {
			t.Fatalf("response for %s carries no agent object: %s", slug, w.Body.String())
		}
		modalities, ok := agent["input_modalities"].(map[string]any)
		if !ok {
			t.Fatalf("agent %s carries no input_modalities object: %s", slug, w.Body.String())
		}
		for _, key := range []string{"image", "pdf"} {
			if _, ok := modalities[key]; !ok {
				t.Fatalf("agent %s input_modalities missing %q: %v", slug, key, modalities)
			}
		}
		return modalities
	}

	// Capable gateway entry → supported/supported.
	vision := get("vision-agent")
	if vision["image"] != string(domain.InputSupported) || vision["pdf"] != string(domain.InputSupported) {
		t.Errorf("vision agent modalities = %v, want image/pdf supported", vision)
	}

	// Same model id family on the same gateway, text-only entry → unsupported.
	textonly := get("textonly-agent")
	if textonly["image"] != string(domain.InputUnsupported) || textonly["pdf"] != string(domain.InputUnsupported) {
		t.Errorf("text-only agent modalities = %v, want image/pdf unsupported", textonly)
	}

	// Unmapped provider without hint → unknown/unknown (read still succeeds).
	unknown := get("unknown-agent")
	if unknown["image"] != string(domain.InputUnknown) || unknown["pdf"] != string(domain.InputUnknown) {
		t.Errorf("unknown agent modalities = %v, want image/pdf unknown", unknown)
	}
}

// TestAgents_InputModalitiesInList covers ListAgents computing the field per
// agent row.
func TestAgents_InputModalitiesInList(t *testing.T) {
	r, _ := newAgentModalitiesHarness(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /agents: status = %d, body = %s", w.Code, w.Body.String())
	}

	var body struct {
		Agents []struct {
			Slug            string `json:"slug"`
			InputModalities *struct {
				Image string `json:"image"`
				PDF   string `json:"pdf"`
			} `json:"input_modalities"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal list response: %v", err)
	}
	if len(body.Agents) != 3 {
		t.Fatalf("agents in list = %d, want 3", len(body.Agents))
	}

	bySlug := make(map[string]struct {
		Image string
		PDF   string
	}, len(body.Agents))
	for _, a := range body.Agents {
		if a.InputModalities == nil {
			t.Fatalf("agent %s carries nil input_modalities in list", a.Slug)
		}
		bySlug[a.Slug] = struct{ Image, PDF string }{a.InputModalities.Image, a.InputModalities.PDF}
	}

	if got := bySlug["vision-agent"]; got.Image != "supported" || got.PDF != "supported" {
		t.Errorf("vision-agent list modalities = %+v, want supported/supported", got)
	}
	if got := bySlug["textonly-agent"]; got.Image != "unsupported" || got.PDF != "unsupported" {
		t.Errorf("textonly-agent list modalities = %+v, want unsupported/unsupported", got)
	}
	if got := bySlug["unknown-agent"]; got.Image != "unknown" || got.PDF != "unknown" {
		t.Errorf("unknown-agent list modalities = %+v, want unknown/unknown", got)
	}
}
