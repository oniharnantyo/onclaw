package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

// agentHookKey is the (agent, hook) pair an assignment links.
type agentHookKey struct{ agentID, hookID string }

type agentHookStore struct{ s *Store }

func (h agentHookStore) Assign(_ context.Context, agentID, hookID string, position int) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	// Mirrors the sqlite adapter: an assignment referencing an absent agent
	// or hook is the store sentinel, not a silent insert.
	if _, ok := h.s.agents[agentID]; !ok {
		return fmt.Errorf("assign hook %s to agent %s: %w", hookID, agentID, store.ErrNotFound)
	}
	if _, ok := h.s.hooks[hookID]; !ok {
		return fmt.Errorf("assign hook %s to agent %s: %w", hookID, agentID, store.ErrNotFound)
	}
	h.s.agentHooks[agentHookKey{agentID, hookID}] = position
	return nil
}

func (h agentHookStore) Unassign(_ context.Context, agentID, hookID string) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	key := agentHookKey{agentID, hookID}
	if _, ok := h.s.agentHooks[key]; !ok {
		return fmt.Errorf("unassign hook %s from agent %s: %w", hookID, agentID, store.ErrNotFound)
	}
	delete(h.s.agentHooks, key)
	return nil
}

func (h agentHookStore) Assignments(_ context.Context, agentID string) ([]store.AgentHook, error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	out := make([]store.AgentHook, 0)
	for key, position := range h.s.agentHooks {
		if key.agentID != agentID {
			continue
		}
		out = append(out, store.AgentHook{AgentID: agentID, HookID: key.hookID, Position: position})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].HookID < out[j].HookID
	})
	return out, nil
}

func (h agentHookStore) HooksForAgent(_ context.Context, agentID string) ([]store.Hook, error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	out := make([]store.Hook, 0)
	for key := range h.s.agentHooks {
		if key.agentID != agentID {
			continue
		}
		hook, ok := h.s.hooks[key.hookID]
		if !ok {
			continue // unreachable: deletes cascade (see deleteHookAssignments)
		}
		out = append(out, hook)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (h agentHookStore) UnassignedHooks(_ context.Context) ([]store.Hook, error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	assigned := make(map[string]bool, len(h.s.agentHooks))
	for key := range h.s.agentHooks {
		assigned[key.hookID] = true
	}
	out := make([]store.Hook, 0)
	for id, hook := range h.s.hooks {
		if assigned[id] {
			continue // assigned to some agent: leaves the global tier for everyone
		}
		out = append(out, hook)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// deleteAgentHooks removes every assignment of the agent — the cascade on
// agent delete. The caller must hold s.mu.
func (s *Store) deleteAgentHooks(agentID string) {
	for key := range s.agentHooks {
		if key.agentID == agentID {
			delete(s.agentHooks, key)
		}
	}
}

// deleteHookAssignments removes every assignment of the hook — the cascade
// on hook delete. The caller must hold s.mu.
func (s *Store) deleteHookAssignments(hookID string) {
	for key := range s.agentHooks {
		if key.hookID == hookID {
			delete(s.agentHooks, key)
		}
	}
}
