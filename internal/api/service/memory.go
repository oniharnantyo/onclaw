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
