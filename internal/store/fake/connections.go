package fake

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// connectionStore implements store.Connections in memory. The disconnect
// cascade (design.md D8) runs as one critical section under the store lock —
// the in-memory analogue of the postgres adapter's single transaction.
type connectionStore struct {
	s *fakeStore
}

func (cs *connectionStore) Create(ctx context.Context, c *domain.Connection) error {
	if c == nil {
		return domain.ErrInvalid
	}
	if err := c.Validate(); err != nil {
		return err
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	if _, exists := cs.s.workspaces[c.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	// Uniqueness is per (workspace, service, origin) (add-recipe-base-url
	// tasks.md 2.1): the scan mirrors the postgres index — a parametrized
	// service may hold several origins side by side, the same origin twice is
	// rejected, and origin='' rows collapse to one-connection-per-service.
	for _, stored := range cs.s.connections {
		if stored.WorkspaceID == c.WorkspaceID && stored.Service == c.Service && stored.Origin == c.Origin {
			return fmt.Errorf("%w: service %q already connected in workspace for origin %q", domain.ErrConnectionExists, c.Service, c.Origin)
		}
	}

	if c.ID != "" {
		if _, exists := cs.s.connections[c.ID]; exists {
			return fmt.Errorf("%w: connection with id %q already exists", domain.ErrConflict, c.ID)
		}
	} else {
		c.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	}
	// Empty status is the pre-OAuth shape (change-1 callers set none): stored
	// as connected (add-connection-oauth design.md D6).
	if c.Status == "" {
		c.Status = domain.ConnectionStatusConnected
	}

	cs.s.connections[c.ID] = cloneConnection(c)
	// The per-service index is meaningful only for origin='' rows — the
	// composite (service, origin) rule means several rows may share a service,
	// and they would fight over the single key. Plain rows remain indexed for
	// the snapshot machinery; reads that miss it fall back to a scan.
	if c.Origin == "" {
		cs.s.connectionsByService[c.WorkspaceID+":"+c.Service] = c.ID
	}
	return nil
}

// UpdateTokenLifecycle persists the OAuth token-lifecycle fields of an
// existing connection (refresh envelope, expiry, granted scopes, status):
// the refresh write-through and the expired transition's write path. Under
// the store lock it is the in-memory analogue of the postgres adapter's
// single UPDATE ... WHERE workspace_id = $1 AND id = $2.
func (cs *connectionStore) UpdateTokenLifecycle(ctx context.Context, c *domain.Connection) error {
	if c == nil {
		return domain.ErrInvalid
	}
	if err := c.Validate(); err != nil {
		return err
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	stored, exists := cs.s.connections[c.ID]
	if !exists || stored.WorkspaceID != c.WorkspaceID {
		return domain.ErrNotFound
	}

	now := time.Now().UTC()
	if !now.After(stored.CreatedAt) {
		// Same-clock-tick update: keep updated_at strictly after created_at.
		now = stored.CreatedAt.Add(time.Microsecond)
	}
	stored.RefreshCiphertext = c.RefreshCiphertext
	stored.ExpiresAt = c.ExpiresAt
	stored.GrantedScopes = cloneStringSlice(c.GrantedScopes)
	stored.Status = c.Status
	stored.UpdatedAt = now
	c.UpdatedAt = now
	c.CreatedAt = stored.CreatedAt
	return nil
}

func (cs *connectionStore) Get(ctx context.Context, workspaceID, id string) (*domain.Connection, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	c, exists := cs.s.connections[id]
	if !exists || c.WorkspaceID != workspaceID {
		return nil, domain.ErrNotFound
	}
	return cloneConnection(c), nil
}

func (cs *connectionStore) List(ctx context.Context, workspaceID string) ([]domain.Connection, error) {
	if workspaceID == "" {
		return []domain.Connection{}, nil
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	connections := make([]domain.Connection, 0)
	for _, c := range cs.s.connections {
		if c.WorkspaceID == workspaceID {
			connections = append(connections, *cloneConnection(c))
		}
	}
	sort.Slice(connections, func(i, j int) bool {
		if connections[i].CreatedAt.Equal(connections[j].CreatedAt) {
			return connections[i].ID < connections[j].ID
		}
		return connections[i].CreatedAt.Before(connections[j].CreatedAt)
	})
	return connections, nil
}

func (cs *connectionStore) GetByService(ctx context.Context, workspaceID, service string) (*domain.Connection, error) {
	if workspaceID == "" || service == "" {
		return nil, domain.ErrNotFound
	}

	cs.s.mu.RLock()
	defer cs.s.mu.RUnlock()

	// Fast path: the plain-row index (parametrized rows are never indexed).
	if id, exists := cs.s.connectionsByService[workspaceID+":"+service]; exists {
		if c, ok := cs.s.connections[id]; ok {
			return cloneConnection(c), nil
		}
	}
	// Fallback for multi-origin services (add-recipe-base-url tasks.md 2.1):
	// the earliest-created row wins, mirroring the List order so the
	// resolution is deterministic.
	var found *domain.Connection
	for _, c := range cs.s.connections {
		if c.WorkspaceID != workspaceID || c.Service != service {
			continue
		}
		if found == nil || c.CreatedAt.Before(found.CreatedAt) || (c.CreatedAt.Equal(found.CreatedAt) && c.ID < found.ID) {
			found = c
		}
	}
	if found == nil {
		return nil, domain.ErrNotFound
	}
	return cloneConnection(found), nil
}

// Delete cascades the disconnect atomically (design.md D8): under one lock the
// connection row, its materialized workspace MCP server row, and the agents'
// attachment references to that server die together — the in-memory analogue
// of the postgres adapter's single transaction.
func (cs *connectionStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	cs.s.mu.Lock()
	defer cs.s.mu.Unlock()

	c, exists := cs.s.connections[id]
	if !exists || c.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}

	// The materialized server(s) linked to this connection.
	for srvID, srv := range cs.s.wsMCPServers {
		if srv.WorkspaceID != workspaceID || srv.OriginConnectionID != id {
			continue
		}
		// Strip the agents' attachment references first — Agent.EnabledMCPS is
		// a plain []string with no FK, so only this loop removes the dangling
		// ids.
		for _, a := range cs.s.agents {
			if a.WorkspaceID != workspaceID {
				continue
			}
			a.EnabledMCPS = removeString(a.EnabledMCPS, srvID)
		}
		delete(cs.s.wsMCPServers, srvID)
		delete(cs.s.wsMCPServerNames, workspaceID+":"+strings.ToLower(srv.Name))
		// The removed server's OAuth token row dies with it (ON DELETE
		// CASCADE, add-mcp-oauth-client design.md D4).
		purgeMCPTokenLocked(cs.s, workspaceID, "", srvID)
	}

	delete(cs.s.connections, id)
	// Only the indexed plain row owns the per-service key; parametrized rows
	// (multi-origin services) are never indexed and must not drop another
	// row's entry.
	if key := workspaceID + ":" + c.Service; cs.s.connectionsByService[key] == id {
		delete(cs.s.connectionsByService, key)
	}
	return nil
}

func removeString(list []string, v string) []string {
	out := list[:0:0]
	for _, s := range list {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

// GetByOriginConnection returns the workspace MCP server materialized by the
// given connection (add-workspace-connections design.md D1); (nil, nil) when
// the connection has no linked server.
func (mss *workspaceMCPServerStore) GetByOriginConnection(ctx context.Context, workspaceID, connectionID string) (*domain.WorkspaceMCPServer, error) {
	if workspaceID == "" || connectionID == "" {
		return nil, nil
	}

	mss.s.mu.RLock()
	defer mss.s.mu.RUnlock()

	var found []*domain.WorkspaceMCPServer
	for _, srv := range mss.s.wsMCPServers {
		if srv.WorkspaceID == workspaceID && srv.OriginConnectionID == connectionID {
			found = append(found, cloneWorkspaceMCPServer(srv))
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].CreatedAt.Equal(found[j].CreatedAt) {
			return found[i].ID < found[j].ID
		}
		return found[i].CreatedAt.Before(found[j].CreatedAt)
	})
	return found[0], nil
}
