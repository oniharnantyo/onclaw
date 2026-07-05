package api

import (
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/auth"
	"github.com/oniharnantyo/onclaw/internal/api/httpx"
)

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Unprotected routes
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/login", auth.Login(s.sessions, s.svc, s.log))

	// Protected routes auth helper
	requireAuth := auth.RequireAuth(s.sessions, s.log)

	mux.Handle("GET /api/providers", requireAuth(http.HandlerFunc(s.handlers.ListProviders)))
	mux.Handle("POST /api/providers", requireAuth(http.HandlerFunc(s.handlers.CreateProvider)))
	mux.Handle("GET /api/providers/{name}", requireAuth(http.HandlerFunc(s.handlers.GetProvider)))
	mux.Handle("PUT /api/providers/{name}", requireAuth(http.HandlerFunc(s.handlers.UpdateProvider)))
	mux.Handle("DELETE /api/providers/{name}", requireAuth(http.HandlerFunc(s.handlers.DeleteProvider)))
	mux.Handle("POST /api/providers/{name}/default", requireAuth(http.HandlerFunc(s.handlers.SetDefaultProvider)))
	mux.Handle("GET /api/providers/{name}/secret", requireAuth(http.HandlerFunc(s.handlers.GetSecretStatus)))
	mux.Handle("POST /api/providers/{name}/secret", requireAuth(http.HandlerFunc(s.handlers.SetSecret)))

	mux.Handle("GET /api/agents", requireAuth(http.HandlerFunc(s.handlers.ListAgents)))
	mux.Handle("POST /api/agents", requireAuth(http.HandlerFunc(s.handlers.CreateAgent)))
	mux.Handle("GET /api/agents/{name}", requireAuth(http.HandlerFunc(s.handlers.GetAgent)))
	mux.Handle("PUT /api/agents/{name}", requireAuth(http.HandlerFunc(s.handlers.UpdateAgent)))
	mux.Handle("DELETE /api/agents/{name}", requireAuth(http.HandlerFunc(s.handlers.DeleteAgent)))

	mux.Handle("GET /api/conversations", requireAuth(http.HandlerFunc(s.handlers.ListConversations)))
	mux.Handle("GET /api/conversations/{id}/messages", requireAuth(http.HandlerFunc(s.handlers.ListMessages)))

	mux.Handle("GET /api/skills", requireAuth(http.HandlerFunc(s.handlers.ListSkills)))
	mux.Handle("POST /api/skills/discover", requireAuth(http.HandlerFunc(s.handlers.DiscoverSkills)))
	mux.Handle("POST /api/skills", requireAuth(http.HandlerFunc(s.handlers.InstallSkills)))
	mux.Handle("GET /api/skills/{name}", requireAuth(http.HandlerFunc(s.handlers.GetSkill)))
	mux.Handle("DELETE /api/skills/{name}", requireAuth(http.HandlerFunc(s.handlers.DeleteSkill)))
	mux.Handle("POST /api/skills/{name}/update", requireAuth(http.HandlerFunc(s.handlers.UpdateSkill)))
	mux.Handle("GET /api/fs/browse", requireAuth(http.HandlerFunc(s.handlers.BrowseFS)))
	mux.Handle("POST /api/skills/upload", requireAuth(http.HandlerFunc(s.handlers.UploadSkill)))

	mux.Handle("GET /api/hooks", requireAuth(http.HandlerFunc(s.handlers.ListHooks)))
	mux.Handle("GET /api/hooks/{id}", requireAuth(http.HandlerFunc(s.handlers.GetHook)))
	mux.Handle("POST /api/hooks", requireAuth(http.HandlerFunc(s.handlers.AddHook)))
	mux.Handle("PUT /api/hooks/{id}", requireAuth(http.HandlerFunc(s.handlers.UpdateHook)))
	mux.Handle("DELETE /api/hooks/{id}", requireAuth(http.HandlerFunc(s.handlers.RemoveHook)))
	mux.Handle("POST /api/hooks/{id}/toggle", requireAuth(http.HandlerFunc(s.handlers.ToggleHook)))
	mux.Handle("POST /api/hooks/test", requireAuth(http.HandlerFunc(s.handlers.TestHook)))
	mux.Handle("GET /api/hooks/executions", requireAuth(http.HandlerFunc(s.handlers.ListHookExecutions)))

	mux.Handle("GET /api/mcp", requireAuth(http.HandlerFunc(s.handlers.ListMCP)))
	mux.Handle("GET /api/mcp/{name}", requireAuth(http.HandlerFunc(s.handlers.GetMCP)))
	mux.Handle("POST /api/mcp", requireAuth(http.HandlerFunc(s.handlers.AddMCP)))
	mux.Handle("PUT /api/mcp/{name}", requireAuth(http.HandlerFunc(s.handlers.UpdateMCP)))
	mux.Handle("DELETE /api/mcp/{name}", requireAuth(http.HandlerFunc(s.handlers.RemoveMCP)))
	mux.Handle("POST /api/mcp/{name}/toggle", requireAuth(http.HandlerFunc(s.handlers.ToggleMCPServer)))
	mux.Handle("POST /api/mcp/test", requireAuth(http.HandlerFunc(s.handlers.TestMCP)))

	mux.Handle("GET /api/tools", requireAuth(http.HandlerFunc(s.handlers.ListTools)))
	mux.Handle("POST /api/tools/{name}/toggle", requireAuth(http.HandlerFunc(s.handlers.ToggleTool)))
	mux.Handle("GET /api/tools/categories/{cat}/config", requireAuth(http.HandlerFunc(s.handlers.GetCategoryConfig)))
	mux.Handle("PUT /api/tools/categories/{cat}/config", requireAuth(http.HandlerFunc(s.handlers.PutCategoryConfig)))

	mux.Handle("GET /api/memory/dreams", requireAuth(http.HandlerFunc(s.handlers.ListDreamSweeps)))
	mux.Handle("GET /api/memory/staged", requireAuth(http.HandlerFunc(s.handlers.ListStagedWrites)))
	mux.Handle("POST /api/memory/staged/{id}/approve", requireAuth(http.HandlerFunc(s.handlers.ApproveStagedWrite)))
	mux.Handle("POST /api/memory/staged/{id}/reject", requireAuth(http.HandlerFunc(s.handlers.RejectStagedWrite)))

	mux.Handle("POST /api/chat", requireAuth(http.HandlerFunc(s.handlers.Chat)))
	mux.Handle("POST /api/logout", requireAuth(auth.Logout(s.sessions)))

	// Static assets and SPA fallback
	mux.Handle("/", s.serveStatic())

	return mux
}
