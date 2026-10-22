package gateway

import "context"

// StateListener is notified on every successful state transition. It receives
// the previous and the new state. It is invoked after the transition is
// committed; it must not block.
type StateListener func(from, to State)

// Manager is the background lifecycle seam of the application: it tracks the
// gateway state (stopped, running, paused) and notifies listeners on every
// transition so UI surfaces such as the system tray can reflect it.
//
// Transition semantics:
//   - Start: Stopped -> Running. Error if already running or paused.
//   - Stop: Running/Paused -> Stopped. Error if already stopped.
//   - Pause: Running -> Paused. Error if not running.
//   - Resume: Paused -> Running. Error if not paused.
type Manager interface {
	// Start transitions the gateway from Stopped to Running.
	Start(_ context.Context) error
	// Stop transitions the gateway from Running or Paused to Stopped.
	Stop() error
	// Pause transitions the gateway from Running to Paused.
	Pause() error
	// Resume transitions the gateway from Paused to Running.
	Resume() error
	// State returns the current gateway state. Safe for concurrent use.
	State() State
	// Subscribe registers fn to be called on every state transition and
	// returns a function that unregisters it. The returned cancel function
	// is safe to call more than once and safe for concurrent use.
	Subscribe(fn StateListener) (cancel func())
}

// NewManager returns a gateway Manager in the Stopped state with no
// trigger adapters attached.
func NewManager() Manager {
	return newManager()
}
