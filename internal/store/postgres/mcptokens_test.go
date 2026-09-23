//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// mcpTokenSeedServer creates a workspace-registered streamable-HTTP MCP server.
func mcpTokenSeedServer(t *testing.T, ctx context.Context, s store.Store, workspaceID, name, url string) string {
	t.Helper()
	srv := &domain.WorkspaceMCPServer{
		WorkspaceID:   workspaceID,
		Name:          name,
		Enabled:       true,
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStreamableHTTP, URL: url},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, srv); err != nil {
		t.Fatalf("unexpected create server error: %v", err)
	}
	return srv.ID
}

// mcpTokenSeedAgentServer creates an agent-private streamable-HTTP MCP server.
func mcpTokenSeedAgentServer(t *testing.T, ctx context.Context, s store.Store, workspaceID, agentID, name, url string) string {
	t.Helper()
	srv := &domain.AgentMCPServer{
		WorkspaceID:   workspaceID,
		AgentID:       agentID,
		Name:          name,
		Enabled:       true,
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStreamableHTTP, URL: url},
	}
	if err := s.AgentMCPServers().Create(ctx, srv); err != nil {
		t.Fatalf("unexpected create agent server error: %v", err)
	}
	return srv.ID
}

func TestIntegration_MCPTokens_WorkspaceScopeLifecycle(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws1 := mcpSeedWorkspace(t, ctx, s, "tok-ws-1")
	ws2 := mcpSeedWorkspace(t, ctx, s, "tok-ws-2")
	srvID := mcpTokenSeedServer(t, ctx, s, ws1.ID, "Notion", "https://mcp.notion.example/mcp")
	tokens := s.MCPTokens()

	// Absence before the first write.
	if _, err := tokens.Get(ctx, ws1.ID, "", srvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound before the first write, got %v", err)
	}

	// Replace (create): the full row round-trips.
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	first := &domain.MCPToken{
		WorkspaceID:            ws1.ID,
		ServerID:               srvID,
		AccessTokenCiphertext:  "v1:enc:access-1",
		RefreshTokenCiphertext: "v1:enc:refresh-1",
		ExpiresAt:              &expiresAt,
		GrantedScopes:          []string{"default"},
		Issuer:                 "https://auth.notion.example",
	}
	if err := tokens.Replace(ctx, first); err != nil {
		t.Fatalf("unexpected replace error: %v", err)
	}
	if first.ID == "" || first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Fatalf("expected identity and timestamps assigned, got %+v", first)
	}

	got, err := tokens.Get(ctx, ws1.ID, "", srvID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.ID != first.ID || got.AccessTokenCiphertext != "v1:enc:access-1" || got.RefreshTokenCiphertext != "v1:enc:refresh-1" {
		t.Fatalf("unexpected token row: %+v", got)
	}
	if got.ScopeKind() != domain.MCPTokenScopeWorkspace || got.AgentID != "" {
		t.Fatalf("expected a workspace-scoped row, got %+v", got)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected the expiry preserved, got %v vs %v", got.ExpiresAt, expiresAt)
	}
	if len(got.GrantedScopes) != 1 || got.GrantedScopes[0] != "default" {
		t.Fatalf("unexpected granted scopes: %v", got.GrantedScopes)
	}
	if got.Issuer != "https://auth.notion.example" {
		t.Fatalf("unexpected issuer: %q", got.Issuer)
	}

	// Tenant isolation: another workspace's lookup is indistinguishable from
	// unknown, and so is an unknown/invalid server id.
	if _, err := tokens.Get(ctx, ws2.ID, "", srvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant get, got %v", err)
	}
	if _, err := tokens.Get(ctx, ws1.ID, "", uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown server, got %v", err)
	}

	// Replace again: same identity, created_at fixed, token set replaced.
	time.Sleep(time.Millisecond)
	second := &domain.MCPToken{
		WorkspaceID:           ws1.ID,
		ServerID:              srvID,
		AccessTokenCiphertext: "v1:enc:access-2",
		GrantedScopes:         []string{"default", "mcp"},
		Issuer:                "https://auth.notion.example",
	}
	if err := tokens.Replace(ctx, second); err != nil {
		t.Fatalf("unexpected replace error: %v", err)
	}
	if second.ID != first.ID || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("expected identity fixed at birth, got id %q/%q created %v/%v", second.ID, first.ID, second.CreatedAt, first.CreatedAt)
	}
	after, _ := tokens.Get(ctx, ws1.ID, "", srvID)
	if after.AccessTokenCiphertext != "v1:enc:access-2" || after.RefreshTokenCiphertext != "" {
		t.Fatalf("expected the whole token set replaced, got %+v", after)
	}

	// Delete: absent afterwards, second delete ErrNotFound.
	if err := tokens.Delete(ctx, ws1.ID, "", srvID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	if _, err := tokens.Get(ctx, ws1.ID, "", srvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := tokens.Delete(ctx, ws1.ID, "", srvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on the second delete, got %v", err)
	}
}

func TestIntegration_MCPTokens_AgentScope(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, a := mcpSeedAgent(t, ctx, s, "tok-agent")
	srvID := mcpTokenSeedAgentServer(t, ctx, s, ws.ID, a.ID, "Sentry", "https://mcp.sentry.example/mcp")
	tokens := s.MCPTokens()

	if err := tokens.Replace(ctx, &domain.MCPToken{
		WorkspaceID:           ws.ID,
		AgentID:               a.ID,
		ServerID:              srvID,
		AccessTokenCiphertext: "v1:enc:agent-access",
	}); err != nil {
		t.Fatalf("unexpected replace error: %v", err)
	}

	got, err := tokens.Get(ctx, ws.ID, a.ID, srvID)
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if got.ScopeKind() != domain.MCPTokenScopeAgent || got.AccessTokenCiphertext != "v1:enc:agent-access" {
		t.Fatalf("unexpected agent-scoped row: %+v", got)
	}

	// The workspace-scope addressing must not resolve the agent-scoped row.
	if _, err := tokens.Get(ctx, ws.ID, "", srvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for the wrong scope kind, got %v", err)
	}

	// FK parity: an unknown server id is refused.
	err = tokens.Replace(ctx, &domain.MCPToken{WorkspaceID: ws.ID, ServerID: uuid.NewString(), AccessTokenCiphertext: "v1:x"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown server, got %v", err)
	}
}

func TestIntegration_MCPTokens_Cascades(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws, a := mcpSeedAgent(t, ctx, s, "tok-cascade")
	wsSrvID := mcpTokenSeedServer(t, ctx, s, ws.ID, "GitLab", "https://gitlab.example/api/v4/mcp")
	agSrvID := mcpTokenSeedAgentServer(t, ctx, s, ws.ID, a.ID, "Private", "https://private.example/mcp")
	tokens := s.MCPTokens()
	if err := tokens.Replace(ctx, &domain.MCPToken{WorkspaceID: ws.ID, ServerID: wsSrvID, AccessTokenCiphertext: "v1:enc:ws"}); err != nil {
		t.Fatalf("seed workspace token: %v", err)
	}
	if err := tokens.Replace(ctx, &domain.MCPToken{WorkspaceID: ws.ID, AgentID: a.ID, ServerID: agSrvID, AccessTokenCiphertext: "v1:enc:agent"}); err != nil {
		t.Fatalf("seed agent token: %v", err)
	}

	// Deleting the workspace server row cascades its token row.
	if err := s.WorkspaceMCPServers().Delete(ctx, ws.ID, wsSrvID); err != nil {
		t.Fatalf("delete workspace server: %v", err)
	}
	if _, err := tokens.Get(ctx, ws.ID, "", wsSrvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected the workspace token row cascaded away, got %v", err)
	}

	// Deleting the agent cascades its private server and its token row.
	if err := s.Agents().Delete(ctx, ws.ID, a.ID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	if _, err := tokens.Get(ctx, ws.ID, a.ID, agSrvID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected the agent token row cascaded away, got %v", err)
	}
}

func TestIntegration_MCPTokens_GetForUpdateInTx(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	ws := mcpSeedWorkspace(t, ctx, s, "tok-tx")
	srvID := mcpTokenSeedServer(t, ctx, s, ws.ID, "Linear", "https://mcp.linear.example/mcp")
	tokens := s.MCPTokens()
	if err := tokens.Replace(ctx, &domain.MCPToken{WorkspaceID: ws.ID, ServerID: srvID, AccessTokenCiphertext: "v1:enc:stale"}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	// The read-refresh-write sequence rides GetForUpdate inside WithTx: the
	// row lock and the replacement commit atomically.
	err := s.WithTx(ctx, func(tx store.Store) error {
		locked, err := tx.MCPTokens().GetForUpdate(ctx, ws.ID, "", srvID)
		if err != nil {
			return err
		}
		locked.AccessTokenCiphertext = "v1:enc:refreshed"
		return tx.MCPTokens().Replace(ctx, locked)
	})
	if err != nil {
		t.Fatalf("unexpected WithTx error: %v", err)
	}
	after, err := tokens.Get(ctx, ws.ID, "", srvID)
	if err != nil || after.AccessTokenCiphertext != "v1:enc:refreshed" {
		t.Fatalf("expected the in-tx refresh committed, got %+v (%v)", after, err)
	}
}
