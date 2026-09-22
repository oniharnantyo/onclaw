package webhooks

import (
	"context"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// DefaultPerConnectionLimit is the default in-flight cap per connection
// (design.md D6): a bulk merge's event flurry cannot flood a channel beyond
// this many concurrent turns, while distinct connections never contend.
const DefaultPerConnectionLimit = 4

// ProcessFunc processes one queued delivery. It runs with a context that
// outlives the acking request; the queue never drops an accepted job —
// Close waits for in-flight work.
type ProcessFunc func(ctx context.Context, job *Delivery)

// Queue is the bounded delivery queue (design.md D6, task 2.3): a fixed-
// depth channel bounds the pending backlog — Enqueue reports false when
// full, and the ingress degrades that refusal to inline processing (the
// delivery id is already recorded; refusing the provider would lose the
// event) — while a per-connection semaphore bounds how many of one
// connection's deliveries process concurrently. A single dispatcher
// goroutine hands each job to its own processing goroutine once the
// connection slot is acquired, so one slow connection never starves another.
type Queue struct {
	depth     int
	perConn   int
	process   ProcessFunc
	jobs      chan *Delivery
	closeOnce sync.Once
	wg        sync.WaitGroup

	mu      sync.Mutex
	started bool
	closed  bool
	slots   map[string]chan struct{}
}

// QueueOption customizes a Queue.
type QueueOption func(*Queue)

// WithPerConnectionLimit overrides the per-connection in-flight cap.
func WithPerConnectionLimit(n int) QueueOption {
	return func(q *Queue) {
		if n > 0 {
			q.perConn = n
		}
	}
}

// NewQueue builds a bounded delivery queue with the given maximum backlog
// depth feeding the process function. The queue starts its dispatcher on
// the first Enqueue; Close stops it and waits for in-flight processing.
func NewQueue(depth int, process ProcessFunc, opts ...QueueOption) *Queue {
	if depth <= 0 {
		depth = 1
	}
	q := &Queue{
		depth:   depth,
		perConn: DefaultPerConnectionLimit,
		process: process,
		jobs:    make(chan *Delivery, depth),
		slots:   make(map[string]chan struct{}),
	}
	for _, opt := range opts {
		opt(q)
	}
	return q
}

// Enqueue submits one delivery for asynchronous processing. It reports
// false when the backlog is at depth or after Close — the ingress degrades
// to inline processing on that refusal (the delivery id is already recorded,
// so a 503 backpressure would lose the event), the queue itself never
// drops an accepted job.
func (q *Queue) Enqueue(job *Delivery) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false
	}
	if !q.started {
		q.started = true
		q.wg.Add(1)
		go q.dispatch()
	}
	q.mu.Unlock()

	select {
	case q.jobs <- job:
		return true
	default:
		return false
	}
}

// Close stops the dispatcher and waits for every in-flight job to finish.
// Idempotent; Enqueue after Close returns false.
func (q *Queue) Close() {
	q.closeOnce.Do(func() {
		q.mu.Lock()
		q.closed = true
		if q.started {
			close(q.jobs)
		}
		q.mu.Unlock()
		q.wg.Wait()
	})
}

// dispatch drains the job channel, spawning one processing goroutine per
// job once the job's connection slot is acquired.
func (q *Queue) dispatch() {
	defer q.wg.Done()
	for job := range q.jobs {
		slot := q.connectionSlot(job.connectionID)
		q.wg.Add(1)
		go func(job *Delivery, slot chan struct{}) {
			defer q.wg.Done()
			slot <- struct{}{}
			defer func() { <-slot }()
			q.process(context.WithoutCancel(context.Background()), job)
		}(job, slot)
	}
}

// connectionSlot returns the per-connection semaphore, creating it on first
// use. Slots are never reclaimed — one channel per connection id ever seen,
// bounded by the workspace's connection count.
func (q *Queue) connectionSlot(connectionID string) chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	slot, exists := q.slots[connectionID]
	if !exists {
		slot = make(chan struct{}, q.perConn)
		q.slots[connectionID] = slot
	}
	return slot
}

// Delivery is one queued, verified delivery with everything rendering and
// routing need: the connection's webhook state snapshot (binding, event
// selection), the registered recipe, and the raw payload body.
type Delivery struct {
	workspaceID  string
	connectionID string
	service      string
	eventID      string
	state        *domain.ConnectionWebhook
	recipe       *domain.Recipe
	body         []byte
}
