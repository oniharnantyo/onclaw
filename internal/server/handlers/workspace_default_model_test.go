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
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// newDefaultModelRouter wires the workspace PATCH/GET endpoints against a fake
// store with the workspace and role bound, mirroring the production middleware
// chain: PATCH is guarded by workspace.write, GET by workspace.read — the
// bound role carries only rolePermission. withDefault seeds an initial default
// model on provA.
func newDefaultModelRouter(t *testing.T, rolePermission string, withDefault bool) (*gin.Engine, store.Store, *domain.Workspace, *domain.ProviderConfig, *domain.ProviderConfig) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	ws := &domain.Workspace{Slug: "default-model-ws", Name: "Default Model WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	provA := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := st.Providers().Create(ctx, provA); err != nil {
		t.Fatalf("failed to create provider A: %v", err)
	}
	provB := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "anthropic", Name: "Anthropic", Enabled: true}
	if err := st.Providers().Create(ctx, provB); err != nil {
		t.Fatalf("failed to create provider B: %v", err)
	}

	if withDefault {
		ws.DefaultModel = &domain.DefaultModelPair{ProviderID: provA.ID, Model: "gpt-4o"}
		if err := st.Workspaces().Update(ctx, ws); err != nil {
			t.Fatalf("failed to seed default model: %v", err)
		}
	}

	az, err := newTestAuthorizer(ctx, st)
	if err != nil {
		t.Fatalf("failed to build test authorizer: %v", err)
	}
	h := handlers.NewWorkspaceHandlers(st, []byte("01234567890123456789012345678901"), nil, nil, nil, t.TempDir(), az)

	role := &domain.Role{WorkspaceID: ws.ID, Name: domain.RoleMember, Permissions: []string{rolePermission}}
	member := &domain.Member{WorkspaceID: ws.ID, UserID: "user-1", RoleID: role.ID}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.RoleContextKey, role)
		c.Set(handlers.MemberContextKey, member)
		c.Next()
	})
	// Guards mirror the production router: PATCH requires workspace.write,
	// GET requires workspace.read.
	writeGuarded := r.Group("", requireTestPermission(domain.WorkspaceWrite))
	readGuarded := r.Group("", requireTestPermission(domain.WorkspaceRead))
	writeGuarded.PATCH("/workspace", h.PatchWorkspace)
	readGuarded.GET("/workspace", h.GetWorkspace)
	return r, st, ws, provA, provB
}

