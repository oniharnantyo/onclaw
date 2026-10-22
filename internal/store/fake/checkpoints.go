package fake

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/store"
)

type checkpointStore struct{ s *Store }

func (c checkpointStore) Get(_ context.Context, checkpointID string) ([]byte, bool, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()

	data, ok := c.s.checkpoints[checkpointID]
	if !ok {
		return nil, false, nil
	}
	return cloneBytes(data), true, nil
}

func (c checkpointStore) Set(_ context.Context, checkpointID string, data []byte) error {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()

	c.s.checkpoints[checkpointID] = cloneBytes(data)
	return nil
}

func (c checkpointStore) Delete(_ context.Context, checkpointID string) error {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()

	if _, ok := c.s.checkpoints[checkpointID]; !ok {
		return fmt.Errorf("checkpoint %s: %w", checkpointID, store.ErrNotFound)
	}
	delete(c.s.checkpoints, checkpointID)
	return nil
}
