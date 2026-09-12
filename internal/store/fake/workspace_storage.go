package fake

import (
	"context"
	"fmt"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// -------------------------------------------------------------------------
// WorkspaceStorageStore implementation (add-chat-attachments D16). One row
// per workspace; absence means the instance-default local storage applies.
// SecretAccessKey persists opaquely — the sealed envelope passes through
// byte-for-byte; sealing/unsealing belongs to the callers.
// -------------------------------------------------------------------------

func cloneWorkspaceStorageConfig(cfg *domain.WorkspaceStorageConfig) *domain.WorkspaceStorageConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}

type workspaceStorageStore struct {
	s *fakeStore
}

func (ws *workspaceStorageStore) Get(ctx context.Context, workspaceID string) (*domain.WorkspaceStorageConfig, error) {
	if workspaceID == "" {
		return nil, domain.ErrNotFound
	}

	ws.s.mu.RLock()
	defer ws.s.mu.RUnlock()

	cfg, exists := ws.s.workspaceStorage[workspaceID]
	if !exists {
		return nil, domain.ErrNotFound
	}
	return cloneWorkspaceStorageConfig(cfg), nil
}

func (ws *workspaceStorageStore) Upsert(ctx context.Context, cfg *domain.WorkspaceStorageConfig) error {
	if cfg == nil || cfg.WorkspaceID == "" || cfg.Driver == "" {
		return domain.ErrInvalid
	}

	ws.s.mu.Lock()
	defer ws.s.mu.Unlock()

	if _, exists := ws.s.workspaces[cfg.WorkspaceID]; !exists {
		return fmt.Errorf("%w: workspace not found", domain.ErrNotFound)
	}

	cfg.UpdatedAt = time.Now().UTC()
	ws.s.workspaceStorage[cfg.WorkspaceID] = cloneWorkspaceStorageConfig(cfg)
	return nil
}
