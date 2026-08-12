package membus

// Event is the interface all memory bus events must implement.
type Event interface {
	EventName() string
}
