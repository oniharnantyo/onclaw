package sqlite

import (
	"context"
	"fmt"
	"sync"

	"github.com/oniharnantyo/buddies/internal/store"
)

type mcpOAuthTokenStore struct {
	e     executor
	locks *serverLocks
}

// serverLocks is a process-wide set of per-server mutexes (design D3):
// mcp-go refreshes tokens inside request threads from several handler
// instances, and serialized store access keeps a rotated refresh token from
// being clobbered. One instance is created per database in newStore and
// shared by every store derived from it, including tx-scoped ones.
type serverLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newServerLocks() *serverLocks {
	return &serverLocks{locks: make(map[string]*sync.Mutex)}
}

// lock returns the mutex guarding one server's token row.
func (l *serverLocks) lock(serverID string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, ok := l.locks[serverID]
	if !ok {
		m = &sync.Mutex{}
		l.locks[serverID] = m
	}
	return m
}

// Get returns the server's persisted token set; ErrNotFound when absent.
func (t *mcpOAuthTokenStore) Get(ctx context.Context, serverID string) (*store.MCPOAuthToken, error) {
	mu := t.locks.lock(serverID)
	mu.Lock()
	defer mu.Unlock()

	row := t.e.QueryRowContext(ctx,
		`SELECT server_id, token, updated_at FROM mcp_oauth_tokens WHERE server_id = ?`, serverID)
	var token store.MCPOAuthToken
	var updatedAt string
	if err := row.Scan(&token.ServerID, &token.Token, &updatedAt); err != nil {
		return nil, readErr("get mcp oauth token", err)
	}
	var err error
	if token.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, fmt.Errorf("get mcp oauth token: %w", err)
	}
	return &token, nil
}

// Save create-or-replaces the server's token, stamping UpdatedAt (UTC) when
// the caller left it zero.
func (t *mcpOAuthTokenStore) Save(ctx context.Context, token *store.MCPOAuthToken) error {
	if token.UpdatedAt.IsZero() {
		token.UpdatedAt = nowUTC()
	}
	mu := t.locks.lock(token.ServerID)
	mu.Lock()
	defer mu.Unlock()

	_, err := t.e.ExecContext(ctx, `
INSERT INTO mcp_oauth_tokens (server_id, token, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(server_id) DO UPDATE SET token = excluded.token, updated_at = excluded.updated_at`,
		token.ServerID, string(token.Token), formatTime(token.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("save mcp oauth token: %w", err)
	}
	return nil
}

// Delete removes the server's token; a missing row is not an error.
func (t *mcpOAuthTokenStore) Delete(ctx context.Context, serverID string) error {
	mu := t.locks.lock(serverID)
	mu.Lock()
	defer mu.Unlock()

	if _, err := t.e.ExecContext(ctx, `DELETE FROM mcp_oauth_tokens WHERE server_id = ?`, serverID); err != nil {
		return fmt.Errorf("delete mcp oauth token: %w", err)
	}
	return nil
}
