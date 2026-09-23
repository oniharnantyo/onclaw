package domain_test

import (
	"encoding/json"
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

func TestMCPConnectionAuthModeValidate(t *testing.T) {
	tests := []struct {
		name    string
		conn    *domain.MCPConnection
		wantErr bool
	}{
		{
			name: "empty auth mode on a static header row stays valid",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStreamableHTTP,
				URL:       "https://mcp.example.com/mcp",
				Headers:   []domain.EnvRow{{Name: "Authorization", Value: "Bearer tok"}},
			},
		},
		{
			name: "explicit none mode with static headers",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStreamableHTTP,
				URL:       "https://mcp.example.com/mcp",
				AuthMode:  domain.MCPAuthModeNone,
				Headers:   []domain.EnvRow{{Name: "X-Key", Value: "k"}},
			},
		},
		{
			name: "oauth mode on streamable http",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStreamableHTTP,
				URL:       "https://mcp.example.com/mcp",
				AuthMode:  domain.MCPAuthModeOAuth,
			},
		},
		{
			name: "oauth mode on sse",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportSSE,
				URL:       "https://mcp.example.com/sse",
				AuthMode:  domain.MCPAuthModeOAuth,
			},
		},
		{
			name: "oauth mode rejected on stdio",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStdio,
				Command:   "/bin/true",
				AuthMode:  domain.MCPAuthModeOAuth,
			},
			wantErr: true,
		},
		{
			name: "unknown auth mode rejected",
			conn: &domain.MCPConnection{
				Transport: domain.MCPTransportStreamableHTTP,
				URL:       "https://mcp.example.com/mcp",
				AuthMode:  "bearer",
			},
			wantErr: true,
		},
		{
			name: "BYO confidential rows in oauth mode",
			conn: &domain.MCPConnection{
				Transport:         domain.MCPTransportStreamableHTTP,
				URL:               "https://mcp.example.com/mcp",
				AuthMode:          domain.MCPAuthModeOAuth,
				OAuthClientID:     "client-id-1",
				OAuthClientSecret: "client-secret-1",
			},
		},
		{
			name: "public client (id without secret) in oauth mode",
			conn: &domain.MCPConnection{
				Transport:     domain.MCPTransportStreamableHTTP,
				URL:           "https://mcp.example.com/mcp",
				AuthMode:      domain.MCPAuthModeOAuth,
				OAuthClientID: "client-id-1",
			},
		},
		{
			name: "BYO secret without an id rejected in oauth mode",
			conn: &domain.MCPConnection{
				Transport:         domain.MCPTransportStreamableHTTP,
				URL:               "https://mcp.example.com/mcp",
				AuthMode:          domain.MCPAuthModeOAuth,
				OAuthClientSecret: "client-secret-1",
			},
			wantErr: true,
		},
		{
			name: "BYO id outside oauth mode rejected",
			conn: &domain.MCPConnection{
				Transport:     domain.MCPTransportStreamableHTTP,
				URL:           "https://mcp.example.com/mcp",
				AuthMode:      domain.MCPAuthModeNone,
				OAuthClientID: "client-id-1",
			},
			wantErr: true,
		},
		{
			name: "BYO id on an empty-mode static row rejected",
			conn: &domain.MCPConnection{
				Transport:     domain.MCPTransportStreamableHTTP,
				URL:           "https://mcp.example.com/mcp",
				OAuthClientID: "client-id-1",
			},
			wantErr: true,
		},
		{
			name: "BYO secret on stdio rejected",
			conn: &domain.MCPConnection{
				Transport:         domain.MCPTransportStdio,
				Command:           "/bin/true",
				AuthMode:          domain.MCPAuthModeNone,
				OAuthClientSecret: "client-secret-1",
			},
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

// TestMCPConnectionStaticModeShapeUnchanged pins the byte-identical JSON
// shape for static-mode rows: none of the OAuth fields may appear unless set
// (spec: "Static-header connections MUST continue to dial exactly as before").
func TestMCPConnectionStaticModeShapeUnchanged(t *testing.T) {
	before := `{"transport":"streamable_http","url":"https://mcp.example.com/mcp","headers":[{"name":"Authorization","value":"Bearer tok"}]}`
	var conn domain.MCPConnection
	if err := json.Unmarshal([]byte(before), &conn); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if conn.AuthMode != "" {
		t.Fatalf("expected an empty auth mode to decode, got %q", conn.AuthMode)
	}
	after, err := json.Marshal(&conn)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(after) != before {
		t.Fatalf("static-mode row changed shape:\n before %s\n after  %s", before, after)
	}
}

func TestValidateMCPAuthMode(t *testing.T) {
	for _, mode := range []string{"", domain.MCPAuthModeNone, domain.MCPAuthModeOAuth} {
		if !domain.IsValidMCPAuthMode(mode) {
			t.Errorf("expected %q to be a valid auth mode", mode)
		}
	}
	if domain.IsValidMCPAuthMode("kerberos") {
		t.Error("expected an unknown auth mode to be invalid")
	}
}

// TestCanTransitionMCPStatus mirrors the connection-status lifecycle rules
// (add-mcp-oauth-client design.md D6): expired is entered only from a live
// status by the refresh-failure path and left only to connected by
// reauthorization.
func TestCanTransitionMCPStatus(t *testing.T) {
	live := []string{domain.MCPStatusConnected, domain.MCPStatusError}
	for _, from := range live {
		if !domain.CanTransitionMCPStatus(from, domain.MCPStatusExpired) {
			t.Errorf("expected %s→expired to be legal (refresh failure enters it)", from)
		}
	}
	if !domain.CanTransitionMCPStatus(domain.MCPStatusExpired, domain.MCPStatusExpired) {
		t.Error("expected expired→expired to be legal (repeated refresh failure is idempotent)")
	}
	if !domain.CanTransitionMCPStatus(domain.MCPStatusExpired, domain.MCPStatusConnected) {
		t.Error("expected expired→connected to be legal (reauthorization clears it)")
	}
	if domain.CanTransitionMCPStatus(domain.MCPStatusExpired, domain.MCPStatusError) {
		t.Error("expected expired→error to be refused (a probe outcome must not mask the recovery state)")
	}
	for _, from := range live {
		for _, to := range live {
			if !domain.CanTransitionMCPStatus(from, to) {
				t.Errorf("expected %s→%s to be legal (probe outcomes)", from, to)
			}
		}
	}
	for _, status := range []string{"", domain.MCPStatusExpired, domain.MCPStatusConnected, domain.MCPStatusError, "nonsense"} {
		if domain.CanTransitionMCPStatus(status, domain.MCPStatusConnected) && status == "nonsense" {
			t.Error("expected an unknown status to be refused as a transition source")
		}
		if domain.CanTransitionMCPStatus(domain.MCPStatusConnected, status) && status == "nonsense" {
			t.Error("expected an unknown status to be refused as a transition target")
		}
	}
}

func TestValidateMCPStatus(t *testing.T) {
	for _, status := range []string{"", domain.MCPStatusConnected, domain.MCPStatusError, domain.MCPStatusExpired} {
		if !domain.IsValidMCPStatus(status) {
			t.Errorf("expected %q to be a valid mcp status", status)
		}
	}
	if domain.IsValidMCPStatus("degraded") {
		t.Error("expected an unknown mcp status to be invalid")
	}
}
