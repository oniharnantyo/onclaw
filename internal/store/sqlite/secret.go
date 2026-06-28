package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// sqliteSecretStore implements store.SecretStore.
type sqliteSecretStore struct {
	db *sql.DB
}

// NewSecretStore creates a new SecretStore backed by SQLite.
func NewSecretStore(db *sql.DB) store.SecretStore {
	return &sqliteSecretStore{db: db}
}

func (s *sqliteSecretStore) SetSecret(ctx context.Context, key string, encryptedValue string) error {
	if key == "" {
		return fmt.Errorf("secret key must not be empty")
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO config_secrets (key, encrypted_value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET encrypted_value = excluded.encrypted_value",
		key, encryptedValue,
	)
	return err
}

func (s *sqliteSecretStore) GetSecret(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, "SELECT encrypted_value FROM config_secrets WHERE key = ?", key).Scan(&val)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return val, nil
}

func (s *sqliteSecretStore) DeleteSecret(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM config_secrets WHERE key = ?", key)
	return err
}
