package store

import "context"

// AgentStore manages agent definitions.
type AgentStore interface {
	// Create stores a new agent, assigning ID and stamping timestamps when
	// empty. The slug is unique among agents: a duplicate is rejected. An
	// empty PromptsStatus is stamped ready, mirroring the app-managed
	// timestamp convention.
	Create(ctx context.Context, agent *Agent) error
	// ByID returns the agent with the given ID; ErrNotFound when absent.
	ByID(ctx context.Context, id string) (*Agent, error)
	// List returns all agents, oldest first. The result is never nil.
	List(ctx context.Context) ([]Agent, error)
	// Update rewrites an existing agent; ErrNotFound when absent.
	Update(ctx context.Context, agent *Agent) error
	// Delete removes an agent; ErrNotFound when absent.
	Delete(ctx context.Context, id string) error
}
