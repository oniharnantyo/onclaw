package fake

import (
	"context"
	"sort"

	"github.com/oniharnantyo/buddies/internal/store"
)

type hookExecutionStore struct{ s *Store }

func (h hookExecutionStore) Record(_ context.Context, execution *store.HookExecution) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	if execution.ID == "" {
		execution.ID = nextID()
	}
	if execution.OccurredAt.IsZero() {
		execution.OccurredAt = nowUTC()
	}
	h.s.hookExecutions = append(h.s.hookExecutions, *execution)
	return nil
}

func (h hookExecutionStore) List(_ context.Context) ([]store.HookExecution, error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()

	// Newest first: reverse insertion order, then stable-sort by time so
	// equal timestamps keep the newest-inserted first — the same
	// (occurred_at DESC, rowid DESC) order the sqlite adapter returns.
	out := make([]store.HookExecution, 0, len(h.s.hookExecutions))
	for i := len(h.s.hookExecutions) - 1; i >= 0; i-- {
		out = append(out, h.s.hookExecutions[i])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].OccurredAt.After(out[j].OccurredAt)
	})
	return out, nil
}
