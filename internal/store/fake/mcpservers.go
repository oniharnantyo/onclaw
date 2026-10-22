package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type mcpServerStore struct{ s *Store }

func (m mcpServerStore) Create(_ context.Context, server *store.MCPServer) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if server.ID == "" {
		server.ID = nextID()
	}
	if _, exists := m.s.mcpServers[server.ID]; exists {
		return fmt.Errorf("mcp server %s already exists", server.ID)
	}
	stamp(&server.CreatedAt, &server.UpdatedAt)
	m.s.mcpServers[server.ID] = cloneMCPServer(*server)
	return nil
}

func (m mcpServerStore) ByID(_ context.Context, id string) (*store.MCPServer, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	server, ok := m.s.mcpServers[id]
	if !ok {
		return nil, fmt.Errorf("mcp server %s: %w", id, store.ErrNotFound)
	}
	out := cloneMCPServer(server)
	return &out, nil
}

func (m mcpServerStore) List(_ context.Context) ([]store.MCPServer, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	out := make([]store.MCPServer, 0, len(m.s.mcpServers))
	for _, server := range m.s.mcpServers {
		out = append(out, cloneMCPServer(server))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m mcpServerStore) Update(_ context.Context, server *store.MCPServer) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	stored, ok := m.s.mcpServers[server.ID]
	if !ok {
		return fmt.Errorf("mcp server %s: %w", server.ID, store.ErrNotFound)
	}
	server.CreatedAt = stored.CreatedAt
	server.UpdatedAt = nowUTC()
	m.s.mcpServers[server.ID] = cloneMCPServer(*server)
	return nil
}

func (m mcpServerStore) Delete(_ context.Context, id string) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	if _, ok := m.s.mcpServers[id]; !ok {
		return fmt.Errorf("mcp server %s: %w", id, store.ErrNotFound)
	}
	delete(m.s.mcpServers, id)
	return nil
}
