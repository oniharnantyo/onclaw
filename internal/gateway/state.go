package gateway

// State is the lifecycle state of the gateway.
type State int

const (
	// StateStopped means the gateway is not running. No triggers would be
	// accepted even if adapters were attached.
	StateStopped State = iota
	// StateRunning means the gateway is running and would accept triggers.
	StateRunning
	// StatePaused means the gateway is running but triggers would be held:
	// neither accepted nor processed until resumed.
	StatePaused
)

// String returns a human-readable name for the state, used by the tray UI.
func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateRunning:
		return "running"
	case StatePaused:
		return "paused"
	default:
		return "unknown"
	}
}
