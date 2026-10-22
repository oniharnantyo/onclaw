package run

import (
	"sync"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// Turn is the handle of one in-flight or finished agent turn.
type Turn struct {
	id   string
	done chan struct{}
	once sync.Once
	err  error
}

// NewTurn creates a new in-flight Turn handle.
func NewTurn(id string) *Turn {
	return &Turn{
		id:   id,
		done: make(chan struct{}),
	}
}

// TurnID returns the turn's identifier.
func (t *Turn) TurnID() string { return t.id }

// Done returns a channel closed when the turn reaches a terminal state.
func (t *Turn) Done() <-chan struct{} { return t.done }

// Wait blocks until the turn finishes.
func (t *Turn) Wait() error {
	<-t.done
	return t.err
}

// Finish records the terminal outcome exactly once and wakes waiters.
func (t *Turn) Finish(err error) {
	t.once.Do(func() {
		t.err = err
		close(t.done)
	})
}

// Subscription is the live-view tap of one session's turns.
type Subscription interface {
	Events() <-chan sessions.Event
	Close()
}

const subscriberBuffer = 128

// StreamSub is the concrete Subscription.
type StreamSub struct {
	events chan sessions.Event
	mu     sync.Mutex
	closed bool
}

// NewStreamSub constructs a new live event subscription.
func NewStreamSub() *StreamSub {
	return &StreamSub{events: make(chan sessions.Event, subscriberBuffer)}
}

// Send enqueues an event without blocking.
func (s *StreamSub) Send(ev sessions.Event) bool {
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

// Events returns the receive channel.
func (s *StreamSub) Events() <-chan sessions.Event { return s.events }

// Close terminates the subscription.
func (s *StreamSub) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
}
