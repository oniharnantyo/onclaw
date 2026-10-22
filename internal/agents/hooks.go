package agents

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The hooks seam (spec agent-hooks, design D4): one turn resolves its hook
// chain — the agent's assigned hooks by position, then the unassigned global
// tier — and dispatches the five lifecycle points through it. The dispatcher
// is a manager capability (WithHooks; the no-op default resolves an empty
// chain), and this file carries the adapter that satisfies the hooks
// package's Store seam over store.Store, so the hooks package itself speaks
// no store types (the mcp.go/skills.go adapter pattern).

// The terminal statuses the run_finished observer receives as data
// (hooks.Resolved.RunFinished).
const (
	hookRunStatusCompleted = "completed"
	hookRunStatusFailed    = "failed"
	hookRunStatusCancelled = "cancelled"
)

// NewHooksDispatcher builds the turn-time hook dispatcher over the store:
// resolution reads both assignment tiers fresh every turn through the
// store-backed seam, and both supported handler types are enabled (design
// D9 — the kill switches stay available to a future operator surface). The
// composition root injects it through WithHooks.
func NewHooksDispatcher(s store.Store) *hooks.Dispatcher {
	return hooks.NewDispatcher(storeHooks{store: s}, hooks.NewRegistry())
}

// storeHooks adapts store.Store onto the hooks package's Store seam: the
// dispatcher speaks definitions and audit rows, never store types.
type storeHooks struct {
	store store.Store
}

// AssignedHooks returns the agent's assigned hook definitions (design D4),
// in assignment order.
func (s storeHooks) AssignedHooks(ctx context.Context, agentID string) ([]hooks.Definition, error) {
	rows, err := s.store.AgentHooks().HooksForAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("read assigned hooks: %w", err)
	}
	out := make([]hooks.Definition, 0, len(rows))
	for _, row := range rows {
		out = append(out, hookDefinition(row))
	}
	return out, nil
}

// GlobalHooks returns the unassigned definitions — the global tier of the
// resolution order (design D4).
func (s storeHooks) GlobalHooks(ctx context.Context) ([]hooks.Definition, error) {
	rows, err := s.store.AgentHooks().UnassignedHooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("read global hooks: %w", err)
	}
	out := make([]hooks.Definition, 0, len(rows))
	for _, row := range rows {
		out = append(out, hookDefinition(row))
	}
	return out, nil
}

// RecordExecution appends one audit row. Rows outlive the hook: the store
// keeps the denormalized hook id without validating it.
func (s storeHooks) RecordExecution(ctx context.Context, exec hooks.Execution) error {
	row := store.HookExecution{
		HookID:     exec.HookID,
		AgentID:    exec.AgentID,
		SessionID:  exec.SessionID,
		TurnID:     exec.TurnID,
		Event:      exec.Event,
		Outcome:    exec.Outcome,
		Error:      exec.Error,
		DurationMS: int(exec.DurationMS),
	}
	if err := s.store.HookExecutions().Record(ctx, &row); err != nil {
		return fmt.Errorf("record hook execution: %w", err)
	}
	return nil
}

// hookDefinition maps a stored hook row into the dispatcher's view.
func hookDefinition(row store.Hook) hooks.Definition {
	return hooks.Definition{
		ID:          row.ID,
		Name:        row.Name,
		Event:       row.Event,
		Matcher:     row.Matcher,
		HandlerType: row.HandlerType,
		Config:      row.Config,
		TimeoutMS:   row.TimeoutMS,
		OnFailure:   row.OnFailure,
		Enabled:     row.Enabled,
	}
}

// resolveTurnHooks resolves the turn's hook chain fresh (never cached at
// boot, design D4). A store read failure is infrastructure failure and fails
// the turn like any other store error.
func (m *manager) resolveTurnHooks(ctx context.Context, agent store.Agent) (*hooks.Resolved, error) {
	chain, err := m.hooks.Resolve(ctx, agent.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve hooks for agent %q: %w", agent.ID, err)
	}
	return chain, nil
}

// hookBaseEvent builds the per-run event identity each hook delivery clones:
// the run's origin (design D6 — who started the run; an empty origin reads
// as the user turn it defaults to), the agent ref, and the session/turn
// coordinates.
func hookBaseEvent(agent store.Agent, sessionID, turnID, origin string) hooks.Event {
	if origin == "" {
		origin = hooks.OriginUser
	}
	return hooks.Event{
		Origin:    origin,
		Agent:     hooks.EventRef{ID: agent.ID, Name: agent.Name},
		SessionID: sessionID,
		TurnID:    turnID,
	}
}

// blockedPromptNotice renders the user-visible notice a hook-blocked prompt
// ends with; the reason is the blocking hook's own words (spec agent-hooks:
// the block reason is visible in the transcript).
func blockedPromptNotice(hookName, reason string) string {
	return fmt.Sprintf("Prompt blocked by hook %q: %s", hookName, reason)
}
