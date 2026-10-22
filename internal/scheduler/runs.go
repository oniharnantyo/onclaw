package scheduler

import (
	"context"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
)

// Runs is the seam between the scheduler and the agent runtime
// (add-scheduler design D7). Create makes a titled session for the agent and
// returns its id; Submit sends the prompt as the session's opening user
// message with origin scheduler and returns once the turn is accepted — the
// turn then runs to completion unattended, transcript persisted as usual.
type Runs interface {
	// Create creates a session for agentID titled title (the runner only
	// auto-titles untitled sessions, so the title must be set here) and
	// returns the session id.
	Create(ctx context.Context, agentID, title string) (sessionID string, err error)
	// Submit submits prompt as the session's next turn with origin
	// scheduler. It returns once the turn is accepted, not when it finishes.
	Submit(ctx context.Context, sessionID, prompt string) error
}

// GatewayState is the narrow read the tick needs from the gateway: whether
// triggers are being accepted (Running), held (Paused), or the gateway is
// down (Stopped).
type GatewayState interface {
	State() gateway.State
}

// Clock supplies the service's notion of "now" — local time in production.
// Tests inject a fixed clock so next-fire computation is deterministic.
type Clock func() time.Time
