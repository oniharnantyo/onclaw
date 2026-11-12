package webhooks

import (
	"context"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/store"
)

// fireTimeLayout is how a delivery time appears in a fire-session title —
// the scheduler's layout, so both trigger surfaces title identically.
const fireTimeLayout = "2006-01-02 15:04"

// dispatchAgent fires a fresh titled session for the endpoint's agent target
// (add-webhook-triggers design D2, tasks 5.1): CreateSession → SetTitle —
// both inside the Runs seam's Create, mirroring scheduleRuns — then Submit
// with the rendered prompt and the webhook origin, so hooks and the turn
// environment treat the run as autonomous. It returns the session id and the
// turn handle; it never waits for the turn to finish.
func (r *Receiver) dispatchAgent(ctx context.Context, ep *store.WebhookEndpoint, body []byte) (dispatchResult, error) {
	// Dead-target re-check at dispatch (task 5.4): the endpoint may outlive
	// its agent — the store keeps no foreign key — so resolve the agent
	// before starting anything.
	if _, err := r.store.Agents().ByID(ctx, ep.AgentID); err != nil {
		return dispatchResult{}, fmt.Errorf("webhook target agent: %w", err)
	}

	title := ep.Name + " — " + r.now().Format(fireTimeLayout)
	sessionID, err := r.runs.Create(ctx, ep.AgentID, title)
	if err != nil {
		return dispatchResult{}, fmt.Errorf("create webhook fire session for agent %s: %w", ep.AgentID, err)
	}

	prompt := RenderPrompt(ep.Prompt, body)
	turn, err := r.runs.Submit(ctx, sessionID, prompt, hooks.OriginWebhook)
	if err != nil {
		return dispatchResult{}, fmt.Errorf("submit webhook fire for session %s: %w", sessionID, err)
	}
	return dispatchResult{outcome: store.WebhookOutcomeAccepted, sessionID: sessionID, turn: turn}, nil
}

// dispatchGroup appends the rendered text to the target group's transcript as
// one user-authored message through the GroupSender seam (add-webhook-triggers
// design D2, tasks 5.2): mentions route to named members, untargeted text
// routes to the lead, and no transcript artifact marks the message
// machine-authored. The send path is synchronous up to the transcript append,
// so the produced message id lands in the delivery log as outcome ran.
func (r *Receiver) dispatchGroup(ctx context.Context, ep *store.WebhookEndpoint, body []byte) (dispatchResult, error) {
	// Dead-target re-check at dispatch (task 5.4), mirroring the agent path.
	if _, err := r.store.Groups().GroupByID(ctx, ep.GroupID); err != nil {
		return dispatchResult{}, fmt.Errorf("webhook target group: %w", err)
	}

	messageID, err := r.groups.Send(ctx, ep.GroupID, RenderPrompt(ep.Prompt, body))
	if err != nil {
		return dispatchResult{}, fmt.Errorf("send webhook delivery to group %s: %w", ep.GroupID, err)
	}
	return dispatchResult{outcome: store.WebhookOutcomeRan, messageID: messageID}, nil
}
