package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// apiKeyStore implements storeport.WorkspaceAPIKeyStore for PostgreSQL.
type apiKeyStore struct {
	db Executor
}

// NewAPIKeyStore creates a new WorkspaceAPIKeyStore with the given database executor.
func NewAPIKeyStore(db Executor) storeport.WorkspaceAPIKeyStore {
	return &apiKeyStore{db: db}
}

func (ks *apiKeyStore) Create(ctx context.Context, key *domain.WorkspaceAPIKey) error {
	if key == nil || key.WorkspaceID == "" || key.Name == "" || key.KeyHash == "" {
		return domain.ErrInvalid
	}

	if key.ID == "" {
		key.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if key.CreatedAt.IsZero() {
		key.CreatedAt = now
	}

	query := `
		INSERT INTO workspace_api_keys (id, workspace_id, name, key_hash, key_prefix, key_suffix, created_by, created_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := ks.db.Exec(ctx, query,
		key.ID,
		key.WorkspaceID,
		key.Name,
		key.KeyHash,
		key.KeyPrefix,
		key.KeySuffix,
		key.CreatedBy,
		key.CreatedAt,
		key.RevokedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}

func (ks *apiKeyStore) List(ctx context.Context, workspaceID string) ([]domain.WorkspaceAPIKey, error) {
	if workspaceID == "" {
		return []domain.WorkspaceAPIKey{}, nil
	}

	query := `
		SELECT id, workspace_id, name, key_hash, key_prefix, key_suffix, created_by, created_at, revoked_at
		FROM workspace_api_keys
		WHERE workspace_id = $1
		ORDER BY created_at DESC, id ASC
	`
	rows, err := ks.db.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, convertError(err)
	}
	defer rows.Close()

	keys := make([]domain.WorkspaceAPIKey, 0)
	for rows.Next() {
		var k domain.WorkspaceAPIKey
		if err := rows.Scan(
			&k.ID,
			&k.WorkspaceID,
			&k.Name,
			&k.KeyHash,
			&k.KeyPrefix,
			&k.KeySuffix,
			&k.CreatedBy,
			&k.CreatedAt,
			&k.RevokedAt,
		); err != nil {
			return nil, convertError(err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, convertError(err)
	}
	return keys, nil
}

func (ks *apiKeyStore) Revoke(ctx context.Context, workspaceID, id string) error {
	if workspaceID == "" || id == "" {
		return domain.ErrNotFound
	}

	query := `
		UPDATE workspace_api_keys
		SET revoked_at = $3
		WHERE workspace_id = $1 AND id = $2 AND revoked_at IS NULL
	`
	tag, err := ks.db.Exec(ctx, query, workspaceID, id, time.Now().UTC())
	if err != nil {
		return convertError(err)
	}
	if tag.RowsAffected() == 0 {
		// Unknown id, a key belonging to another workspace, or already revoked.
		return domain.ErrNotFound
	}
	return nil
}

func (ks *apiKeyStore) LookupByHash(ctx context.Context, keyHash string) (*domain.WorkspaceAPIKey, error) {
	if keyHash == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT id, workspace_id, name, key_hash, key_prefix, key_suffix, created_by, created_at, revoked_at
		FROM workspace_api_keys
		WHERE key_hash = $1
	`
	var k domain.WorkspaceAPIKey
	err := ks.db.QueryRow(ctx, query, keyHash).Scan(
		&k.ID,
		&k.WorkspaceID,
		&k.Name,
		&k.KeyHash,
		&k.KeyPrefix,
		&k.KeySuffix,
		&k.CreatedBy,
		&k.CreatedAt,
		&k.RevokedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &k, nil
}
