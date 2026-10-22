package store

import "context"

// CheckpointStore manages session compaction checkpoints: opaque blobs
// keyed by checkpoint ID, addressed by the agent runtime.
type CheckpointStore interface {
	// Get returns the checkpoint data; (nil, false, nil) when absent.
	Get(ctx context.Context, checkpointID string) (data []byte, ok bool, err error)
	// Set create-or-replaces the checkpoint's data.
	Set(ctx context.Context, checkpointID string, data []byte) error
	// Delete removes a checkpoint; ErrNotFound when absent.
	Delete(ctx context.Context, checkpointID string) error
}
