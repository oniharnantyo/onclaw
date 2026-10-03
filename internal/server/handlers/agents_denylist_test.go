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

// denylistTestEnv wires the agent handlers (create + patch routes) over the
// fake stores with the stubbed prompt-generation model.
type denylistTestEnv struct {
	st     store.Store
	router *gin.Engine
	ws     *domain.Workspace
}

func newDenylistTestEnv(t *testing.T) *denylistTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	wsDir := t.TempDir()
	key := []byte("01234567890123456789012345678901")

	ws := &domain.Workspace{ID: "ws-denylist", Slug: "denylist-ws", Name: "Denylist WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}
	prov := &domain.ProviderConfig{
		ID:          "prov-denylist",
		WorkspaceID: ws.ID,
		Type:        providers.TypeOpenAI,
		Name:        "OpenAI",
		Enabled:     true,
	}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	factory := func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseChatModel, error) {
		return &createFlowChatModel{}, nil
	}
	agentSvc := promptgen.NewService(st.Agents(), st.Providers(), key,
		promptgen.WithModelFactory(factory),
		promptgen.WithTimeout(5*time.Second),
	)
	agentH := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), st.AgentSessions(), key, providers.NewRegistry(), nil, agentSvc, wsDir, nil, nil, mustTestAuthorizer(t, st))

	currentUser := &domain.User{ID: "user-denylist", Email: "owner@example.com", Name: "Owner"}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, currentUser)
		c.Next()
	})
	r.POST("/agents", agentH.CreateAgent)
	r.PATCH("/agents/:agent", agentH.PatchAgent)

	return &denylistTestEnv{st: st, router: r, ws: ws}
}

func (e *denylistTestEnv) do(t *testing.T, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func decodeAgent(t *testing.T, w *httptest.ResponseRecorder) domain.Agent {
	t.Helper()
	var res struct {
		Agent domain.Agent `json:"agent"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return res.Agent
}

// TestCreateAgent_DisabledToolsDefaultsAndLegacyToolsIgnored pins the create
// path's denylist semantics (refactor-agent-tools-denylist 2.1): omitting
// disabled_tools stores the empty denylist (every catalog tool exposed), a
// provided disabled_tools stores as given, and the legacy `tools` allowlist
// key binds nowhere — accepted and ignored like a managed field.
func TestCreateAgent_DisabledToolsDefaultsAndLegacyToolsIgnored(t *testing.T) {
	env := newDenylistTestEnv(t)

	t.Run("create defaults to the empty denylist", func(t *testing.T) {
		w := env.do(t, http.MethodPost, "/agents",
			`{"name":"Scout","slug":"scout","role":"scout","brief":"Scout the perimeter.","provider_id":"prov-denylist","model":"gpt-4o"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if agent.DisabledTools == nil || len(agent.DisabledTools) != 0 {
			t.Errorf("expected empty disabled_tools array, got %v", agent.DisabledTools)
		}
	})

	t.Run("provided disabled_tools stores as given", func(t *testing.T) {
		w := env.do(t, http.MethodPost, "/agents",
			`{"name":"Pinned","slug":"pinned","role":"scout","brief":"Pinned denylist.","provider_id":"prov-denylist","model":"gpt-4o","disabled_tools":["execute","browser"]}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if len(agent.DisabledTools) != 2 || agent.DisabledTools[0] != "execute" || agent.DisabledTools[1] != "browser" {
			t.Errorf("expected disabled_tools stored as provided, got %v", agent.DisabledTools)
		}
	})

	t.Run("legacy tools key is accepted and ignored", func(t *testing.T) {
		w := env.do(t, http.MethodPost, "/agents",
			`{"name":"Legacy","slug":"legacy","role":"scout","brief":"Legacy allowlist payload.","provider_id":"prov-denylist","model":"gpt-4o","tools":["web.search","ghost.tool"]}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created with the legacy tools key ignored, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if agent.DisabledTools == nil || len(agent.DisabledTools) != 0 {
			t.Errorf("legacy tools payload must not seed any denylist state, got %v", agent.DisabledTools)
		}
		if strings.Contains(w.Body.String(), "\"tools\":") {
			t.Error("responses must not carry the legacy tools field")
		}
	})
}

// TestPatchAgent_DisabledToolsReplaces pins the PATCH semantics: a
// disabled_tools key replaces the stored denylist wholesale (including with
// the empty array), an absent key leaves it untouched, and the legacy `tools`
// key is ignored.
func TestPatchAgent_DisabledToolsReplaces(t *testing.T) {
	env := newDenylistTestEnv(t)

	w := env.do(t, http.MethodPost, "/agents",
		`{"name":"Patchee","slug":"patchee","role":"scout","brief":"Patch target.","provider_id":"prov-denylist","model":"gpt-4o","disabled_tools":["execute","web.search"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	t.Run("patch replaces the stored denylist", func(t *testing.T) {
		w := env.do(t, http.MethodPatch, "/agents/patchee", `{"disabled_tools":["grep"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if len(agent.DisabledTools) != 1 || agent.DisabledTools[0] != "grep" {
			t.Errorf("expected the denylist replaced, got %v", agent.DisabledTools)
		}
	})

	t.Run("empty array clears the denylist", func(t *testing.T) {
		w := env.do(t, http.MethodPatch, "/agents/patchee", `{"disabled_tools":[]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if agent.DisabledTools == nil || len(agent.DisabledTools) != 0 {
			t.Errorf("expected the empty array to disable nothing, got %v", agent.DisabledTools)
		}
	})

	t.Run("absent key leaves the denylist untouched", func(t *testing.T) {
		w := env.do(t, http.MethodPatch, "/agents/patchee", `{"description":"renamed only"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if len(agent.DisabledTools) != 0 {
			t.Errorf("expected the denylist untouched, got %v", agent.DisabledTools)
		}
	})

	t.Run("legacy tools key in a patch is ignored", func(t *testing.T) {
		// A patch must carry at least one recognized field (the same guard a
		// disabled_mcps-only payload hits); the legacy key itself binds
		// nowhere and must not touch the denylist.
		w := env.do(t, http.MethodPatch, "/agents/patchee", `{"description":"renamed","tools":["web.search"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with the legacy key ignored, got %d: %s", w.Code, w.Body.String())
		}
		agent := decodeAgent(t, w)
		if agent.Description != "renamed" {
			t.Errorf("expected the recognized field applied, got %q", agent.Description)
		}
		if len(agent.DisabledTools) != 0 {
			t.Errorf("legacy tools patch must not change the denylist, got %v", agent.DisabledTools)
		}
	})
}
