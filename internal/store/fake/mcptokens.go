package fake

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// mcpTokenStore implements store.MCPTokens in memory. One row per oauth-mode
// MCP server (add-mcp-oauth-client design.md D4); the cascade helpers below
// are the in-memory analogue of the postgres table's ON DELETE CASCADE.
type mcpTokenStore struct {
	s *fakeStore
}

// mcpTokenKey is the fake's (workspace, agent, server) uniqueness key; the
// empty agentID is the workspace scope.
func mcpTokenKey(workspaceID, agentID, serverID string) string {
	return workspaceID + ":" + agentID + ":" + serverID
}

// cloneMCPToken deep-copies a token row; GrantedScopes always leaves the
// store as an array, never null (the Connection row's normalization).
func cloneMCPToken(t *domain.MCPToken) *domain.MCPToken {
	if t == nil {
		return nil
	}
	cp := *t
	if cp.GrantedScopes == nil {
		cp.GrantedScopes = []string{}
	}
	return &cp
}

// Replace create-or-replaces the server row's single token set under one
// lock — the in-memory analogue of the postgres adapter's single upsert.
// Reference parity with the schema: the workspace-scope server must exist in
// the workspace, the agent-scope server must exist for the agent in the
// workspace, and the agent itself must belong to the workspace.
func (m *mcpTokenStore) Replace(ctx context.Context, t *domain.MCPToken) error {
	if t == nil {
		return domain.ErrInvalid
	}
	if err := t.Validate(); err != nil {
		return err
	}

	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if _, exists := m.s.workspaces[t.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}
	if t.AgentID == "" {
		srv, exists := m.s.wsMCPServers[t.ServerID]
		if !exists || srv.WorkspaceID != t.WorkspaceID {
			return fmt.Errorf("%w: mcp server not found in workspace", domain.ErrNotFound)
		}
	} else {
		agent, exists := m.s.agents[t.AgentID]
		if !exists || agent.WorkspaceID != t.WorkspaceID {
			return fmt.Errorf("%w: agent not found in workspace", domain.ErrNotFound)
		}
		srv, exists := m.s.agentMCPServers[t.ServerID]
		if !exists || srv.AgentID != t.AgentID || srv.WorkspaceID != t.WorkspaceID {
			return fmt.Errorf("%w: mcp server not found for agent", domain.ErrNotFound)
		}
	}

	now := time.Now().UTC()
	if existing, exists := m.s.mcptokens[mcpTokenKey(t.WorkspaceID, t.AgentID, t.ServerID)]; exists {
		// Replace-in-place: identity and birth are fixed, everything else is
		// the new token set.
		t.ID = existing.ID
		t.CreatedAt = existing.CreatedAt
		t.UpdatedAt = now
		m.s.mcptokens[mcpTokenKey(t.WorkspaceID, t.AgentID, t.ServerID)] = cloneMCPToken(t)
		return nil
	}

	t.ID = uuid.NewString()
	t.CreatedAt = now
	t.UpdatedAt = now
	m.s.mcptokens[mcpTokenKey(t.WorkspaceID, t.AgentID, t.ServerID)] = cloneMCPToken(t)
	return nil
}

func (m *mcpTokenStore) Get(ctx context.Context, workspaceID, agentID, serverID string) (*domain.MCPToken, error) {
	if workspaceID == "" || serverID == "" {
		return nil, domain.ErrNotFound
	}

	m.s.mu.RLock()
	defer m.s.mu.RUnlock()

	t, exists := m.s.mcptokens[mcpTokenKey(workspaceID, agentID, serverID)]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneMCPToken(t), nil
}

// GetForUpdate is a plain Get here: the fake's WithTx holds the store lock
// across the whole transaction, so the read is already serialized against
// every other write.
func (m *mcpTokenStore) GetForUpdate(ctx context.Context, workspaceID, agentID, serverID string) (*domain.MCPToken, error) {
	return m.Get(ctx, workspaceID, agentID, serverID)
}

func (m *mcpTokenStore) Delete(ctx context.Context, workspaceID, agentID, serverID string) error {
	if workspaceID == "" || serverID == "" {
		return domain.ErrNotFound
	}

	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	key := mcpTokenKey(workspaceID, agentID, serverID)
	if _, exists := m.s.mcptokens[key]; !exists {
		return domain.ErrNotFound
	}
	delete(m.s.mcptokens, key)
	return nil
}

// purgeMCPTokenLocked drops one server's token row (caller holds s.mu) —
// the ON DELETE CASCADE behind a server row's deletion, including the
// disconnect path that removes workspace servers directly.
func purgeMCPTokenLocked(s *fakeStore, workspaceID, agentID, serverID string) {
	delete(s.mcptokens, mcpTokenKey(workspaceID, agentID, serverID))
}

// purgeAgentMCPTokensLocked drops every token row owned by the agent (caller
// holds s.mu) — the cascade tail behind the agent's deletion (the agent's
// private servers and their token rows die together).
func purgeAgentMCPTokensLocked(s *fakeStore, workspaceID, agentID string) {
	for key, t := range s.mcptokens {
		if t.WorkspaceID == workspaceID && t.AgentID == agentID {
			delete(s.mcptokens, key)
		}
	}
}
