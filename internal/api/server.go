package api

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/auth"
	"github.com/oniharnantyo/onclaw/internal/api/handler"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// AssembledAgent defines the interface for running the assembled agent.
type AssembledAgent = service.AssembledAgent

// ResolveAndAssembleFunc resolves agent settings and assembles an agent instance.
type ResolveAndAssembleFunc = service.ResolveAndAssembleFunc

// Server represents the API and static asset management server.
type Server struct {
	svc      *service.Service
	handlers *handler.Handler
	sessions *auth.SessionStore
	log      *slog.Logger
}

// NewServer initializes a new Server with service layer and logging.
func NewServer(svc *service.Service, log *slog.Logger) *Server {
	return &Server{
		svc:      svc,
		handlers: handler.New(svc),
		sessions: auth.NewSessionStore(),
		log:      log,
	}
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	mux := s.routes()

	s.log.Info("Web UI server starting", "addr", addr)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return server.ListenAndServe()
}

// Start starts the server on a given listener (useful for testing or dynamic ports).
func (s *Server) Start(ln net.Listener) error {
	mux := s.routes()
	server := &http.Server{
		Handler: mux,
	}
	return server.Serve(ln)
}
