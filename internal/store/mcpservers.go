package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// WorkspaceMCPServers manages workspace-registered MCP servers: the shared,
// admin-managed registry agents opt into. All operations are workspace-scoped;
// a server belonging to another workspace is indistinguishable from an unknown
// id (domain.ErrNotFound). Names are unique per workspace case-insensitively;
// violations return domain.ErrMCPServerNameTaken.
type WorkspaceMCPServers interface {
	List(ctx context.Context, workspaceID string) ([]domain.WorkspaceMCPServer, error)
	Get(ctx context.Context, workspaceID, id string) (*domain.WorkspaceMCPServer, error)
	Create(ctx context.Context, server *domain.WorkspaceMCPServer) error
	// Update replaces the server's editable fields (name, connection, enabled
	// switch). Status, status_error, and tool_count persist until the next
	// SetStatus; they are not writable through Update.
	Update(ctx context.Context, server *domain.WorkspaceMCPServer) error
	Delete(ctx context.Context, workspaceID, id string) error
	// SetStatus persists the outcome of a probe or runtime connection attempt.
	SetStatus(ctx context.Context, workspaceID, id, status, statusError string, toolCount int) error
	// GetByOriginConnection returns the workspace MCP server materialized by
	// the given connection (add-workspace-connections design.md D1);
	// (nil, nil) when the connection has no linked server. Workspace-scoped.
	GetByOriginConnection(ctx context.Context, workspaceID, connectionID string) (*domain.WorkspaceMCPServer, error)
}

// AgentMCPServers manages agent-private MCP servers. All operations are
// agent-scoped; a server belonging to another agent is indistinguishable from
// an unknown id (domain.ErrNotFound). Names are unique per agent
// case-insensitively; violations return domain.ErrMCPServerNameTaken.
type AgentMCPServers interface {
	List(ctx context.Context, agentID string) ([]domain.AgentMCPServer, error)
	Get(ctx context.Context, agentID, id string) (*domain.AgentMCPServer, error)
	Create(ctx context.Context, server *domain.AgentMCPServer) error
	// Update replaces the server's editable fields (name, connection, enabled
	// switch). Status, status_error, and tool_count persist until the next
	// SetStatus; they are not writable through Update.
	Update(ctx context.Context, server *domain.AgentMCPServer) error
	Delete(ctx context.Context, agentID, id string) error
	// SetStatus persists the outcome of a probe or runtime connection attempt.
	SetStatus(ctx context.Context, agentID, id, status, statusError string, toolCount int) error
}
