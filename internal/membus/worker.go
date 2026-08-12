package membus

import "context"

// Worker processes events dispatched by the Bus.
// Each worker declares which event names it subscribes to.
// Handle is called sequentially within a single worker's goroutine.
type Worker interface {
	// Handle processes a single event. Errors are logged but never propagate.
	Handle(ctx context.Context, event Event) error
	// Subscribes returns the event names this worker handles.
	Subscribes() []string
}
