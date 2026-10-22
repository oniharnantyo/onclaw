package store

import "context"

// HookExecutionStore appends to and reads the hook execution audit trail:
// every hook evaluation is recorded with its outcome and duration (the
// agent-hooks spec's execution audit requirement).
type HookExecutionStore interface {
	// Record appends one audit row, assigning ID and stamping OccurredAt
	// when empty.
	Record(ctx context.Context, execution *HookExecution) error
	// List returns the audit trail, newest first. The result is never nil.
	List(ctx context.Context) ([]HookExecution, error)
}
