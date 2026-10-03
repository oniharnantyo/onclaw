package ingest

// The queue mechanics moved verbatim from internal/memory (D1): bounded
// non-blocking enqueue with drop counting, stop semantics, per-session
// serialization, and per-consumer fault isolation.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// testJob is a direct-chat job shape (one human).
func testJob(sessionID, turnID string) Job {
	return Job{
		WorkspaceID:       "ws-1",
		AgentID:           "agent-1",
		UserID:            "user-1",
		SessionID:         sessionID,
		TurnID:            turnID,
		Origin:            OriginUser,
		HumanParticipants: 1,
		Status:            "completed",
	}
}

// recorder is a fake consumer: it records every job it receives and can be
// scripted to return an error or panic.
type recorder struct {
	name   string
	mu     sync.Mutex
	jobs   []Job
	err    error
	panics bool
}

func (r *recorder) Ingest(_ context.Context, job Job) error {
	r.mu.Lock()
	r.jobs = append(r.jobs, job)
	r.mu.Unlock()
	if r.panics {
		panic("boom from " + r.name)
	}
	return r.err
}

func (r *recorder) received() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Job(nil), r.jobs...)
}

// ConsumerFunc adapts a function to the Consumer interface (test helper).
type ConsumerFunc func(ctx context.Context, job Job) error

func (f ConsumerFunc) Ingest(ctx context.Context, job Job) error { return f(ctx, job) }

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

// TestWorkerQueueOverflowNeverBlocks: a full queue drops the job and counts
// it — Enqueue must return immediately even when nothing drains.
func TestWorkerQueueOverflowNeverBlocks(t *testing.T) {
	w := NewWorker(testLogger, WithQueueSize(1)) // never started — nothing drains

	start := time.Now()
	for i := 0; i < 4; i++ {
		w.Enqueue(testJob("sess-1", "turn-1"))
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Enqueue blocked on a full queue for %v", elapsed)
	}
	stats := w.Stats()
	if stats.Enqueued != 1 {
		t.Fatalf("expected exactly 1 enqueued, got %+v", stats)
	}
	if stats.QueueDropped != 3 {
		t.Fatalf("expected 3 queue drops, got %+v", stats)
	}
}

// TestWorkerEnqueueAfterStopDrops: a stopped worker counts late enqueues as
// dropped instead of parking them in the queue.
func TestWorkerEnqueueAfterStopDrops(t *testing.T) {
	w := NewWorker(testLogger, WithQueueSize(4))
	w.Start(context.Background())
	w.Stop()

	w.Enqueue(testJob("sess-1", "turn-1"))
	stats := w.Stats()
	if stats.Enqueued != 0 || stats.QueueDropped != 1 {
		t.Fatalf("expected the post-stop enqueue to drop, got %+v", stats)
	}
}

// TestWorkerDispatchesToEveryConsumerInOrder: every registered consumer
// receives the same job, in registration order.
func TestWorkerDispatchesToEveryConsumerInOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	recorder := func(name string) ConsumerFunc {
		return ConsumerFunc(func(_ context.Context, _ Job) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			return nil
		})
	}
	w := NewWorker(testLogger,
		WithConsumers(recorder("first"), recorder("second")),
		WithConsumers(recorder("third")),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob("sess-1", "turn-1"))
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job")

	mu.Lock()
	defer mu.Unlock()
	want := []string{"first", "second", "third"}
	if len(order) != len(want) {
		t.Fatalf("expected all three consumers to receive the job, got %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("consumers must dispatch in registration order, got %v", order)
		}
	}
}

// TestConsumerPanicOrErrorDoesNotStarveOthers (the fault-isolation contract):
// one consumer panics and another returns an error — the remaining consumers
// still receive the same job, and the dispatch still completes.
func TestConsumerPanicOrErrorDoesNotStarveOthers(t *testing.T) {
	panicking := &recorder{name: "panicking", panics: true}
	failing := &recorder{name: "failing", err: errors.New("consumer down")}
	clean := &recorder{name: "clean"}
	w := NewWorker(testLogger, WithConsumers(panicking, failing, clean))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob("sess-1", "turn-1"))
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 1 }, "worker did not process the job despite the panicking consumer")

	for _, c := range []*recorder{panicking, failing, clean} {
		if got := c.received(); len(got) != 1 {
			t.Fatalf("consumer %q must still receive the job, got %d", c.name, len(got))
		}
	}
}

// TestConsumerPanicDoesNotStarveOthersOnLaterJobs: a consumer that panics on
// every job must not wedge the queue — later jobs still reach the other
// consumers.
func TestConsumerPanicDoesNotStarveOthersOnLaterJobs(t *testing.T) {
	panicking := &recorder{name: "panicking", panics: true}
	clean := &recorder{name: "clean"}
	w := NewWorker(testLogger, WithConsumers(panicking, clean))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob("sess-1", "turn-1"))
	w.Enqueue(testJob("sess-2", "turn-2"))
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 2 }, "worker did not process both jobs despite the panicking consumer")

	if got := len(clean.received()); got != 2 {
		t.Fatalf("the healthy consumer must receive every job, got %d", got)
	}
}

// TestWorkerSerializesSameSession: two jobs on one session never run
// concurrently — the second starts only after the first finished (the
// incremental-cursor invariant the serialization protects).
func TestWorkerSerializesSameSession(t *testing.T) {
	var mu sync.Mutex
	var timeline []string
	release := make(chan struct{})
	tracing := ConsumerFunc(func(_ context.Context, job Job) error {
		mu.Lock()
		timeline = append(timeline, "start:"+job.TurnID)
		mu.Unlock()
		if job.TurnID == "turn-1" {
			<-release
		}
		mu.Lock()
		timeline = append(timeline, "end:"+job.TurnID)
		mu.Unlock()
		return nil
	})
	w := NewWorker(testLogger, WithConsumers(tracing), WithConcurrency(2))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(testJob("sess-1", "turn-1"))
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(timeline) == 1
	}, "the first job never started")

	// A second job on the SAME session must queue behind the held session
	// lock even though a drain goroutine is free.
	w.Enqueue(testJob("sess-1", "turn-2"))
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(timeline) != 1 {
		mu.Unlock()
		t.Fatalf("the same-session job must not start while the first holds the session lock, got %v", timeline)
	}
	mu.Unlock()

	close(release)
	waitFor(t, 5*time.Second, func() bool { return w.Stats().Processed == 2 }, "worker did not process both jobs")

	mu.Lock()
	defer mu.Unlock()
	want := []string{"start:turn-1", "end:turn-1", "start:turn-2", "end:turn-2"}
	if len(timeline) != len(want) {
		t.Fatalf("expected the serialized timeline %v, got %v", want, timeline)
	}
	for i := range want {
		if timeline[i] != want[i] {
			t.Fatalf("same-session jobs must serialize, got %v", timeline)
		}
	}
}
