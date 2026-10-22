package agents

import "sync"

// Turn is the handle of one in-flight or finished agent turn. Send returns
// it immediately (design D7): the turn proceeds in the background, progress
// flows through session subscriptions, and durability is independent of any
// subscriber. Wait blocks for the terminal outcome.
type Turn struct {
	id string

	done chan struct{}
	once sync.Once
	err  error
}

// TurnID returns the turn's identifier, carried on every event it emits.
func (t *Turn) TurnID() string { return t.id }

// Done returns a channel closed when the turn reaches a terminal state.
func (t *Turn) Done() <-chan struct{} { return t.done }

// Wait blocks until the turn finishes. It returns nil when the turn
// completed, the failure for a turn that errored, and ErrTurnCancelled for a
// turn cancelled while in flight or parked on a pending approval. A turn
// parked on a pending approval finishes only when the approval is resolved
// or the turn is cancelled.
func (t *Turn) Wait() error {
	<-t.done
	return t.err
}

// finish records the terminal outcome exactly once and wakes waiters.
func (t *Turn) finish(err error) {
	t.once.Do(func() {
		t.err = err
		close(t.done)
	})
}
