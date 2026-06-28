package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// sqliteKVStore implements store.KVStore.
type sqliteKVStore struct {
	db *sql.DB
}

// NewKVStore creates a new KVStore backed by SQLite.
func NewKVStore(db *sql.DB) store.KVStore {
	return &sqliteKVStore{db: db}
}

func (s *sqliteKVStore) Set(ctx context.Context, key string, value string) error {
	if key == "" {
		return fmt.Errorf("KV key must not be empty")
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO preferences (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

func (s *sqliteKVStore) Get(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = ?", key).Scan(&val)
	if err != nil {
		return "", err
	}
	return val, nil
}

func (s *sqliteKVStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM preferences WHERE key = ?", key)
	return err
}
