package membus

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/memory"
)

// DreamerWorker subscribes to episode_created events and triggers
// the Dreamer's consolidation logic (MaybeDream) asynchronously.
type DreamerWorker struct {
	Dreamer *memory.Dreamer
}

func (w *DreamerWorker) Subscribes() []string {
	return []string{"episode_created"}
}

func (w *DreamerWorker) Handle(ctx context.Context, event Event) error {
	_, ok := event.(EpisodeCreated)
	if !ok {
		return fmt.Errorf("dreamer_worker: unexpected event type %T", event)
	}

	if w.Dreamer == nil {
		return nil
	}

	return w.Dreamer.MaybeDream(ctx)
}