func doPatchWorkspace(r *gin.Engine, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/workspace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestPatchWorkspace_SetDefaultModel(t *testing.T) {
	r, st, ws, _, provB := newDefaultModelRouter(t, domain.WorkspaceWrite, false)

	body := `{"default_model": {"provider_id": "` + provB.ID + `", "model": "claude-sonnet-4-5"}}`
	w := doPatchWorkspace(r, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Workspace domain.Workspace `json:"workspace"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Workspace.DefaultModel == nil || res.Workspace.DefaultModel.ProviderID != provB.ID || res.Workspace.DefaultModel.Model != "claude-sonnet-4-5" {
		t.Fatalf("response pair mismatch: %+v", res.Workspace.DefaultModel)
	}

	stored, err := st.Workspaces().ByID(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("read stored workspace: %v", err)
	}
	if stored.DefaultModel == nil || stored.DefaultModel.Model != "claude-sonnet-4-5" {
		t.Fatalf("stored pair mismatch: %+v", stored.DefaultModel)
	}
}

func TestPatchWorkspace_ReplaceDefaultModel(t *testing.T) {
	r, _, _, provA, provB := newDefaultModelRouter(t, domain.WorkspaceWrite, true)

	body := `{"default_model": {"provider_id": "` + provB.ID + `", "model": "claude-sonnet-4-5"}}`
	w := doPatchWorkspace(r, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on replace, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Workspace domain.Workspace `json:"workspace"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Workspace.DefaultModel == nil || res.Workspace.DefaultModel.ProviderID != provB.ID {
		t.Fatalf("expected replaced pair, got %+v", res.Workspace.DefaultModel)
	}
	if provA.ID == "" {
		t.Fatal("provider A expected seeded")
	}
}

func TestPatchWorkspace_ClearDefaultModelFree(t *testing.T) {
	r, st, ws, _, _ := newDefaultModelRouter(t, domain.WorkspaceWrite, true)

	w := doPatchWorkspace(r, `{"default_model": {"provider_id": "", "model": ""}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on free clear, got %d: %s", w.Code, w.Body.String())
	}

	stored, err := st.Workspaces().ByID(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("read stored workspace: %v", err)
	}
	if stored.DefaultModel != nil {
		t.Fatalf("expected cleared pair, got %+v", stored.DefaultModel)
	}
}

func TestPatchWorkspace_ClearDefaultModelBlockedWhileInherited(t *testing.T) {
	r, st, ws, provA, _ := newDefaultModelRouter(t, domain.WorkspaceWrite, true)

	// Two inherit agents + one pinned: the clear must refuse with the count 2.
	ctx := context.Background()
	for _, slug := range []string{"inh-1", "inh-2"} {
		a := &domain.Agent{WorkspaceID: ws.ID, Slug: slug, Name: slug, Role: "assistant", Brief: "b"}
		if err := st.Agents().Create(ctx, a); err != nil {
			t.Fatalf("create inherit agent: %v", err)
		}
	}
	pinned := &domain.Agent{WorkspaceID: ws.ID, Slug: "pinned", Name: "pinned", Role: "assistant", Brief: "b", ProviderID: provA.ID, Model: "gpt-4o"}
	if err := st.Agents().Create(ctx, pinned); err != nil {
		t.Fatalf("create pinned agent: %v", err)
	}

	w := doPatchWorkspace(r, `{"default_model": {"provider_id": "", "model": ""}}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on blocked clear, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "2 agent(s)") {
		t.Fatalf("expected the inherit count in the refusal, got %s", w.Body.String())
	}

	stored, err := st.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read stored workspace: %v", err)
	}
	if stored.DefaultModel == nil || stored.DefaultModel.ProviderID != provA.ID {
		t.Fatalf("expected stored default unchanged, got %+v", stored.DefaultModel)
	}
}

func TestPatchWorkspace_ClearDefaultModelNull(t *testing.T) {
	// The UI sends literal null to clear: the explicit null must be treated as
	// a clear attempt, not an absent key.
	r, st, ws, _, _ := newDefaultModelRouter(t, domain.WorkspaceWrite, true)

	w := doPatchWorkspace(r, `{"default_model": null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on null clear, got %d: %s", w.Code, w.Body.String())
	}

	stored, err := st.Workspaces().ByID(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("read stored workspace: %v", err)
	}
	if stored.DefaultModel != nil {
		t.Fatalf("expected cleared pair, got %+v", stored.DefaultModel)
	}
}

func TestPatchWorkspace_ClearDefaultModelNullBlockedWhileInherited(t *testing.T) {
	r, st, ws, provA, _ := newDefaultModelRouter(t, domain.WorkspaceWrite, true)

	// Two inherit agents: the null clear must refuse with the count 2.
	ctx := context.Background()
	for _, slug := range []string{"inh-1", "inh-2"} {
		a := &domain.Agent{WorkspaceID: ws.ID, Slug: slug, Name: slug, Role: "assistant", Brief: "b"}
		if err := st.Agents().Create(ctx, a); err != nil {
			t.Fatalf("create inherit agent: %v", err)
		}
	}

	w := doPatchWorkspace(r, `{"default_model": null}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on blocked null clear, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "2 agent(s)") {
		t.Fatalf("expected the inherit count in the refusal, got %s", w.Body.String())
	}

	stored, err := st.Workspaces().ByID(ctx, ws.ID)
	if err != nil {
		t.Fatalf("read stored workspace: %v", err)
	}
	if stored.DefaultModel == nil || stored.DefaultModel.ProviderID != provA.ID {
		t.Fatalf("expected stored default unchanged, got %+v", stored.DefaultModel)
	}
}

func TestPatchWorkspace_AbsentDefaultModelKeyLeavesStoredPair(t *testing.T) {
	// A patch without the default_model key at all must leave the stored pair
	// untouched — distinct from the explicit-null clear.
	r, st, ws, provA, _ := newDefaultModelRouter(t, domain.WorkspaceWrite, true)

	w := doPatchWorkspace(r, `{"name": "Renamed WS"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	stored, err := st.Workspaces().ByID(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("read stored workspace: %v", err)
	}
	if stored.DefaultModel == nil || stored.DefaultModel.ProviderID != provA.ID || stored.DefaultModel.Model != "gpt-4o" {
		t.Fatalf("expected stored pair untouched, got %+v", stored.DefaultModel)
	}
	if stored.Name != "Renamed WS" {
		t.Fatalf("expected the name update to apply, got %q", stored.Name)
	}
}

func TestPatchWorkspace_HalfSetDefaultModelRejected(t *testing.T) {
	r, _, _, provA, _ := newDefaultModelRouter(t, domain.WorkspaceWrite, false)

	// Model without provider id.
	w := doPatchWorkspace(r, `{"default_model": {"provider_id": "", "model": "gpt-4o"}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for model-only pair, got %d: %s", w.Code, w.Body.String())
	}

	// Provider id without model.
	w = doPatchWorkspace(r, `{"default_model": {"provider_id": "`+provA.ID+`", "model": ""}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for provider-only pair, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPatchWorkspace_UnknownDefaultProviderRejected(t *testing.T) {
	r, _, _, _, _ := newDefaultModelRouter(t, domain.WorkspaceWrite, false)

	w := doPatchWorkspace(r, `{"default_model": {"provider_id": "prov-does-not-exist", "model": "gpt-4o"}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown provider, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPatchWorkspace_DefaultModelMemberForbidden(t *testing.T) {
	// Member roles carry no workspace.write: the guard must 403 before the
	// handler runs.
	r, _, _, provA, _ := newDefaultModelRouter(t, domain.ProvidersWrite, false)

	w := doPatchWorkspace(r, `{"default_model": {"provider_id": "`+provA.ID+`", "model": "gpt-4o"}}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without workspace.write, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetWorkspace_PayloadCarriesDefaultModel(t *testing.T) {
	r, _, _, provA, _ := newDefaultModelRouter(t, domain.WorkspaceRead, true)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workspace", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Workspace domain.Workspace `json:"workspace"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.Workspace.DefaultModel == nil || res.Workspace.DefaultModel.ProviderID != provA.ID {
		t.Fatalf("expected payload pair, got %+v", res.Workspace.DefaultModel)
	}

	// Unset workspaces carry an explicit null.
	r2, _, _, _, _ := newDefaultModelRouter(t, domain.WorkspaceRead, false)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/workspace", nil)
	r2.ServeHTTP(w2, req2)
	if !strings.Contains(w2.Body.String(), `"default_model":null`) {
		t.Fatalf("expected explicit null default_model, got %s", w2.Body.String())
	}
}
