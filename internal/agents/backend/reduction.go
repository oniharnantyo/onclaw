package backend

import (
	"context"
	"fmt"

	einofs "github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
)

// reductionBackend wraps an adkfs.Backend to satisfy reduction.Backend
// while enforcing workspace jail containment.
type reductionBackend struct {
	jail einofs.Backend
}

// Write writes the request to the jailed filesystem backend.
func (r *reductionBackend) Write(ctx context.Context, req *einofs.WriteRequest) error {
	if req == nil {
		return fmt.Errorf("write request cannot be nil")
	}
	return r.jail.Write(ctx, req)
}

// Jail returns the underlying filesystem jail backend.
func (r *reductionBackend) Jail() einofs.Backend {
	return r.jail
}

// NewReductionBackend creates a reduction.Backend backed by a jailed workspace.
// All writes are restricted to the primary agent workspace root, safely
// supporting mount-scoped paths (e.g. /workspace/...) and relative paths,
// while rejecting path escapes or host-absolute writes outside the jail.
func NewReductionBackend(agentDir string, extraRoots ...string) (reduction.Backend, error) {
	fs, err := NewFilesystemJailedWithRoots(agentDir, extraRoots...)
	if err != nil {
		return nil, fmt.Errorf("new reduction backend: %w", err)
	}
	return &reductionBackend{jail: fs}, nil
}
