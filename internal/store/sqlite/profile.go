package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// sqliteProfileStore implements store.ProfileStore.
type sqliteProfileStore struct {
	db *sql.DB
}

// NewProfileStore creates a new ProfileStore backed by SQLite.
func NewProfileStore(db *sql.DB) store.ProfileStore {
	return &sqliteProfileStore{db: db}
}

func (s *sqliteProfileStore) AddProfile(ctx context.Context, p *store.Profile) error {
	if p.Name == "" || p.ProviderType == "" {
		return fmt.Errorf("profile name and provider_type must not be empty")
	}

	if p.CreatedAt == "" {
		p.CreatedAt = now()
	}
	p.UpdatedAt = now()

	if p.Settings == "" {
		p.Settings = "{}"
	}

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO llm_providers (name, provider_type, api_base, settings, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		p.Name, p.ProviderType, p.APIBase, p.Settings, p.Enabled, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return nil
}

func (s *sqliteProfileStore) GetProfile(ctx context.Context, name string) (*store.Profile, error) {
	var p store.Profile
	err := s.db.QueryRowContext(ctx,
		"SELECT name, provider_type, api_base, settings, enabled, created_at, updated_at FROM llm_providers WHERE name = ?",
		name,
	).Scan(&p.Name, &p.ProviderType, &p.APIBase, &p.Settings, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *sqliteProfileStore) ListProfiles(ctx context.Context) ([]*store.Profile, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, provider_type, api_base, settings, enabled, created_at, updated_at FROM llm_providers ORDER BY name ASC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []*store.Profile
	for rows.Next() {
		var p store.Profile
		err := rows.Scan(&p.Name, &p.ProviderType, &p.APIBase, &p.Settings, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return profiles, nil
}

func (s *sqliteProfileStore) RemoveProfile(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM llm_providers WHERE name = ?", name)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
