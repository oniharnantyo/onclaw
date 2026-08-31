package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// providerStore implements storeport.ProviderStore for PostgreSQL.
type providerStore struct {
	db Executor
}

// NewProviderStore creates a new ProviderStore with the given database executor.
func NewProviderStore(db Executor) storeport.ProviderStore {
	return &providerStore{db: db}
}

func (ps *providerStore) Create(ctx context.Context, p *domain.ProviderConfig) error {
	if p == nil || p.WorkspaceID == "" || p.Type == "" || p.Name == "" {
		return domain.ErrInvalid
	}

	if p.ID == "" {
		p.ID = uuid.NewString()
	}

	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}

	query := `
		INSERT INTO workspace_providers (id, workspace_id, type, name, base_url, key_ciphertext, key_hint, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := ps.db.Exec(ctx, query,
		p.ID,
		p.WorkspaceID,
		p.Type,
		p.Name,
		p.BaseURL,
		p.KeyCiphertext,
		p.KeyHint,
		p.Enabled,
		p.CreatedAt,
		p.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ps *providerStore) ByID(ctx context.Context, workspaceID, id string) (*domain.ProviderConfig, error) {
	if workspaceID == "" || id == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, type, name, base_url, key_ciphertext, key_hint, enabled, created_at, updated_at
		FROM workspace_providers
		WHERE workspace_id = $1 AND id = $2
	`
	var p domain.ProviderConfig
	err := ps.db.QueryRow(ctx, query, workspaceID, id).Scan(
		&p.ID,
		&p.WorkspaceID,
		&p.Type,
		&p.Name,
		&p.BaseURL,
		&p.KeyCiphertext,
		&p.KeyHint,
		&p.Enabled,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &p, nil
}

func (ps *providerStore) ListForWorkspace(ctx context.Context, workspaceID string) ([]domain.ProviderConfig, error) {
	if workspaceID == "" {
		return []domain.ProviderConfig{}, nil
	}

	query := `
		SELECT id, workspace_id, type, name, base_url, key_ciphertext, key_hint, enabled, created_at, updated_at
		FROM workspace_providers
		WHERE workspace_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := ps.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	providers := make([]domain.ProviderConfig, 0)
	for rows.Next() {
		var p domain.ProviderConfig
		if err := rows.Scan(
			&p.ID,
			&p.WorkspaceID,
			&p.Type,
			&p.Name,
			&p.BaseURL,
			&p.KeyCiphertext,
			&p.KeyHint,
			&p.Enabled,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, convertError(err)
		}
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return providers, nil
}

func (ps *providerStore) Update(ctx context.Context, p *domain.ProviderConfig) error {
	if p == nil || p.ID == "" || p.WorkspaceID == "" {
		return domain.ErrInvalid
	}

	now := time.Now().UTC()
	query := `
		UPDATE workspace_providers
		SET type = CASE WHEN $1 <> '' THEN $1 ELSE type END,
		    name = CASE WHEN $2 <> '' THEN $2 ELSE name END,
		    base_url = $3,
		    key_ciphertext = $4,
		    key_hint = $5,
		    enabled = $6,
		    updated_at = $7
		WHERE workspace_id = $8 AND id = $9
		RETURNING type, name, base_url, key_ciphertext, key_hint, enabled, created_at, updated_at
	`
	err := ps.db.QueryRow(ctx, query,
		p.Type,
		p.Name,
		p.BaseURL,
		p.KeyCiphertext,
		p.KeyHint,
		p.Enabled,
		now,
		p.WorkspaceID,
		p.ID,
	).Scan(
		&p.Type,
		&p.Name,
		&p.BaseURL,
		&p.KeyCiphertext,
		&p.KeyHint,
		&p.Enabled,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ps *providerStore) Delete(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		DELETE FROM workspace_providers
		WHERE workspace_id = $1 AND id = $2
	`
	tag, err := ps.db.Exec(ctx, query, workspaceID, id)
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
