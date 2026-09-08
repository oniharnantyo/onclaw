package domain_test

import (
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestMCPConnectionValidate(t *testing.T) {
	tests := []struct {
		name    string
		conn    *domain.MCPConnection
		wantErr bool
	}{
		{
			name: "valid stdio with args and env",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStdio,
				Command:   "npx",
				Args:      []string{"-y", "@modelcontextprotocol/server-github"},
				Env:       []domain.EnvRow{{Name: "GITHUB_TOKEN", Value: "secret"}},
			},
		},
		{
			name:    "stdio without command",
			conn:    &domain.MCPConnection{Transport: domain.MCPTransportStdio, Args: []string{"-y"}},
			wantErr: true,
		},
		{
			name: "stdio ignores url and headers",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStdio,
				Command:   "npx",
				URL:       "http://example.invalid",
				Headers:   []domain.EnvRow{{Name: "X-Dup", Value: "a"}, {Name: "X-Dup", Value: "b"}},
			},
		},
		{
			name:    "stdio with empty env name",
			conn:    &domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx", Env: []domain.EnvRow{{Name: " ", Value: "x"}}},
			wantErr: true,
		},
		{
			name:    "stdio with duplicate env names",
			conn:    &domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx", Env: []domain.EnvRow{{Name: "TOKEN"}, {Name: "TOKEN"}}},
			wantErr: true,
		},
		{
			name: "valid streamable http",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStreamableHTTP,
				URL:       "https://mcp.example.com/mcp",
				Headers:   []domain.EnvRow{{Name: "Authorization", Value: "Bearer tok"}},
			},
		},
		{
			name:    "streamable http without url",
			conn:    &domain.MCPConnection{Transport: domain.MCPTransportStreamableHTTP, Headers: []domain.EnvRow{{Name: "Authorization"}}},
			wantErr: true,
		},
		{
			name:    "streamable http with duplicate header names",
			conn:    &domain.MCPConnection{Transport: domain.MCPTransportStreamableHTTP, URL: "https://mcp.example.com", Headers: []domain.EnvRow{{Name: "X-A"}, {Name: "X-A"}}},
			wantErr: true,
		},
		{
			name:    "streamable http with empty header name",
			conn:    &domain.MCPConnection{Transport: domain.MCPTransportStreamableHTTP, URL: "https://mcp.example.com", Headers: []domain.EnvRow{{Name: ""}}},
			wantErr: true,
		},
		{
			name: "streamable http ignores command args and env",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStreamableHTTP,
				URL:       "https://mcp.example.com/mcp",
				Command:   "npx",
				Args:      []string{"-y"},
				Env:       []domain.EnvRow{{Name: "DUP"}, {Name: "DUP"}},
			},
		},
		{
			name: "valid sse",
			conn: &domain.MCPConnection{Transport: domain.MCPTransportSSE, URL: "https://mcp.example.com/sse"},
		},
		{
			name:    "sse without url",
			conn:    &domain.MCPConnection{Transport: domain.MCPTransportSSE},
			wantErr: true,
		},
		{
			name:    "unknown transport",
			conn:    &domain.MCPConnection{Transport: "websocket", Command: "npx"},
			wantErr: true,
		},
		{
			name:    "empty transport",
			conn:    &domain.MCPConnection{Command: "npx"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.conn.Validate()
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestWorkspaceMCPServerValidate(t *testing.T) {
	var nilServer *domain.WorkspaceMCPServer
	if err := nilServer.Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("nil receiver should be ErrInvalid, got %v", err)
	}

	valid := &domain.WorkspaceMCPServer{
		WorkspaceID: "ws-1",
		Name:        "GitHub",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStdio,
			Command:   "npx",
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	noWorkspace := *valid
	noWorkspace.WorkspaceID = ""
	if err := (&noWorkspace).Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty workspace id should be ErrInvalid, got %v", err)
	}

	noName := *valid
	noName.Name = "   "
	if err := (&noName).Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank name should be ErrInvalid, got %v", err)
	}

	badConn := *valid
	badConn.Transport = "carrier-pigeon"
	if err := (&badConn).Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid connection should be ErrInvalid, got %v", err)
	}
}

func TestAgentMCPServerValidate(t *testing.T) {
	var nilServer *domain.AgentMCPServer
	if err := nilServer.Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("nil receiver should be ErrInvalid, got %v", err)
	}

	valid := &domain.AgentMCPServer{
		WorkspaceID: "ws-1",
		AgentID:     "agent-1",
		Name:        "Private Fetch",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       "https://internal.example.com/mcp",
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	noAgent := *valid
	noAgent.AgentID = ""
	if err := (&noAgent).Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty agent id should be ErrInvalid, got %v", err)
	}

	noName := *valid
	noName.Name = ""
	if err := (&noName).Validate(); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty name should be ErrInvalid, got %v", err)
	}
}

func TestMCPServerNameTakenSentinel(t *testing.T) {
	if !errors.Is(domain.ErrMCPServerNameTaken, domain.ErrConflict) {
		t.Fatal("ErrMCPServerNameTaken must chain to ErrConflict")
	}
}
