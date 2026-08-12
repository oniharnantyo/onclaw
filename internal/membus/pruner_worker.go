package membus

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

// PrunerWorker subscribes to prune_tick events and prunes expired episodic summaries.
type PrunerWorker struct {
	EpisodicStore memory.EpisodicStore
}

func (w *PrunerWorker) Subscribes() []string {
	return []string{"prune_tick"}
}

func (w *PrunerWorker) Handle(ctx context.Context, event Event) error {
	_, ok := event.(PruneTick)
	if !ok {
		return fmt.Errorf("pruner_worker: unexpected event type %T", event)
	}

	if w.EpisodicStore == nil {
		return nil
	}

	n, err := w.EpisodicStore.PruneExpired(ctx)
	if err != nil {
		return fmt.Errorf("prune expired: %w", err)
	}
	if n > 0 {
		slog.Info("pruner_worker: pruned expired episodes", "count", n)
	}
	return nil
}
