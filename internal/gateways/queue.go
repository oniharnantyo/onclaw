package gateways

import (
	"context"
	"errors"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// DefaultQueueDepth is the per-session busy-queue bound (design D4): beyond
// it, new messages during an active run are refused instead of queued, so a
// flooded chat can never grow the queue unbounded.
const DefaultQueueDepth = 8

// resubmitFunc submits a queued turn again once the session frees up. It
// returns domain.ErrConflict when the session is busy again (the entry goes
// back to the front of its queue); nil means the turn was accepted and its
// stream handed to the delivery side.
type resubmitFunc func(ctx context.Context, plan TurnPlan) error

// notifyFunc delivers a chat message for queue lifecycle events: the queued
// acknowledgment, or the queue-full refusal.
type notifyFunc func(ctx context.Context, plan TurnPlan, text string)

// BusyQueue implements the gateway-side busy queue (design D4): when the
// RunManager's one-run-per-session guard rejects a submission with
// domain.ErrConflict, the message is enqueued (bounded per session), the
// sender is acknowledged, and the queue head is resubmitted when the
// previous run's terminal outcome is observed — the service's
// NotifyRunFinished hook, fed by the stream controller's drain.
type BusyQueue struct {
	depth    int
	resubmit resubmitFunc
	notify   notifyFunc

	mu     sync.Mutex
	queues map[string][]TurnPlan // sessionID → FIFO
}

// QueueOption customizes a BusyQueue.
type QueueOption func(*BusyQueue)

// WithQueueDepth overrides the per-session queue bound.
func WithQueueDepth(depth int) QueueOption {
	return func(q *BusyQueue) {
		if depth > 0 {
			q.depth = depth
		}
	}
}

// NewBusyQueue builds the busy queue. The resubmit and notify callbacks are
// required — the queue never talks to the runner or the platform itself.
func NewBusyQueue(resubmit resubmitFunc, notify notifyFunc, opts ...QueueOption) *BusyQueue {
	q := &BusyQueue{
		depth:    DefaultQueueDepth,
		resubmit: resubmit,
		notify:   notify,
		queues:   make(map[string][]TurnPlan),
	}
	for _, opt := range opts {
		opt(q)
	}
	return q
}

// Enqueue parks a turn for a busy session and acknowledges the sender. It
// returns false when the session's queue is at depth — the caller then
// refuses the message with a chat notice instead.
func (q *BusyQueue) Enqueue(ctx context.Context, plan TurnPlan, ack string) bool {
	q.mu.Lock()
	if len(q.queues[plan.SessionID]) >= q.depth {
		q.mu.Unlock()
		return false
	}
	q.queues[plan.SessionID] = append(q.queues[plan.SessionID], plan)
	q.mu.Unlock()

	if ack != "" {
		q.notify(ctx, plan, ack)
	}
	return true
}

// NotifyRunFinished releases the next queued turn for the session whose run
// just reached a terminal outcome (stream-drain observation, design D4).
// No-op when nothing is queued. The resubmitted turn goes through the
// service's normal submission path — if the session was grabbed again in
// between, the entry returns to the front of its queue and waits for the
// next terminal observation.
func (q *BusyQueue) NotifyRunFinished(ctx context.Context, sessionID string) {
	q.mu.Lock()
	queue := q.queues[sessionID]
	if len(queue) == 0 {
		q.mu.Unlock()
		return
	}
	head := queue[0]
	rest := queue[1:]
	if len(rest) == 0 {
		delete(q.queues, sessionID)
	} else {
		// Copy-on-write: keep the backing array out of the caller's reach.
		q.queues[sessionID] = append([]TurnPlan(nil), rest...)
	}
	q.mu.Unlock()

	if err := q.resubmit(ctx, head); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			q.mu.Lock()
			q.queues[sessionID] = append([]TurnPlan{head}, q.queues[sessionID]...)
			q.mu.Unlock()
			return
		}
		// Submission failed for a non-conflict reason (runner-side error):
		// drop the turn and tell the chat rather than spinning.
		q.notify(ctx, head, "The queued message could not be submitted — the run failed. Please try again.")
	}
}

// Depth reports the queued depth for one session (tests and diagnostics).
func (q *BusyQueue) Depth(sessionID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queues[sessionID])
}
