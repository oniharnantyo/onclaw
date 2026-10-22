package fake

import (
	"context"
	"fmt"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type hookStore struct{ s *Store }

func (h hookStore) Create(_ context.Context, hook *store.Hook) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	if hook.ID == "" {
		hook.ID = nextID()
	}
	if _, exists := h.s.hooks[hook.ID]; exists {
		return fmt.Errorf("hook %s already exists", hook.ID)
	}
	stamp(&hook.CreatedAt, &hook.UpdatedAt)
	h.s.hooks[hook.ID] = *hook
	return nil
}

func (h hookStore) List(_ context.Context) ([]store.Hook, error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	out := make([]store.Hook, 0, len(h.s.hooks))
	for _, hook := range h.s.hooks {
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

func (h hookStore) Update(_ context.Context, hook *store.Hook) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	stored, ok := h.s.hooks[hook.ID]
	if !ok {
		return fmt.Errorf("hook %s: %w", hook.ID, store.ErrNotFound)
	}
	hook.CreatedAt = stored.CreatedAt
	hook.UpdatedAt = nowUTC()
	h.s.hooks[hook.ID] = *hook
	return nil
}

func (h hookStore) Delete(_ context.Context, id string) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	if _, ok := h.s.hooks[id]; !ok {
		return fmt.Errorf("hook %s: %w", id, store.ErrNotFound)
	}
	delete(h.s.hooks, id)
	// Mirrors the sqlite FK cascade: the hook's assignments go too (its
	// execution audit rows are kept).
	h.s.deleteHookAssignments(id)
	return nil
}
