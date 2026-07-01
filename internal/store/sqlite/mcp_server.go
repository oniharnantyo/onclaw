package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type sqliteMCPServerStore struct {
	db *sql.DB
}

// NewMCPServerStore creates a new MCPServerStore backed by SQLite.
func NewMCPServerStore(db *sql.DB) store.MCPServerStore {
	return &sqliteMCPServerStore{db: db}
}

func (s *sqliteMCPServerStore) AddServer(ctx context.Context, srv *store.MCPServer) error {
	if srv.Name == "" {
		return fmt.Errorf("mcp server name must not be empty")
	}

	if srv.CreatedAt == "" {
		srv.CreatedAt = now()
	}
	srv.UpdatedAt = now()

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO mcp_servers (name, transport, command, args, env, url, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		srv.Name, srv.Transport, srv.Command, srv.Args, srv.Env, srv.URL, srv.Enabled, srv.CreatedAt, srv.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return nil
}

func (s *sqliteMCPServerStore) GetServer(ctx context.Context, name string) (*store.MCPServer, error) {
	var srv store.MCPServer
	err := s.db.QueryRowContext(ctx,
		"SELECT name, transport, command, args, env, url, enabled, created_at, updated_at FROM mcp_servers WHERE name = ?",
		name,
	).Scan(&srv.Name, &srv.Transport, &srv.Command, &srv.Args, &srv.Env, &srv.URL, &srv.Enabled, &srv.CreatedAt, &srv.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &srv, nil
}

func (s *sqliteMCPServerStore) ListServers(ctx context.Context) ([]*store.MCPServer, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, transport, command, args, env, url, enabled, created_at, updated_at FROM mcp_servers ORDER BY name ASC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []*store.MCPServer
	for rows.Next() {
		var srv store.MCPServer
		err := rows.Scan(&srv.Name, &srv.Transport, &srv.Command, &srv.Args, &srv.Env, &srv.URL, &srv.Enabled, &srv.CreatedAt, &srv.UpdatedAt)
		if err != nil {
			return nil, err
		}
		servers = append(servers, &srv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return servers, nil
}

func (s *sqliteMCPServerStore) UpdateServer(ctx context.Context, srv *store.MCPServer) error {
	if srv.Name == "" {
		return fmt.Errorf("mcp server name must not be empty")
	}
	srv.UpdatedAt = now()

	res, err := s.db.ExecContext(ctx,
		"UPDATE mcp_servers SET transport = ?, command = ?, args = ?, env = ?, url = ?, enabled = ?, updated_at = ? WHERE name = ?",
		srv.Transport, srv.Command, srv.Args, srv.Env, srv.URL, srv.Enabled, srv.UpdatedAt, srv.Name,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqliteMCPServerStore) RemoveServer(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM mcp_servers WHERE name = ?", name)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
