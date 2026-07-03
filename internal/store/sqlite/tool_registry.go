package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type sqliteToolRegistryStore struct {
	db *sql.DB
}

// NewToolRegistryStore creates a new ToolRegistryStore backed by SQLite.
func NewToolRegistryStore(db *sql.DB) store.ToolRegistryStore {
	return &sqliteToolRegistryStore{db: db}
}

func (s *sqliteToolRegistryStore) ListTools(ctx context.Context) ([]*store.ToolRegistry, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, category, enabled, created_at, updated_at FROM tool_registry ORDER BY category ASC, name ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	defer rows.Close()

	var tools []*store.ToolRegistry
	for rows.Next() {
		var t store.ToolRegistry
		err := rows.Scan(&t.Name, &t.Category, &t.Enabled, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan tool registry: %w", err)
		}
		tools = append(tools, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}
	return tools, nil
}

func (s *sqliteToolRegistryStore) GetTool(ctx context.Context, name string) (*store.ToolRegistry, error) {
	var t store.ToolRegistry
	err := s.db.QueryRowContext(ctx,
		"SELECT name, category, enabled, created_at, updated_at FROM tool_registry WHERE name = ?",
		name,
	).Scan(&t.Name, &t.Category, &t.Enabled, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get tool %s: %w", name, err)
	}
	return &t, nil
}

func (s *sqliteToolRegistryStore) UpsertTool(ctx context.Context, t *store.ToolRegistry) error {
	if t.Name == "" {
		return fmt.Errorf("tool name must not be empty")
	}
	if t.CreatedAt == "" {
		t.CreatedAt = now()
	}
	t.UpdatedAt = now()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_registry (name, category, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			category = excluded.category,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at`,
		t.Name, t.Category, t.Enabled, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert tool %s: %w", t.Name, err)
	}
	return nil
}

func (s *sqliteToolRegistryStore) ToggleTool(ctx context.Context, name string, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE tool_registry SET enabled = ?, updated_at = ? WHERE name = ?",
		enabledInt, now(), name,
	)
	if err != nil {
		return fmt.Errorf("toggle tool %s: %w", name, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
