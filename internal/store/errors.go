package store

import "errors"

// ErrNotFound is returned by store implementations when a requested row does
// not exist (or a mutation targets an absent row). Implementations MUST wrap
// it with %w so callers can test with errors.Is, and MUST NOT leak
// driver-specific error types across the interface boundary.
var ErrNotFound = errors.New("not found")

// ErrDuplicate is returned by store implementations when a unique constraint
// violation occurs (e.g. UNIQUE(provider_id, model_id)). Implementations MUST wrap
// it with %w so callers can test with errors.Is.
var ErrDuplicate = errors.New("duplicate")
