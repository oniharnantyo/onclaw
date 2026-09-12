package store

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// WorkspaceStorageStore manages the per-workspace blob-storage configuration
// (design D16) — one row per workspace. Absence of a row means the workspace
// uses the instance-default local storage: Get returns domain.ErrNotFound
// and callers apply the default.
type WorkspaceStorageStore interface {
	Get(ctx context.Context, workspaceID string) (*domain.WorkspaceStorageConfig, error)
	// Upsert REPLACES the workspace's configuration wholesale (PUT semantics).
	Upsert(ctx context.Context, cfg *domain.WorkspaceStorageConfig) error
}
