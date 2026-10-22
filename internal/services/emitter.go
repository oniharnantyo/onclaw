package services

// Emitter forwards service events to the host's event system (the Wails
// event bus on desktop and server hosts). Services emit through this
// interface so they stay unit-testable without Wails (design D1: the
// services layer is a thin wrapper; the core stays Wails-free).
type Emitter interface {
	// Emit forwards one event: name identifies it, data carries its payload.
	// Implementations must not block.
	Emit(name string, data ...any)
}

// Noop is the Emitter null object: it discards every event. Hosts (or tests)
// without event forwarding inject it so a nil emitter never has to be
// guarded against.
type Noop struct{}

// Emit discards the event.
func (Noop) Emit(_ string, _ ...any) {}
