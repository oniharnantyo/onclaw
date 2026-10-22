package store

import "context"

// HookStore manages lifecycle hook definitions.
type HookStore interface {
	// Create stores a new hook, assigning ID and stamping timestamps when
	// empty. A duplicate hook name is an error.
	Create(ctx context.Context, hook *Hook) error
	// List returns all hooks ordered by Position then name. The result is
	// never nil.
	List(ctx context.Context) ([]Hook, error)
	// Update rewrites an existing hook; ErrNotFound when absent.
	Update(ctx context.Context, hook *Hook) error
	// Delete removes a hook; ErrNotFound when absent.
	Delete(ctx context.Context, id string) error
}
