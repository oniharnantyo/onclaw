package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// seedTokenScope builds one workspace + agent + both server scopes' rows so
// token tests can address either scope kind.
func seedTokenScope(t *testing.T, ctx context.Context, s *fakeStore, slug string) (workspaceID, agentID, wsServerID, agentServerID string) {
	t.Helper()
	ws := &domain.Workspace{Slug: slug, Name: "Token WS " + slug, Timezone: "UTC"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI " + slug, Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "token-agent", Name: "Token Agent", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	wsSrv := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws.ID,
		Name:          "Shared Notion",
		Enabled:       true,
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStreamableHTTP, URL: "https://mcp.example.com/mcp"},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, wsSrv); err != nil {
		t.Fatalf("create workspace server: %v", err)
	}
	agSrv := &domain.AgentMCPServer{
		WorkspaceID:   ws.ID,
		AgentID:       a.ID,
		Name:          "Private Sentry",
		Enabled:       true,
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStreamableHTTP, URL: "https://mcp.sentry.example/mcp"},
	}
	if err := s.AgentMCPServers().Create(ctx, agSrv); err != nil {
		t.Fatalf("create agent server: %v", err)
	}
	return ws.ID, a.ID, wsSrv.ID, agSrv.ID
}

func TestMCPTokens_WorkspaceScopeLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	wsID, _, srvID, _ := seedTokenScope(t, ctx, s, "tok-ws")
	tokens := s.MCPTokens()

	// Get before any write: absent is ErrNotFound.
	if _, err := tokens.Get(ctx, wsID, "", srvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound before the first write, got %v", err)
	}

	// Replace (create): the full row round-trips, timestamps assigned.
	expiresAt := time.Now().Add(time.Hour).UTC()
	first := &domain.MCPToken{
		WorkspaceID:            wsID,
		ServerID:               srvID,
		AccessTokenCiphertext:  "v1:enc:access-1",
		RefreshTokenCiphertext: "v1:enc:refresh-1",
		ExpiresAt:              &expiresAt,
		GrantedScopes:          []string{"default"},
		Issuer:                 "https://auth.example.com",
	}
	if err := tokens.Replace(ctx, first); err != nil {
		t.Fatalf("replace (create): %v", err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Fatalf("expected identity and timestamps assigned, got %+v", first)
	}

	got, err := tokens.Get(ctx, wsID, "", srvID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != first.ID || got.AccessTokenCiphertext != "v1:enc:access-1" || got.RefreshTokenCiphertext != "v1:enc:refresh-1" ||
		got.Issuer != "https://auth.example.com" || got.AgentID != "" {
		t.Fatalf("unexpected token row: %+v", got)
	}
	if got.ScopeKind() != domain.MCPTokenScopeWorkspace {
		t.Fatalf("expected workspace scope kind, got %q", got.ScopeKind())
	}
	if got.GrantedScopes == nil || len(got.GrantedScopes) != 1 || got.GrantedScopes[0] != "default" {
		t.Fatalf("expected granted scopes as an array, got %v", got.GrantedScopes)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected the expiry preserved, got %v", got.ExpiresAt)
	}

	// Replace again (reauthorization/refresh write): same row identity, new
	// token set, created_at fixed, updated_at advancing.
	updatedAt := got.UpdatedAt
	time.Sleep(time.Millisecond)
	second := &domain.MCPToken{
		WorkspaceID:           wsID,
		ServerID:              srvID,
		AccessTokenCiphertext: "v1:enc:access-2",
		GrantedScopes:         []string{"default", "mcp"},
		Issuer:                "https://auth.example.com",
	}
	if err := tokens.Replace(ctx, second); err != nil {
		t.Fatalf("replace (update): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same row id on replace, got %q vs %q", second.ID, first.ID)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("expected created_at fixed at birth, got %v vs %v", second.CreatedAt, first.CreatedAt)
	}
	if !second.UpdatedAt.After(updatedAt) {
		t.Fatalf("expected updated_at to advance, got %v", second.UpdatedAt)
	}
	after, _ := tokens.Get(ctx, wsID, "", srvID)
	if after.AccessTokenCiphertext != "v1:enc:access-2" || after.RefreshTokenCiphertext != "" {
		t.Fatalf("expected the whole token set replaced, got %+v", after)
	}

	// Delete: absent afterwards, and a second delete is ErrNotFound.
	if err := tokens.Delete(ctx, wsID, "", srvID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := tokens.Get(ctx, wsID, "", srvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := tokens.Delete(ctx, wsID, "", srvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on the second delete, got %v", err)
	}
}

func TestMCPTokens_AgentScopeAndTenancy(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	wsID, agentID, _, agentSrvID := seedTokenScope(t, ctx, s, "tok-agent")
	other := newStore()
	otherWsID, _, _, _ := seedTokenScope(t, ctx, other, "tok-other")
	tokens := s.MCPTokens()

	tok := &domain.MCPToken{
		WorkspaceID:           wsID,
		AgentID:               agentID,
		ServerID:              agentSrvID,
		AccessTokenCiphertext: "v1:enc:agent-access",
	}
	if err := tokens.Replace(ctx, tok); err != nil {
		t.Fatalf("replace agent scope: %v", err)
	}

	got, err := tokens.Get(ctx, wsID, agentID, agentSrvID)
	if err != nil {
		t.Fatalf("get agent scope: %v", err)
	}
	if got.ScopeKind() != domain.MCPTokenScopeAgent {
		t.Fatalf("expected agent scope kind, got %q", got.ScopeKind())
	}

	// The workspace-scope addressing (empty agent id) must NOT resolve the
	// agent-scoped row: the scope kind is part of the key.
	if _, err := tokens.Get(ctx, wsID, "", agentSrvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the wrong scope kind, got %v", err)
	}
	// Cross-tenant reads are indistinguishable from unknown.
	if _, err := tokens.Get(ctx, otherWsID, agentID, agentSrvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get, got %v", err)
	}

	// FK parity: an unknown server id is refused, as is an agent/server
	// mismatch.
	if err := tokens.Replace(ctx, &domain.MCPToken{WorkspaceID: wsID, ServerID: "no-such-server", AccessTokenCiphertext: "v1:x"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown workspace server, got %v", err)
	}
	if err := tokens.Replace(ctx, &domain.MCPToken{WorkspaceID: wsID, AgentID: agentID, ServerID: otherWsID, AccessTokenCiphertext: "v1:x"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a server not owned by the agent, got %v", err)
	}
	// Validation parity: a token row without an access envelope is invalid.
	if err := tokens.Replace(ctx, &domain.MCPToken{WorkspaceID: wsID, ServerID: agentSrvID, AgentID: agentID}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a missing access envelope, got %v", err)
	}

	// GetForUpdate reads the same row (the fake serializes via its lock).
	locked, err := tokens.GetForUpdate(ctx, wsID, agentID, agentSrvID)
	if err != nil || locked.AccessTokenCiphertext != "v1:enc:agent-access" {
		t.Fatalf("get-for-update: %v (%+v)", err, locked)
	}
}

// TestMCPTokens_ServerAndAgentDeletionCascade pins the cascade contract
// (add-mcp-oauth-client design.md D4): a server row's deletion (explicit,
// disconnect, agent deletion) takes the token row with it.
func TestMCPTokens_ServerAndAgentDeletionCascade(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	wsID, agentID, wsSrvID, agentSrvID := seedTokenScope(t, ctx, s, "tok-cascade")
	tokens := s.MCPTokens()
	for _, tok := range []*domain.MCPToken{
		{WorkspaceID: wsID, ServerID: wsSrvID, AccessTokenCiphertext: "v1:enc:ws"},
		{WorkspaceID: wsID, AgentID: agentID, ServerID: agentSrvID, AccessTokenCiphertext: "v1:enc:agent"},
	} {
		if err := tokens.Replace(ctx, tok); err != nil {
			t.Fatalf("seed token: %v", err)
		}
	}

	// Explicit delete of the workspace server's token row.
	if err := tokens.Delete(ctx, wsID, "", wsSrvID); err != nil {
		t.Fatalf("delete workspace token: %v", err)
	}

	// Deleting the agent cascades its private server and the token row.
	if err := s.Agents().Delete(ctx, wsID, agentID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	if _, err := tokens.Get(ctx, wsID, agentID, agentSrvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected the agent-scoped token row cascaded away, got %v", err)
	}
}

// TestMCPTokens_ConnectionDisconnectCascades covers the disconnect path: the
// connection's atomic cascade removes the materialized server and its token
// row together.
func TestMCPTokens_ConnectionDisconnectCascades(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	wsID, _, wsSrvID, _ := seedTokenScope(t, ctx, s, "tok-disconnect")
	tokens := s.MCPTokens()
	if err := tokens.Replace(ctx, &domain.MCPToken{WorkspaceID: wsID, ServerID: wsSrvID, AccessTokenCiphertext: "v1:enc:ws"}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	// A connection-linked server dies through the connection cascade.
	if err := s.WorkspaceMCPServers().Delete(ctx, wsID, wsSrvID); err != nil {
		t.Fatalf("delete workspace server: %v", err)
	}
	if _, err := tokens.Get(ctx, wsID, "", wsSrvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected the token row cascaded away with the server, got %v", err)
	}
}
