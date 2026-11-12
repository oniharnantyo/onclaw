package webhooks

import (
	"context"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/notifications"
)

// TurnView is the turn identity the agent-target dispatch hands back (the
// telegram channel's TurnView, plus Wait): the turn id every event of the
// turn carries, the done signal marking its terminal state, and the blocking
// wait the completion announcer uses. *agents.Turn satisfies it structurally;
// tests fake it.
type TurnView interface {
	// TurnID returns the turn's identifier.
	TurnID() string
	// Done returns a channel closed when the turn reaches a terminal state.
	Done() <-chan struct{}
	// Wait blocks until the turn finishes and returns its terminal outcome
	// (nil, the failure, or ErrTurnCancelled).
	Wait() error
}

// The real turn handle satisfies the seam without an adapter.
var _ TurnView = (*agents.Turn)(nil)

// Runs is the schedule-shaped seam between the receiver and the agent runtime
// for agent-target deliveries (add-webhook-triggers design D2: mirror of the
// scheduler's Runs). Create opens a titled session for the agent — the title
// is set here because the runtime only auto-titles untitled sessions — and
// Submit sends the rendered prompt as the session's opening turn with an
// explicit origin (hooks.OriginWebhook), returning the turn handle the
// cli-side completion announcer waits on. The composition root adapts the
// agents manager onto it; tests fake it.
type Runs interface {
	// Create creates a session for agentID titled title and returns the
	// session id; store.ErrNotFound when the agent does not exist.
	Create(ctx context.Context, agentID, title string) (sessionID string, err error)
	// Submit submits prompt as the session's next turn with the given
	// origin. It returns once the turn is accepted, not when it finishes.
	Submit(ctx context.Context, sessionID, prompt, origin string) (TurnView, error)
}

// GroupSender is the group-ingress seam (add-webhook-triggers design D2):
// one user-authored send of the rendered text into the group's shared
// transcript through the groups service's send path. It returns the
// transcript message id the delivery log records — the groups service's Send
// answers only with an error, so the composition-root adapter resolves the
// produced message id (from the send path's group event or a store read).
// Tests fake it.
type GroupSender interface {
	// Send appends text to the group's transcript as the user and routes it
	// (mentions, lead, tasks); the returned message id locates the
	// transcript row. An error — including a vanished group — fails the
	// dispatch.
	Send(ctx context.Context, groupID, text string) (messageID string, err error)
}

// GatewayState is the narrow read the pipeline's pause gate needs from the
// gateway: whether triggers are being accepted (Running), held (Paused — the
// listener stays up and answers 503), or the gateway is down (Stopped). The
// gateway manager satisfies it structurally.
type GatewayState interface {
	State() gateway.State
}

var _ GatewayState = gateway.Manager(nil)

// Notifier is the notification sink seam (add-webhook-triggers spec
// "Agent-target completions notify"): the cli-side completion announcer
// enqueues Webhooks-category candidates through it after an agent-target
// turn reaches a terminal state. *notifications.Engine satisfies it
// structurally; the announcer owns the turn-wait and excerpt assembly — the
// receiver never blocks on turn completion.
type Notifier interface {
	// Enqueue gates one candidate and delivers it when it passes. It never
	// returns an error: notification paths are best-effort.
	Enqueue(c notifications.Candidate)
}

var _ Notifier = (*notifications.Engine)(nil)
