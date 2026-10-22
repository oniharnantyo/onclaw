package store

import "context"

// MCPServerStore manages MCP server definitions.
type MCPServerStore interface {
	// Create stores a new MCP server, assigning ID and stamping timestamps
	// when empty. A duplicate server name is an error.
	Create(ctx context.Context, server *MCPServer) error
	// ByID returns the MCP server with the given ID; ErrNotFound when
	// absent.
	ByID(ctx context.Context, id string) (*MCPServer, error)
	// List returns all MCP servers, oldest first. The result is never nil.
	List(ctx context.Context) ([]MCPServer, error)
	// Update rewrites an existing MCP server; ErrNotFound when absent.
	Update(ctx context.Context, server *MCPServer) error
	// Delete removes an MCP server; ErrNotFound when absent.
	Delete(ctx context.Context, id string) error
}
