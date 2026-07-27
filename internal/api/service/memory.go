package service

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

// ListDreamSweeps returns parsed dreaming sweep records from the configured workspace.
func (s *Service) ListDreamSweeps(ctx context.Context) ([]*memory.DreamSweepRecord, error) {
	if s.workspacePath == "" {
		return []*memory.DreamSweepRecord{}, nil
	}
	files, err := memory.ListDreamFiles(s.workspacePath)
	if err != nil {
		return nil, fmt.Errorf("list dream files: %w", err)
	}
	var all []*memory.DreamSweepRecord
	for _, f := range files {
		records, err := memory.ParseDreamSweeps(f)
		if err != nil {
			continue
		}
		all = append(all, records...)
	}
	if all == nil {
		return []*memory.DreamSweepRecord{}, nil
	}
	return all, nil
}

// ListStagedWrites returns all staged memory writes across all agents.
func (s *Service) ListStagedWrites(ctx context.Context) ([]*memory.StagedWrite, error) {
	if s.stagedWriteStore == nil {
		return []*memory.StagedWrite{}, nil
	}
	writes, err := s.stagedWriteStore.ListStaged(ctx, "")
	if err != nil {
		return nil, err
	}
	if writes == nil {
		return []*memory.StagedWrite{}, nil
	}
	return writes, nil
}

// ApproveStagedWrite approves a staged memory write by ID.
func (s *Service) ApproveStagedWrite(ctx context.Context, id int64) error {
	if s.stagedWriteStore == nil {
		return fmt.Errorf("staged write store not configured")
	}
	return s.stagedWriteStore.ApproveWrite(ctx, id)
}

// RejectStagedWrite rejects a staged memory write by ID.
func (s *Service) RejectStagedWrite(ctx context.Context, id int64) error {
	if s.stagedWriteStore == nil {
		return fmt.Errorf("staged write store not configured")
	}
	return s.stagedWriteStore.RejectWrite(ctx, id)
}

// GetEmbeddingsConfig retrieves global embeddings configuration from preferences KV.
func (s *Service) GetEmbeddingsConfig(ctx context.Context) (*EmbeddingsConfig, error) {
	if s.kv == nil {
		return &EmbeddingsConfig{}, nil
	}
	provider, _ := s.kv.Get(ctx, "embedding_provider")
	model, _ := s.kv.Get(ctx, "embedding_model")
	apiBase, _ := s.kv.Get(ctx, "embedding_api_base")
	timeout, _ := s.kv.Get(ctx, "embedding_timeout")
	return &EmbeddingsConfig{
		Provider: provider,
		Model:    model,
		APIBase:  apiBase,
		Timeout:  timeout,
	}, nil
}

// SetEmbeddingsConfig updates global embeddings configuration in preferences KV.
func (s *Service) SetEmbeddingsConfig(ctx context.Context, cfg *EmbeddingsConfig) error {
	if s.kv == nil {
		return fmt.Errorf("kv store not configured")
	}
	if cfg == nil {
		return nil
	}
	if err := s.kv.Set(ctx, "embedding_provider", cfg.Provider); err != nil {
		return fmt.Errorf("set embedding_provider: %w", err)
	}
	if err := s.kv.Set(ctx, "embedding_model", cfg.Model); err != nil {
		return fmt.Errorf("set embedding_model: %w", err)
	}
	if err := s.kv.Set(ctx, "embedding_api_base", cfg.APIBase); err != nil {
		return fmt.Errorf("set embedding_api_base: %w", err)
	}
	if err := s.kv.Set(ctx, "embedding_timeout", cfg.Timeout); err != nil {
		return fmt.Errorf("set embedding_timeout: %w", err)
	}
	return nil
}
