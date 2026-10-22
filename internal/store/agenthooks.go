package store

import "context"

// AgentHookStore manages the per-agent hook assignment links (design D4).
// Resolution per turn: the agent's assigned hooks by position first, then
// the unassigned (global tier) hooks.
type AgentHookStore interface {
	// Assign links a hook to an agent at the given position, replacing the
	// position of an existing pair. Assigning an absent agent or hook is
	// ErrNotFound.
	Assign(ctx context.Context, agentID, hookID string, position int) error
	// Unassign removes the link; ErrNotFound when absent.
	Unassign(ctx context.Context, agentID, hookID string) error
	// Assignments returns the agent's assignment rows ordered by position
	// then hook ID. The result is never nil.
	Assignments(ctx context.Context, agentID string) ([]AgentHook, error)
	// HooksForAgent returns the definitions of the hooks assigned to the
	// agent, in assignment order (position then name). The result is never
	// nil.
	HooksForAgent(ctx context.Context, agentID string) ([]Hook, error)
	// UnassignedHooks returns the definitions assigned to no agent — the
	// global tier of the resolution order (design D4: assigning a hook to
	// any agent removes it from the global tier for everyone) — ordered by
	// position then name. The result is never nil.
	UnassignedHooks(ctx context.Context) ([]Hook, error)
}
