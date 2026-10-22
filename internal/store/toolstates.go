package store

import "context"

// ToolStateStore persists the global enable state of the built-in tool
// catalog: one row per tool the user has explicitly toggled. A tool with no
// stored row is enabled — absence is the default-on state, so the catalog
// can grow without new rows appearing implicitly.
type ToolStateStore interface {
	// SetEnabled create-or-replaces the state row of the named tool.
	SetEnabled(ctx context.Context, name string, enabled bool) error
	// Enabled reports the global enable state of the named tool; a tool
	// with no stored row reports true (the default-on state).
	Enabled(ctx context.Context, name string) (bool, error)
	// List returns every stored state row ordered by name. The result is
	// never nil, and is not required to cover the full catalog.
	List(ctx context.Context) ([]ToolState, error)
}
