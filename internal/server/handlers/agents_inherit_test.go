package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// newInheritHarness seeds a workspace with an optional default model, one
// provider, and a promptgen factory that records the (providerType, model)
// pairs generation requests — the inherit leg must generate on the workspace
// default, not on an empty pair.
func newInheritHarness(t *testing.T, withDefault bool, providerType string) (*gin.Engine, store.Store, *domain.Workspace, *domain.ProviderConfig, *[]string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{Slug: "inherit-ws", Name: "Inherit WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: providerType, Name: "Provider", Enabled: true}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if withDefault {
		ws.DefaultModel = &domain.DefaultModelPair{ProviderID: prov.ID, Model: "workspace-default-model"}
		if err := st.Workspaces().Update(ctx, ws); err != nil {
			t.Fatalf("failed to seed default model: %v", err)
		}
	}

	var generatedModels []string
	factory := func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseChatModel, error) {
		generatedModels = append(generatedModels, modelName)
		return &createFlowChatModel{}, nil
	}
	agentSvc := promptgen.NewService(st.Agents(), st.Providers(), []byte("01234567890123456789012345678901"),
		promptgen.WithModelFactory(factory),
		promptgen.WithTimeout(5*time.Second),
	)
	agentH := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), st.AgentSessions(), []byte("01234567890123456789012345678901"), providers.NewRegistry(), nil, agentSvc, t.TempDir(), nil, nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, &domain.User{ID: "user-1", Email: "owner@example.com", Name: "Owner"})
		c.Next()
	})
	r.POST("/agents", agentH.CreateAgent)
	r.PATCH("/agents/:agent", agentH.PatchAgent)
	return r, st, ws, prov, &generatedModels
}

func TestCreateAgent_InheritsWorkspaceDefault(t *testing.T) {
	r, st, ws, prov, generated := newInheritHarness(t, true, providers.TypeOpenAI)

	body := `{"name":"Scout","slug":"scout","role":"scout","brief":"Scout the perimeter."}`
	req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Agent domain.Agent `json:"agent"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Agent.ProviderID != "" || res.Agent.Model != "" {
		t.Fatalf("expected empty pair persisted, got %q/%q", res.Agent.ProviderID, res.Agent.Model)
	}

	// Generation resolved through the workspace default.
	if len(*generated) != 1 || (*generated)[0] != "workspace-default-model" {
		t.Fatalf("expected generation on the workspace default model, got %v", *generated)
	}

	stored, err := st.Agents().ByID(context.Background(), ws.ID, res.Agent.ID)
	if err != nil {
		t.Fatalf("read stored agent: %v", err)
	}
	if stored.ProviderID != "" || stored.Model != "" {
		t.Fatalf("expected stored empty pair, got %q/%q", stored.ProviderID, stored.Model)
	}
	if stored.ProviderID == prov.ID {
		t.Fatal("inherit agent must not pin the provider")
	}
}

func TestCreateAgent_InheritRefusedWithoutDefault(t *testing.T) {
	r, _, _, _, _ := newInheritHarness(t, false, providers.TypeOpenAI)

	body := `{"name":"Scout","slug":"scout","role":"scout","brief":"Scout the perimeter."}`
	req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 without a workspace default, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "workspace default model") {
		t.Fatalf("expected the missing workspace setting named, got %s", w.Body.String())
	}
}

func TestCreateAgent_HalfSetPairRejected(t *testing.T) {
	r, _, _, prov, _ := newInheritHarness(t, true, providers.TypeOpenAI)

	// Model without provider.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"Scout","slug":"s1","role":"scout","brief":"b","model":"gpt-4o"}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for model-only create, got %d: %s", w.Code, w.Body.String())
	}

	// Provider without model.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"Scout","slug":"s2","role":"scout","brief":"b","provider_id":"`+prov.ID+`"}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for provider-only create, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_InheritSkipsMaxTokensRequirement(t *testing.T) {
	// Anthropic requires max_tokens at save time for pinned agents; an inherit
	// agent defers the requirement to run start and saves without one.
	r, _, _, _, _ := newInheritHarness(t, true, providers.TypeAnthropic)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(`{"name":"Scout","slug":"scout","role":"scout","brief":"b"}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for inherit agent on a requiring provider type, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPatchAgent_SwitchToInherit(t *testing.T) {
	r, st, ws, prov, _ := newInheritHarness(t, true, providers.TypeOpenAI)

	ctx := context.Background()
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "pinned-agent", Name: "Pinned", Role: "assistant", Brief: "b", ProviderID: prov.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create pinned agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/agents/pinned-agent", strings.NewReader(`{"provider_id":"", "model":""}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 switching to inherit, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Agent domain.Agent `json:"agent"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Agent.ProviderID != "" || res.Agent.Model != "" {
		t.Fatalf("expected empty pair after switch, got %q/%q", res.Agent.ProviderID, res.Agent.Model)
	}
}

func TestPatchAgent_HalfSetPairRejected(t *testing.T) {
	r, st, ws, prov, _ := newInheritHarness(t, true, providers.TypeOpenAI)

	ctx := context.Background()
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "half-agent", Name: "Half", Role: "assistant", Brief: "b", ProviderID: prov.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/agents/half-agent", strings.NewReader(`{"model":""}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for half-set patch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPatchAgent_SwitchToInheritWithoutDefaultRefused(t *testing.T) {
	// No default in this harness; a pinned agent cannot switch to inherit.
	r, st, ws, prov, _ := newInheritHarness(t, false, providers.TypeOpenAI)

	ctx := context.Background()
	agent := &domain.Agent{WorkspaceID: ws.ID, Slug: "pinned-2", Name: "Pinned", Role: "assistant", Brief: "b", ProviderID: prov.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create pinned agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/agents/pinned-2", strings.NewReader(`{"provider_id":"","model":""}`))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 switching to inherit without a default, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "workspace default model") {
		t.Fatalf("expected the missing workspace setting named, got %s", w.Body.String())
	}
}
