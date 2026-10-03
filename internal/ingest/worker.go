package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultQueueSize   = 256
	defaultConcurrency = 2
	// stopGrace bounds Stop's wait for in-flight jobs; a job past the
	// deadline logs and the process proceeds (fail-soft to the shutdown).
	stopGrace = 30 * time.Second
)

// Worker drains turn-end jobs off a bounded queue and hands each to every
// registered consumer, in registration order. Enqueueing never blocks the
// caller and nothing a job does can fail a run. Jobs on the same session are
// serialized — consumers' incremental cursors are read-then-write — while
// different sessions process concurrently. Per-consumer fault isolation: one
// consumer's panic or error is logged and cannot prevent the other consumers
// from receiving the same job.
type Worker struct {
	consumers []Consumer
	log       *slog.Logger
	queue     chan Job
	queueSize int
	workers   int
	wg        sync.WaitGroup
	stopCh    chan struct{}
	stopOnce  sync.Once

	locksMu     sync.Mutex
	sessionLock map[string]*sync.Mutex

	enqueued  atomic.Int64
	processed atomic.Int64
	dropped   atomic.Int64
}

// WorkerOption configures the worker; every knob has a safe default.
type WorkerOption func(*Worker)

// WithConsumers registers the consumers that receive every drained job, in
// registration (dispatch) order. Repeatable: each call appends.
func WithConsumers(consumers ...Consumer) WorkerOption {
	return func(w *Worker) {
		w.consumers = append(w.consumers, consumers...)
	}
}

// WithQueueSize caps the ingest queue (default 256). The queue exists to
// absorb bursts, not to gate the turn path — overflow drops.
func WithQueueSize(n int) WorkerOption {
	return func(w *Worker) {
		if n > 0 {
			w.queueSize = n
		}
	}
}

// WithConcurrency sets how many background goroutines drain the queue
// (default 2).
func WithConcurrency(n int) WorkerOption {
	return func(w *Worker) {
		if n > 0 {
			w.workers = n
		}
	}
}

// NewWorker constructs the worker from its logger and options. The
// composition root registers the consumers (WithConsumers) in dispatch
// order; a worker without consumers drains jobs as quiet no-ops.
func NewWorker(log *slog.Logger, opts ...WorkerOption) *Worker {
	w := &Worker{
		log:         log,
		queueSize:   defaultQueueSize,
		workers:     defaultConcurrency,
		stopCh:      make(chan struct{}),
		sessionLock: make(map[string]*sync.Mutex),
	}
	for _, opt := range opts {
		opt(w)
	}
	// Sized after the options so WithQueueSize takes effect.
	w.queue = make(chan Job, w.queueSize)
	return w
}

// sessionMutex returns the per-session serialization lock: two jobs on one
// session must not dispatch concurrently or a consumer's incremental cursor
// (read-then-write) processes the same material twice. Different sessions
// never contend.
func (w *Worker) sessionMutex(workspaceID, sessionID string) *sync.Mutex {
	w.locksMu.Lock()
	defer w.locksMu.Unlock()
	key := workspaceID + "/" + sessionID
	mu, ok := w.sessionLock[key]
	if !ok {
		mu = &sync.Mutex{}
		w.sessionLock[key] = mu
	}
	return mu
}

// Start launches the drain goroutines under the process-lifetime context:
// the loops exit when ctx is cancelled or Stop is called; in-flight jobs are
// not interrupted — Stop waits for them (the scheduler/heartbeat lifecycle).
func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < w.workers; i++ {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-w.stopCh:
					return
				case job := <-w.queue:
					w.drain(ctx, job)
				}
			}
		}()
	}
}

// Stop halts enqueue acceptance and waits for in-flight jobs to reach their
// terminal outcome, bounded by the stop grace. Queued-but-unstarted jobs are
// abandoned — consumers reprocess from their own raw sources. Idempotent.
func (w *Worker) Stop() {
	w.stopOnce.Do(func() { close(w.stopCh) })
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(stopGrace)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		w.log.Warn("ingest: stop deadline exceeded with jobs still in flight")
	}
}

// Enqueue submits one turn for background ingestion. It never blocks the
// caller: a full queue (or a stopped worker) drops the job with a warning
// and a QueueDropped count — the enqueue sits on the turn path and must
// never stall a turn.
func (w *Worker) Enqueue(job Job) {
	select {
	case <-w.stopCh:
		w.dropped.Add(1)
		return
	default:
	}
	select {
	case w.queue <- job:
		w.enqueued.Add(1)
	default:
		w.dropped.Add(1)
		w.log.Warn("ingest: queue full; dropping job",
			"workspace_id", job.WorkspaceID,
			"session_id", job.SessionID,
			"turn_id", job.TurnID)
	}
}

// Stats is the worker's monotonic counter snapshot — the queue-level
// counters. Per-consumer pipeline counters (extraction failures and the
// like) live on the consumers themselves.
type Stats struct {
	Enqueued     int64
	Processed    int64
	QueueDropped int64
}

// Stats returns the current counters.
func (w *Worker) Stats() Stats {
	return Stats{
		Enqueued:     w.enqueued.Load(),
		Processed:    w.processed.Load(),
		QueueDropped: w.dropped.Load(),
	}
}

// drain dispatches one job to every registered consumer in registration
// order, holding the session lock for the whole dispatch so the consumers'
// incremental cursors never race, and counts the job processed only after
// every consumer returned.
func (w *Worker) drain(ctx context.Context, job Job) {
	mutex := w.sessionMutex(job.WorkspaceID, job.SessionID)
	mutex.Lock()
	defer mutex.Unlock()

	for _, c := range w.consumers {
		w.dispatch(ctx, c, job)
	}
	w.processed.Add(1)
}

// dispatch hands one job to one consumer, isolating faults: a panic is
// recovered and an error return is logged — neither can prevent the
// consumers after this one from receiving the same job (D1's per-consumer
// fault isolation).
func (w *Worker) dispatch(ctx context.Context, c Consumer, job Job) {
	defer func() {
		if r := recover(); r != nil {
			w.log.Error("ingest: consumer panic recovered; remaining consumers still receive the job",
				"consumer", fmt.Sprintf("%T", c),
				"workspace_id", job.WorkspaceID,
				"session_id", job.SessionID,
				"turn_id", job.TurnID,
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	if err := c.Ingest(ctx, job); err != nil {
		w.log.Warn("ingest: consumer failed; remaining consumers still receive the job",
			"consumer", fmt.Sprintf("%T", c),
			"workspace_id", job.WorkspaceID,
			"session_id", job.SessionID,
			"turn_id", job.TurnID,
			"error", err)
	}
}
