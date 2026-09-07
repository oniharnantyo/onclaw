package agents

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// EphemeralSessionAdapter is a no-store session adapter backing unbound
// (/v1) turns: session event appends are dropped, loads always return empty
// history (full replay has nothing to replay), and interrupt checkpoints are
// skipped. Nothing about an ephemeral run persists — resolution of an
// approval raised in an ephemeral run is not supported by design.
type EphemeralSessionAdapter struct{}

// NewEphemeralSessionAdapter creates the no-store adapter.
func NewEphemeralSessionAdapter() *EphemeralSessionAdapter {
	return &EphemeralSessionAdapter{}
}

// AppendEvents drops the records.
func (e *EphemeralSessionAdapter) AppendEvents(_ context.Context, _ string, _ []*adk.SessionEvent[*schema.AgenticMessage]) error {
	return nil
}

// LoadEvents always reports empty history.
func (e *EphemeralSessionAdapter) LoadEvents(_ context.Context, _ string, _ *adk.LoadSessionEventsRequest) (*adk.LoadSessionEventsResult[*schema.AgenticMessage], error) {
	return &adk.LoadSessionEventsResult[*schema.AgenticMessage]{
		Events: []*adk.SessionEvent[*schema.AgenticMessage]{},
	}, nil
}

// Get reports no checkpoint.
func (e *EphemeralSessionAdapter) Get(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, nil
}

// Set skips checkpoint writes.
func (e *EphemeralSessionAdapter) Set(_ context.Context, _ string, _ []byte) error {
	return nil
}

// Delete is a no-op.
func (e *EphemeralSessionAdapter) Delete(_ context.Context, _ string) error {
	return nil
}
