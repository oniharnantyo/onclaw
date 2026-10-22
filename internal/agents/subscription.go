package agents

import (
	"sync"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// Subscription is the live-view tap of one session's turns. Events arrive as
// produced; Close ends the subscription. A subscription that misses events —
// attached late, disconnected mid-turn, or dropped under backpressure —
// recovers them from the store by replaying the session's events.
type Subscription interface {
	// Events yields turn events for the subscribed session until Close.
	Events() <-chan sessions.Event
	// Close unregisters and closes the subscription. Buffered events remain
	// readable until drained. Close is idempotent and safe for concurrent
	// use.
	Close()
}

// subscriberBuffer bounds one subscription's event buffer. Sized after the
// donor's live-view tap: enough to absorb a streaming turn's deltas for a
// reading consumer; overflow drops the newest event instead of stalling the
// turn.
const subscriberBuffer = 128

// streamSub is the concrete Subscription: a bounded channel with drop-new
// semantics. Send never blocks — an unwatched or slow subscription can never
// stall a turn, and durability lives in the store, not in the tap.
type streamSub struct {
	events chan sessions.Event

	mu     sync.Mutex
	closed bool
}

func newStreamSub() *streamSub {
	return &streamSub{events: make(chan sessions.Event, subscriberBuffer)}
}

// send enqueues an event without blocking: drop-new discards it when the
// buffer is full or the subscription is closed.
func (s *streamSub) send(ev sessions.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.events <- ev:
		return true
	default:
		return false
	}
}

func (s *streamSub) Events() <-chan sessions.Event { return s.events }

func (s *streamSub) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.events)
}
