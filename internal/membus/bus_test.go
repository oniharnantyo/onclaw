package membus_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/membus"
)

// testEvent is a simple event for testing.
type testEvent struct {
	name string
}

func (e testEvent) EventName() string { return e.name }

// countingWorker counts how many events it handles.
type countingWorker struct {
	events     []string
	count      atomic.Int64
	handleFunc func(ctx context.Context, event membus.Event) error
	mu         sync.Mutex
	subs       []string
}

func newCountingWorker(subs ...string) *countingWorker {
	return &countingWorker{subs: subs}
}

func (w *countingWorker) Subscribes() []string { return w.subs }

func (w *countingWorker) Handle(ctx context.Context, event membus.Event) error {
	w.count.Add(1)
	w.mu.Lock()
	w.events = append(w.events, event.EventName())
	w.mu.Unlock()
	if w.handleFunc != nil {
		return w.handleFunc(ctx, event)
	}
	return nil
}

func (w *countingWorker) Count() int64 {
	return w.count.Load()
}

func TestBus_PublishSubscribe(t *testing.T) {
	bus := membus.New(64)
	worker := newCountingWorker("test_event")
	bus.Register(worker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	bus.Publish(testEvent{name: "test_event"})

	// Wait for delivery.
	deadline := time.After(2 * time.Second)
	for worker.Count() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for event delivery")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if worker.Count() != 1 {
		t.Errorf("expected 1 event, got %d", worker.Count())
	}

	bus.Stop()
}

func TestBus_FanOut(t *testing.T) {
	bus := membus.New(64)
	w1 := newCountingWorker("episode_created")
	w2 := newCountingWorker("episode_created")
	bus.Register(w1)
	bus.Register(w2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	bus.Publish(membus.EpisodeCreated{AgentName: "test", EpisodeID: 1, Summary: "s"})

	deadline := time.After(2 * time.Second)
	for w1.Count() == 0 || w2.Count() == 0 {
		select {
		case <-deadline:
			t.Fatalf("timed out: w1=%d, w2=%d", w1.Count(), w2.Count())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if w1.Count() != 1 || w2.Count() != 1 {
		t.Errorf("expected both workers to get 1 event, got w1=%d, w2=%d", w1.Count(), w2.Count())
	}

	bus.Stop()
}

func TestBus_SubscriptionFilter(t *testing.T) {
	bus := membus.New(64)
	epWorker := newCountingWorker("episode_created")
	pruneWorker := newCountingWorker("prune_tick")
	bus.Register(epWorker)
	bus.Register(pruneWorker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	bus.Publish(membus.EpisodeCreated{AgentName: "test", EpisodeID: 1, Summary: "s"})

	deadline := time.After(2 * time.Second)
	for epWorker.Count() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for episode worker")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Give pruneWorker a moment to (incorrectly) receive.
	time.Sleep(50 * time.Millisecond)

	if pruneWorker.Count() != 0 {
		t.Errorf("prune worker should not receive episode_created, got %d", pruneWorker.Count())
	}

	bus.Stop()
}

func TestBus_NonBlockingPublish_DropOnFull(t *testing.T) {
	// Buffer of 1 with a slow worker that blocks.
	bus := membus.New(1)
	blocker := newCountingWorker("test_event")
	blocker.handleFunc = func(ctx context.Context, event membus.Event) error {
		time.Sleep(500 * time.Millisecond)
		return nil
	}
	bus.Register(blocker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	// Fill the bus buffer and the worker channel: publish many events quickly.
	// With buffer=1 for bus + buffer=1 for worker channel, we can hold 2 events max.
	// Additional publishes should be non-blocking (dropped).
	for i := 0; i < 10; i++ {
		bus.Publish(testEvent{name: "test_event"})
	}

	// This should not block — verify by completing within timeout.
	done := make(chan struct{})
	go func() {
		bus.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop() blocked — non-blocking publish may have failed")
	}
}

func TestBus_GracefulShutdown_Drain(t *testing.T) {
	bus := membus.New(64)
	worker := newCountingWorker("test_event")
	bus.Register(worker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	// Publish several events.
	for i := 0; i < 5; i++ {
		bus.Publish(testEvent{name: "test_event"})
	}

	// Stop should drain all buffered events.
	bus.Stop()

	if worker.Count() != 5 {
		t.Errorf("expected 5 events after drain, got %d", worker.Count())
	}
}

func TestBus_WorkerError_NonFatal(t *testing.T) {
	bus := membus.New(64)
	failWorker := newCountingWorker("test_event")
	failWorker.handleFunc = func(ctx context.Context, event membus.Event) error {
		return context.DeadlineExceeded // simulate error
	}

	successWorker := newCountingWorker("test_event")
	bus.Register(failWorker)
	bus.Register(successWorker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	bus.Publish(testEvent{name: "test_event"})
	bus.Publish(testEvent{name: "test_event"})

	deadline := time.After(2 * time.Second)
	for successWorker.Count() < 2 {
		select {
		case <-deadline:
			t.Fatalf("timed out: success=%d, fail=%d", successWorker.Count(), failWorker.Count())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Both workers should have processed both events regardless of errors.
	if failWorker.Count() != 2 {
		t.Errorf("fail worker should have processed 2 events, got %d", failWorker.Count())
	}
	if successWorker.Count() != 2 {
		t.Errorf("success worker should have processed 2 events, got %d", successWorker.Count())
	}

	bus.Stop()
}

func TestBus_SequentialPerWorker(t *testing.T) {
	bus := membus.New(64)
	var order []int
	var mu sync.Mutex

	worker := newCountingWorker("test_event")
	worker.handleFunc = func(ctx context.Context, event membus.Event) error {
		mu.Lock()
		order = append(order, int(worker.Count()))
		mu.Unlock()
		time.Sleep(10 * time.Millisecond) // simulate work
		return nil
	}
	bus.Register(worker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus.Start(ctx)

	for i := 0; i < 5; i++ {
		bus.Publish(testEvent{name: "test_event"})
	}

	bus.Stop()

	// Verify sequential: each count should increase monotonically.
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(order); i++ {
		if order[i] <= order[i-1] {
			t.Errorf("events not processed sequentially: order[%d]=%d <= order[%d]=%d", i, order[i], i-1, order[i-1])
		}
	}
}
