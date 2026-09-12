package postgres

import (
	"context"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	storeport "github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceStorageStore implements storeport.WorkspaceStorageStore for
// PostgreSQL (add-chat-attachments D16): one row per workspace; absence of a
// row means the workspace uses the instance-default local storage. Upsert is
// a single statement with wholesale replacement (PUT semantics). The
// secret_access_key column round-trips the SEALED envelope opaquely —
// sealing/unsealing is the caller's job (handlers seal on write, the storage
// resolver unseals when building the driver); this store never encrypts or
// decrypts.
type workspaceStorageStore struct {
	db Executor
}

// NewWorkspaceStorageStore creates a new WorkspaceStorageStore with the given database executor.
func NewWorkspaceStorageStore(db Executor) storeport.WorkspaceStorageStore {
	return &workspaceStorageStore{db: db}
}

func (ws *workspaceStorageStore) Get(ctx context.Context, workspaceID string) (*domain.WorkspaceStorageConfig, error) {
	if workspaceID == "" {
		return nil, domain.ErrNotFound
	}

	query := `
		SELECT workspace_id, driver, endpoint, region, bucket, access_key_id,
		       secret_access_key, use_path_style, updated_at
		FROM workspace_storage
		WHERE workspace_id = $1
	`
	var cfg domain.WorkspaceStorageConfig
	err := ws.db.QueryRow(ctx, query, workspaceID).Scan(
		&cfg.WorkspaceID,
		&cfg.Driver,
		&cfg.Endpoint,
		&cfg.Region,
		&cfg.Bucket,
		&cfg.AccessKeyID,
		&cfg.SecretAccessKey,
		&cfg.UsePathStyle,
		&cfg.UpdatedAt,
	)
	if err != nil {
		return nil, convertError(err)
	}
	return &cfg, nil
}

// Upsert REPLACES the workspace's configuration wholesale: every column is
// rewritten from the given config on conflict (PUT semantics), including
// emptying fields the new config leaves unset. UpdatedAt is app-managed
// (store invariant 4).
func (ws *workspaceStorageStore) Upsert(ctx context.Context, cfg *domain.WorkspaceStorageConfig) error {
	if cfg == nil || cfg.WorkspaceID == "" || cfg.Driver == "" {
		return domain.ErrInvalid
	}

	if cfg.UpdatedAt.IsZero() {
		cfg.UpdatedAt = time.Now().UTC()
	}

	query := `
		INSERT INTO workspace_storage (workspace_id, driver, endpoint, region, bucket, access_key_id, secret_access_key, use_path_style, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (workspace_id) DO UPDATE
		SET driver = EXCLUDED.driver,
		    endpoint = EXCLUDED.endpoint,
		    region = EXCLUDED.region,
		    bucket = EXCLUDED.bucket,
		    access_key_id = EXCLUDED.access_key_id,
		    secret_access_key = EXCLUDED.secret_access_key,
		    use_path_style = EXCLUDED.use_path_style,
		    updated_at = EXCLUDED.updated_at
	`
	_, err := ws.db.Exec(ctx, query,
		cfg.WorkspaceID,
		cfg.Driver,
		cfg.Endpoint,
		cfg.Region,
		cfg.Bucket,
		cfg.AccessKeyID,
		cfg.SecretAccessKey,
		cfg.UsePathStyle,
		cfg.UpdatedAt,
	)
	if err != nil {
		return convertError(err)
	}
	return nil
}
