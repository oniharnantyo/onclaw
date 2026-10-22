package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CheckpointID returns the deterministic checkpoint key of a session: the
// checkpoint store is keyed by id, and a session owns exactly one checkpoint
// (each compaction supersedes the previous one), so the session id alone
// addresses it. Compaction markers reference the same id.
func CheckpointID(sessionID string) string {
	return "checkpoint_" + sessionID
}

func (s *service) SaveCheckpoint(ctx context.Context, sessionID string, cp Checkpoint) error {
	cp.ID = CheckpointID(sessionID)
	cp.SessionID = sessionID
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}

	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("save checkpoint: marshal: %w", err)
	}
	if err := s.store.Checkpoints().Set(ctx, cp.ID, data); err != nil {
		return fmt.Errorf("save checkpoint %s: %w", cp.ID, err)
	}
	return nil
}

func (s *service) Checkpoint(ctx context.Context, sessionID string) (Checkpoint, bool, error) {
	id := CheckpointID(sessionID)

	data, ok, err := s.store.Checkpoints().Get(ctx, id)
	if err != nil {
		return Checkpoint{}, false, fmt.Errorf("load checkpoint %s: %w", id, err)
	}
	if !ok {
		return Checkpoint{}, false, nil
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return Checkpoint{}, false, fmt.Errorf("load checkpoint %s: decode: %w", id, err)
	}
	return cp, true, nil
}
