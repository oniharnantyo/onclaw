package fake

import (
	"context"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type toolStateStore struct{ s *Store }

func (r toolStateStore) SetEnabled(_ context.Context, name string, enabled bool) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	existing, exists := r.s.toolStates[name]
	now := nowUTC()
	state := store.ToolState{Name: name, Enabled: enabled, UpdatedAt: now}
	if exists {
		state.CreatedAt = existing.CreatedAt
	} else {
		state.CreatedAt = now
	}
	r.s.toolStates[name] = state
	return nil
}

func (r toolStateStore) Enabled(_ context.Context, name string) (bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	state, ok := r.s.toolStates[name]
	if !ok {
		// No stored row: the default-on state (see the port contract).
		return true, nil
	}
	return state.Enabled, nil
}

func (r toolStateStore) List(_ context.Context) ([]store.ToolState, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	out := make([]store.ToolState, 0, len(r.s.toolStates))
	for _, state := range r.s.toolStates {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
