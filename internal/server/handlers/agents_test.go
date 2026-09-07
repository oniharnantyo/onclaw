package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestAgents_ListComposesPromptDocuments_GetIncludesPromptDocuments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	st := storefake.New()
	wsDir := t.TempDir()

	ws := &domain.Workspace{
		ID:   "ws-summary-roster-test",
		Slug: "summary-roster-ws",
		Name: "Summary Roster Workspace",
	}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	prov := &domain.ProviderConfig{
		ID:          "prov-openai-1",
		WorkspaceID: ws.ID,
		Type:        "openai",
		Name:        "OpenAI",
		Enabled:     true,
	}
	if err := st.Providers().Create(ctx, prov); err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	agentDir := domain.AgentWorkspaceDir(wsDir, ws.Slug, "oracle")
	if err := promptdocs.SeedWorkspace(agentDir); err != nil {
		t.Fatalf("SeedWorkspace: %v", err)
	}
	if err := promptdocs.WritePromptDocuments(agentDir, "# Identity\nOracle identity", "# Soul\nOracle soul"); err != nil {
		t.Fatalf("failed to write prompt documents: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-oracle-1",
		WorkspaceID:   ws.ID,
		Slug:          "oracle",
		Name:          "Oracle Agent",
		Role:          "Advisor",
		Description:   "Advises on system architecture",
		Brief:         "Provide architectural guidance",
		ProviderID:    prov.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusReady,
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	agentH := handlers.NewAgentHandlers(st.Agents(), st.AgentUserMemories(), st.Providers(), st.SessionEvents(), []byte("01234567890123456789012345678901"), nil, nil, nil, wsDir, nil, nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(handlers.WorkspaceContextKey, ws)
		c.Next()
	})
	r.GET("/agents", agentH.ListAgents)
	r.GET("/agents/:agent", agentH.GetAgent)

	// 1. ListAgents omits identity/soul/bootstrap from the list response (summary roster)
	wList := httptest.NewRecorder()
	reqList := httptest.NewRequest(http.MethodGet, "/agents", nil)
	r.ServeHTTP(wList, reqList)

	if wList.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK on ListAgents, got %d: %s", wList.Code, wList.Body.String())
	}

	var listRes struct {
		Agents []domain.Agent `json:"agents"`
	}
	if err := json.Unmarshal(wList.Body.Bytes(), &listRes); err != nil {
		t.Fatalf("failed to unmarshal list response: %v", err)
	}

	if len(listRes.Agents) != 1 {
		t.Fatalf("expected 1 agent in list response, got %d", len(listRes.Agents))
	}

	listAgent := listRes.Agents[0]
	if listAgent.Identity != "" || listAgent.Soul != "" || listAgent.Bootstrap != "" {
		t.Errorf("expected list entry to omit prompt documents, got identity=%q soul=%q bootstrap=%q",
			listAgent.Identity, listAgent.Soul, listAgent.Bootstrap)
	}

	// 2. GetAgent (detail response) should include identity/soul/bootstrap document content
	wGet := httptest.NewRecorder()
	reqGet := httptest.NewRequest(http.MethodGet, "/agents/oracle", nil)
	r.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK on GetAgent, got %d: %s", wGet.Code, wGet.Body.String())
	}

	var getRes struct {
		Agent domain.Agent `json:"agent"`
	}
	if err := json.Unmarshal(wGet.Body.Bytes(), &getRes); err != nil {
		t.Fatalf("failed to unmarshal get response: %v", err)
	}

	if getRes.Agent.Identity != "# Identity\nOracle identity" ||
		getRes.Agent.Soul != "# Soul\nOracle soul" ||
		getRes.Agent.Bootstrap != "" {
		t.Errorf("expected detail response to include prompt documents, got identity=%q soul=%q bootstrap=%q",
			getRes.Agent.Identity, getRes.Agent.Soul, getRes.Agent.Bootstrap)
	}
}
