package membus_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/membus"
	"github.com/oniharnantyo/onclaw/internal/memory"
)

// --- Fakes for worker tests ---

type fakeEpisodicStore struct {
	pruneCalls atomic.Int64
}

func (f *fakeEpisodicStore) AppendEpisodic(ctx context.Context, agent, summary, l0Abstract, keyTopics, sourceID, expiresAt string) (int64, error) {
	return 1, nil
}
func (f *fakeEpisodicStore) ListUnpromoted(ctx context.Context, agent string) ([]*memory.EpisodicSummary, error) {
	return nil, nil
}
func (f *fakeEpisodicStore) CountUnpromoted(ctx context.Context, agent string) (int, error) {
	return 0, nil
}
func (f *fakeEpisodicStore) MarkPromoted(ctx context.Context, id int64) error { return nil }
func (f *fakeEpisodicStore) PruneExpired(ctx context.Context) (int64, error) {
	f.pruneCalls.Add(1)
	return 1, nil
}
func (f *fakeEpisodicStore) GetEpisodic(ctx context.Context, id int64) (*memory.EpisodicSummary, error) {
	return nil, nil
}

// --- Tests ---

func TestKGExtractionWorker_Subscribes(t *testing.T) {
	w := &membus.KGExtractionWorker{}
	subs := w.Subscribes()
	if len(subs) != 1 || subs[0] != "episode_created" {
		t.Errorf("expected [episode_created], got %v", subs)
	}
}

func TestKGExtractionWorker_NilKGStore(t *testing.T) {
	w := &membus.KGExtractionWorker{KGStore: nil}
	err := w.Handle(context.Background(), membus.EpisodeCreated{EpisodeID: 1, Summary: "test"})
	if err != nil {
		t.Errorf("expected nil error for nil KGStore, got %v", err)
	}
}

func TestKGExtractionWorker_WrongEvent(t *testing.T) {
	w := &membus.KGExtractionWorker{}
	err := w.Handle(context.Background(), membus.PruneTick{})
	if err == nil {
		t.Error("expected error for wrong event type")
	}
}

func TestDreamerWorker_Subscribes(t *testing.T) {
	w := &membus.DreamerWorker{}
	subs := w.Subscribes()
	if len(subs) != 1 || subs[0] != "episode_created" {
		t.Errorf("expected [episode_created], got %v", subs)
	}
}

func TestDreamerWorker_NilDreamer(t *testing.T) {
	w := &membus.DreamerWorker{Dreamer: nil}
	err := w.Handle(context.Background(), membus.EpisodeCreated{EpisodeID: 1})
	if err != nil {
		t.Errorf("expected nil error for nil Dreamer, got %v", err)
	}
}

func TestDreamerWorker_WrongEvent(t *testing.T) {
	w := &membus.DreamerWorker{}
	err := w.Handle(context.Background(), membus.PruneTick{})
	if err == nil {
		t.Error("expected error for wrong event type")
	}
}

func TestPrunerWorker_Subscribes(t *testing.T) {
	w := &membus.PrunerWorker{}
	subs := w.Subscribes()
	if len(subs) != 1 || subs[0] != "prune_tick" {
		t.Errorf("expected [prune_tick], got %v", subs)
	}
}

func TestPrunerWorker_Handle(t *testing.T) {
	store := &fakeEpisodicStore{}
	w := &membus.PrunerWorker{EpisodicStore: store}
	err := w.Handle(context.Background(), membus.PruneTick{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if store.pruneCalls.Load() != 1 {
		t.Errorf("expected 1 prune call, got %d", store.pruneCalls.Load())
	}
}

func TestPrunerWorker_NilStore(t *testing.T) {
	w := &membus.PrunerWorker{EpisodicStore: nil}
	err := w.Handle(context.Background(), membus.PruneTick{})
	if err != nil {
		t.Errorf("expected nil error for nil store, got %v", err)
	}
}

func TestPrunerWorker_WrongEvent(t *testing.T) {
	w := &membus.PrunerWorker{EpisodicStore: &fakeEpisodicStore{}}
	err := w.Handle(context.Background(), membus.EpisodeCreated{})
	if err == nil {
		t.Error("expected error for wrong event type")
	}
}

// TestBusIntegration_WorkerReceivesEvent verifies end-to-end bus delivery to a pruner worker.
func TestBusIntegration_WorkerReceivesEvent(t *testing.T) {
	store := &fakeEpisodicStore{}
	w := &membus.PrunerWorker{EpisodicStore: store}

	bus := membus.New(64)
	bus.Register(w)

	ctx := context.Background()
	bus.Start(ctx)

	bus.Publish(membus.PruneTick{})
	bus.Stop()

	if store.pruneCalls.Load() != 1 {
		t.Errorf("expected 1 prune call via bus, got %d", store.pruneCalls.Load())
	}
}

// Ensure event name constants match expectations.
func TestEventNames(t *testing.T) {
	tests := []struct {
		event membus.Event
		want  string
	}{
		{membus.EpisodeCreated{}, "episode_created"},
		{membus.DreamCompleted{}, "dream_completed"},
		{membus.PruneTick{}, "prune_tick"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.event), func(t *testing.T) {
			if got := tt.event.EventName(); got != tt.want {
				t.Errorf("EventName() = %q, want %q", got, tt.want)
			}
		})
	}
}
