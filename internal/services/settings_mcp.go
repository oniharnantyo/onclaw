package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/agents/mcp"
	"github.com/oniharnantyo/buddies/internal/store"
)

// CreateMCPServer stores a new MCP server definition, assigning its ID and
// timestamps. A duplicate server name is an error.
func (s *SettingsService) CreateMCPServer(ctx context.Context, server store.MCPServer) (store.MCPServer, error) {
	if err := s.store.MCPServers().Create(ctx, &server); err != nil {
		return store.MCPServer{}, fmt.Errorf("create mcp server: %w", err)
	}
	return server, nil
}

// MCPServer returns the MCP server with the given ID; store.ErrNotFound when
// absent.
func (s *SettingsService) MCPServer(ctx context.Context, id string) (store.MCPServer, error) {
	server, err := s.store.MCPServers().ByID(ctx, id)
	if err != nil {
		return store.MCPServer{}, fmt.Errorf("get mcp server: %w", err)
	}
	return *server, nil
}

// MCPServers returns all MCP servers, oldest first. The result is never nil.
func (s *SettingsService) MCPServers(ctx context.Context) ([]store.MCPServer, error) {
	servers, err := s.store.MCPServers().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	return servers, nil
}

// UpdateMCPServer rewrites an existing MCP server definition, addressed by
// server.ID; store.ErrNotFound when absent. Connection status fields
// (Status, StatusError, ToolCount) persist the last probe outcome and are
// rewritten as given. A successful update invalidates the server's cached
// connection so the new definition takes effect on the next use (design D7);
// invalidation never fails the write.
func (s *SettingsService) UpdateMCPServer(ctx context.Context, server store.MCPServer) (store.MCPServer, error) {
	if err := s.store.MCPServers().Update(ctx, &server); err != nil {
		return store.MCPServer{}, fmt.Errorf("update mcp server: %w", err)
	}
	s.invalidateMCP(server.ID)
	return server, nil
}

// DeleteMCPServer removes the MCP server; store.ErrNotFound when absent. A
// successful delete invalidates (and thereby closes) the server's cached
// connection, reaping any stdio child (design D7); invalidation never fails
// the write.
func (s *SettingsService) DeleteMCPServer(ctx context.Context, id string) error {
	if err := s.store.MCPServers().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete mcp server: %w", err)
	}
	s.invalidateMCP(id)
	return nil
}

// ProbeMCPServer dials the stored server definition on demand, discovers its
// tools, and persists the outcome onto the row's status fields (spec
// agent-mcp "Probe and tool discovery"): connected with the discovered-tool
// count, or error with the underlying reason and a zero count. Refresh is
// invalidate-then-probe (design D6, spec "Connection refresh"): the row's
// cached connection is dropped before dialing — always, even when the probe
// then fails — so what the row shows and what agents use never diverge. An
// authorization-required outcome (design D5) persists StatusAuthRequired
// with the Authorize-call-to-action for the row's auth mode. A probe never
// mutates the definition — the row is read first and only the
// status/status_error/tool_count columns are rewritten. The dial is bounded
// at the manager's connect timeout; store.ErrNotFound when absent.
func (s *SettingsService) ProbeMCPServer(ctx context.Context, id string) (store.MCPServer, error) {
	stored, err := s.store.MCPServers().ByID(ctx, id)
	if err != nil {
		return store.MCPServer{}, fmt.Errorf("probe mcp server: %w", err)
	}
	s.invalidateMCP(id)

	pctx, cancel := context.WithTimeout(ctx, mcp.DefaultConnectTimeout)
	defer cancel()
	count, probeErr := mcp.Probe(pctx, agents.MCPServerRef(*stored, s.store.MCPOAuthTokens()))

	status, statusError := mcp.StatusConnected, ""
	if probeErr != nil {
		status, statusError, count = mcp.StatusError, probeErr.Error(), 0
		if errors.Is(probeErr, mcp.ErrAuthRequired) {
			status = mcp.StatusAuthRequired
			statusError = MCPOAuthRequiredMessage
			if !stored.Auth.IsOAuth() {
				statusError = MCPOAuthRequiredToEnableMessage
			}
		}
	}
	stored.Status = status
	stored.StatusError = statusError
	stored.ToolCount = count
	if err := s.store.MCPServers().Update(ctx, stored); err != nil {
		return store.MCPServer{}, fmt.Errorf("probe mcp server: %w", err)
	}
	return *stored, nil
}
