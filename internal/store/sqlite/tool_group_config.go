package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type sqliteToolGroupConfigStore struct {
	db *sql.DB
}

// NewToolGroupConfigStore creates a new ToolGroupConfigStore backed by SQLite.
func NewToolGroupConfigStore(db *sql.DB) store.ToolGroupConfigStore {
	return &sqliteToolGroupConfigStore{db: db}
}

func (s *sqliteToolGroupConfigStore) GetConfig(ctx context.Context, category string) (*store.ToolGroupConfig, error) {
	var c store.ToolGroupConfig
	err := s.db.QueryRowContext(ctx,
		"SELECT category, config, created_at, updated_at FROM tool_group_config WHERE category = ?",
		category,
	).Scan(&c.Category, &c.Config, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get config for category %s: %w", category, err)
	}
	return &c, nil
}

func (s *sqliteToolGroupConfigStore) PutConfig(ctx context.Context, category string, config string) error {
	if category == "" {
		return fmt.Errorf("category must not be empty")
	}
	currentTime := now()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_group_config (category, config, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(category) DO UPDATE SET
			config = excluded.config,
			updated_at = excluded.updated_at`,
		category, config, currentTime, currentTime,
	)
	if err != nil {
		return fmt.Errorf("put config for category %s: %w", category, err)
	}
	return nil
}
