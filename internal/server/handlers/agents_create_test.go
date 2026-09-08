package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// createFlowChatModel is a stand-in BaseChatModel whose Generate either fails
// or returns a valid submit_prompts tool call.
type createFlowChatModel struct{ err error }

func (m *createFlowChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	args, err := json.Marshal(promptgen.GeneratedPrompts{
		Identity: "# Identity\nCreated agent identity.",
		Soul:     "# Soul\nCreated agent soul.",
	})
	if err != nil {
		return nil, err
	}
	return &schema.Message{ToolCalls: []schema.ToolCall{{
		ID:       "call_prompts",
		Function: schema.FunctionCall{Name: "submit_prompts", Arguments: string(args)},
	}}}, nil
}

func (m *createFlowChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream not implemented in mock")
}

func TestCreateAgent_GeneratesBeforePersist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	wsDir := t.TempDir()
	key := []byte("01234567890123456789012345678901")

	ws := &domain.Workspace{ID: "ws-create-flow", Slug: "create-flow-ws", Name: "Create Flow WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	prov := &domain.ProviderConfig{
		ID:          "prov-create-flow",
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
	agentH := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), key, providers.NewRegistry(), nil, agentSvc, wsDir, nil, nil)

	currentUser := &domain.User{ID: "user-1", Email: "owner@example.com", Name: "Owner"}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, currentUser)
		c.Next()
	})
	r.POST("/agents", agentH.CreateAgent)

	body := `{"name":"Scout","slug":"scout","role":"scout","brief":"Scout the perimeter.","provider_id":"prov-create-flow","model":"gpt-4o"}`
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

	if res.Agent.PromptsStatus != domain.PromptsStatusReady {
		t.Errorf("expected prompts_status ready, got %q", res.Agent.PromptsStatus)
	}
	if res.Agent.Identity != "# Identity\nCreated agent identity." {
		t.Errorf("expected composed identity in response, got %q", res.Agent.Identity)
	}

	// The row exists…
	if _, err := st.Agents().ByID(ctx, ws.ID, res.Agent.ID); err != nil {
		t.Fatalf("expected agent row after successful create: %v", err)
	}

	// …and the workspace directory holds the generated documents, with
	// BOOTSTRAP.md seeded from the embedded template.
	dir := filepath.Join(wsDir, ws.Slug, "agents", "scout")
	id, soul, _, err := promptdocs.ReadPromptDocuments(dir)
	if err != nil {
		t.Fatalf("read documents: %v", err)
	}
	if id != "# Identity\nCreated agent identity." || soul != "# Soul\nCreated agent soul." {
		t.Errorf("unexpected document contents: %q / %q", id, soul)
	}
	boot, err := os.ReadFile(filepath.Join(dir, "BOOTSTRAP.md"))
	if err != nil {
		t.Fatalf("read BOOTSTRAP.md: %v", err)
	}
	if string(boot) != promptdocs.BootstrapTemplate {
		t.Errorf("BOOTSTRAP.md = %q, want the embedded template", string(boot))
	}
}

func TestCreateAgent_GenerationFailureAbortsCreate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	wsDir := t.TempDir()
	key := []byte("01234567890123456789012345678901")

	ws := &domain.Workspace{ID: "ws-create-fail", Slug: "create-fail-ws", Name: "Create Fail WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	prov := &domain.ProviderConfig{
		ID:          "prov-create-fail",
		WorkspaceID: ws.ID,
		Type:        providers.TypeOpenAI,
		Name:        "OpenAI",
		Enabled:     true,
	}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	factory := func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseChatModel, error) {
		return &createFlowChatModel{err: errors.New("401 unauthorized from upstream")}, nil
	}
	agentSvc := promptgen.NewService(st.Agents(), st.Providers(), key,
		promptgen.WithModelFactory(factory),
		promptgen.WithTimeout(5*time.Second),
	)
	agentH := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), key, providers.NewRegistry(), nil, agentSvc, wsDir, nil, nil)

	currentUser := &domain.User{ID: "user-1", Email: "owner@example.com", Name: "Owner"}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, currentUser)
		c.Next()
	})
	r.POST("/agents", agentH.CreateAgent)

	body := `{"name":"Echo","slug":"echo","role":"echo","brief":"Echo things.","provider_id":"prov-create-fail","model":"gpt-4o"}`
	req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("expected invalid_request, got %q", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "provider authentication failed") {
		t.Errorf("expected sanitized provider-auth message, got %q", env.Error.Message)
	}

	// Nothing persisted: no row, no directory.
	agentsList, err := st.Agents().ListForWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agentsList) != 0 {
		t.Errorf("expected no agent rows, got %d", len(agentsList))
	}
	if _, statErr := os.Stat(filepath.Join(wsDir, ws.Slug, "agents", "echo")); !os.IsNotExist(statErr) {
		t.Errorf("expected agent directory to be removed, stat err: %v", statErr)
	}
}

func TestCreateAgent_SlugConflictKeepsExistingAgentWorkspace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	wsDir := t.TempDir()
	key := []byte("01234567890123456789012345678901")

	ws := &domain.Workspace{ID: "ws-conflict", Slug: "conflict-ws", Name: "Conflict WS"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	prov := &domain.ProviderConfig{
		ID:          "prov-conflict",
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
	agentH := handlers.NewAgentHandlers(st.Agents(), st.Providers(), st.SessionEvents(), key, providers.NewRegistry(), nil, agentSvc, wsDir, nil, nil)

	currentUser := &domain.User{ID: "user-1", Email: "owner@example.com", Name: "Owner"}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Set(handlers.UserContextKey, currentUser)
		c.Next()
	})
	r.POST("/agents", agentH.CreateAgent)

	// First create succeeds and writes documents on disk.
	body := `{"name":"Scout","slug":"scout","role":"scout","brief":"Scout the perimeter.","provider_id":"prov-conflict","model":"gpt-4o"}`
	req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	dir := filepath.Join(wsDir, ws.Slug, "agents", "scout")
	if _, err := os.Stat(filepath.Join(dir, "IDENTITY.md")); err != nil {
		t.Fatalf("expected IDENTITY.md after first create: %v", err)
	}

	// A duplicate slug must be refused before any filesystem work — seeding or
	// generating into a taken directory would clobber the live agent.
	dupBody := `{"name":"Imposter","slug":"scout","role":"thief","brief":"Steal the dir.","provider_id":"prov-conflict","model":"gpt-4o"}`
	req = httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(dupBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate slug: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// The existing agent's row and workspace survive untouched.
	if _, err := st.Agents().BySlug(ctx, ws.ID, "scout"); err != nil {
		t.Fatalf("existing agent row vanished after conflicting create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "IDENTITY.md")); err != nil {
		t.Fatalf("existing agent's documents were destroyed by the conflicting create: %v", err)
	}
}
