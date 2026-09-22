package domain

import (
	"fmt"
	"strings"
	"time"
)

// MCP transports. Exactly one connection shape is meaningful per transport:
// stdio executes a local command; streamable HTTP and SSE connect to a URL.
const (
	MCPTransportStdio          = "stdio"
	MCPTransportStreamableHTTP = "streamable_http"
	MCPTransportSSE            = "sse"
)

// MCP connection probe statuses persisted on server rows. An empty status
// means the server has not been probed yet.
const (
	MCPStatusConnected = "connected"
	MCPStatusError     = "error"
)

// ErrMCPServerNameTaken indicates an MCP server name is already used by
// another server within the same scope (workspace or agent, case-insensitive).
// It chains to ErrConflict so generic conflict mapping keeps working.
var ErrMCPServerNameTaken = fmt.Errorf("%w: mcp server name already taken", ErrConflict)

// EnvRow is one name-keyed row of an MCP connection's environment variables or
// HTTP headers. Names are the stable merge key for secret handling; values may
// carry secrets and are encrypted by the settings service before storage.
type EnvRow struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// MCPConnection is the transport-specific connection configuration shared by
// workspace-registered and agent-private MCP servers. Only the fields of the
// configured transport are meaningful; the others are ignored.
type MCPConnection struct {
	Transport string   `json:"transport"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	Env       []EnvRow `json:"env,omitempty"`
	URL       string   `json:"url,omitempty"`
	Headers   []EnvRow `json:"headers,omitempty"`
}

// Validate checks the connection against its transport: the transport must be
// one of the supported constants, stdio requires a command, streamable HTTP
// and SSE require a URL, and the transport's row names (env for stdio, headers
// for the URL transports) must be non-empty and unique within the connection.
// Fields belonging to the other transports are ignored.
func (c *MCPConnection) Validate() error {
	if c == nil {
		return ErrInvalid
	}
	switch c.Transport {
	case MCPTransportStdio:
		if c.Command == "" {
			return fmt.Errorf("%w: command is required for %s transport", ErrInvalid, MCPTransportStdio)
		}
		return validateEnvRowNames(c.Env)
	case MCPTransportStreamableHTTP, MCPTransportSSE:
		if c.URL == "" {
			return fmt.Errorf("%w: url is required for %s transport", ErrInvalid, c.Transport)
		}
		return validateEnvRowNames(c.Headers)
	default:
		return fmt.Errorf("%w: transport %q must be one of %s, %s, or %s", ErrInvalid, c.Transport, MCPTransportStdio, MCPTransportStreamableHTTP, MCPTransportSSE)
	}
}

// validateEnvRowNames checks that every row has a non-blank name and that
// names are unique (exactly, case-sensitively) within the list.
func validateEnvRowNames(rows []EnvRow) error {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Name) == "" {
			return fmt.Errorf("%w: env/header row name cannot be empty", ErrInvalid)
		}
		if _, dup := seen[row.Name]; dup {
			return fmt.Errorf("%w: env/header row name %q is duplicated", ErrInvalid, row.Name)
		}
		seen[row.Name] = struct{}{}
	}
	return nil
}

// WorkspaceMCPServer is one workspace-registered MCP server: a shared,
// admin-managed registry entry agents opt into through Agent.EnabledMCPS.
type WorkspaceMCPServer struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	MCPConnection
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status"`
	StatusError string `json:"status_error,omitempty"`
	ToolCount   int    `json:"tool_count"`
	// OriginConnectionID is the connection that materialized this server
	// (add-workspace-connections design.md D1); empty for hand-made servers.
	// The marker is birth-stamped: the connection is the single authority over
	// a managed server's URL, secret rows, and lifecycle (design.md D11), so
	// it is never rewritten through Update.
	OriginConnectionID string    `json:"origin_connection_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Validate checks the server structurally: workspace scope and name present,
// and the embedded connection valid for its transport.
func (s *WorkspaceMCPServer) Validate() error {
	if s == nil {
		return ErrInvalid
	}
	if s.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalid)
	}
	return s.MCPConnection.Validate()
}

// AgentMCPServer is one agent-private MCP server, addressed and usable only by
// its owning agent. It dies with the agent.
type AgentMCPServer struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	Name        string `json:"name"`
	MCPConnection
	Enabled     bool      `json:"enabled"`
	Status      string    `json:"status"`
	StatusError string    `json:"status_error,omitempty"`
	ToolCount   int       `json:"tool_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Validate checks the server structurally: workspace and agent scope, name,
// and the embedded connection valid for its transport.
func (s *AgentMCPServer) Validate() error {
	if s == nil {
		return ErrInvalid
	}
	if s.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id cannot be empty", ErrInvalid)
	}
	if s.AgentID == "" {
		return fmt.Errorf("%w: agent id cannot be empty", ErrInvalid)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalid)
	}
	return s.MCPConnection.Validate()
}
