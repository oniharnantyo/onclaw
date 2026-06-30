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

	mux.Handle("POST /api/chat", requireAuth(http.HandlerFunc(s.handlers.Chat)))
	mux.Handle("POST /api/logout", requireAuth(auth.Logout(s.sessions)))

	// Static assets and SPA fallback
	mux.Handle("/", s.serveStatic())

	return mux
}
