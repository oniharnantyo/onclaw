package notifications

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webhookCandidate builds an agent-target webhook-fire completion candidate
// (add-webhook-triggers): Webhooks category, routed to the fire session.
func webhookCandidate(sessionID, turnID string, origin Origin) Candidate {
	return Candidate{
		ID:       WebhookNotificationID(sessionID, turnID),
		Category: CategoryWebhooks,
		Origin:   origin,
		NameKey:  "agent-1",
		Outcome:  "completed",
		Detail:   "deploy finished",
		Route:    Route{Kind: RouteSession, ID: sessionID},
		ThreadID: sessionID,
	}
}

// TestWebhookCategory_DeliversUnderMasterFlag pins the webhooks category's
// gating: on with the master flag (no per-category toggle exists yet), off
// with it, and titled through the agent resolver.
func TestWebhookCategory_DeliversUnderMasterFlag(t *testing.T) {
	e, n, _ := newTestEngine(t, testResolvers)
	e.Enqueue(webhookCandidate("sess-1", "turn-1", OriginWebhook))

	sent := n.payloads()
	require.Len(t, sent, 1, "the webhooks category delivers under the master flag")
	assert.Equal(t, CategoryWebhooks, sent[0].Category)
	assert.Equal(t, "Scout", sent[0].Title, "the fire resolves its title through the agent resolver")
	assert.Equal(t, "completed\ndeploy finished", sent[0].Body)
	assert.Equal(t, "sess-1", sent[0].ThreadID)

	// Master off: nothing delivers.
	e2, n2, _ := newTestEngine(t, testResolvers)
	e2.SetSettings(Values{Enabled: false, Turns: true, Attention: true, Schedules: true, Groups: true})
	e2.Enqueue(webhookCandidate("sess-1", "turn-2", OriginWebhook))
	assert.Empty(t, n2.payloads(), "the master flag gates the webhooks category")
}

// TestWebhookOrigin_TurnPathDrop pins the matrix's webhook row: a
// webhook-origin turn candidate is dropped on the turn path because the
// webhook announcer is the sole notifier for webhook fires
// (add-webhook-triggers) — preventing double notifications.
func TestWebhookOrigin_TurnPathDrop(t *testing.T) {
	e, n, _ := newTestEngine(t, testResolvers)

	// The dropped webhook-origin turn candidate.
	e.Enqueue(Candidate{
		ID:       TurnNotificationID("sess-1", "turn-1"),
		Category: CategoryTurns,
		Origin:   OriginWebhook,
		NameKey:  "agent-1",
		Outcome:  "completed",
		Route:    Route{Kind: RouteSession, ID: "sess-1"},
		ThreadID: "sess-1",
	})
	// The Webhooks-category fire completion delivers.
	e.Enqueue(webhookCandidate("sess-1", "turn-1", OriginWebhook))

	sent := n.payloads()
	require.Len(t, sent, 1, "only the Webhooks-category candidate delivers")
	assert.Equal(t, CategoryWebhooks, sent[0].Category)
	assert.Equal(t, WebhookNotificationID("sess-1", "turn-1"), sent[0].ID)
}

// TestWebhookOrigin_MatrixRegressions pins the rest of the dedup matrix
// against the webhook addition: scheduler and telegram origins still drop,
// the empty origin (user) still delivers.
func TestWebhookOrigin_MatrixRegressions(t *testing.T) {
	tests := []struct {
		name      string
		origin    Origin
		delivered bool
	}{
		{"empty origin reads as user and delivers", "", true},
		{"user origin delivers", OriginUser, true},
		{"scheduler origin drops", OriginScheduler, false},
		{"telegram origin drops", OriginTelegram, false},
		{"webhook origin drops", OriginWebhook, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, n, _ := newTestEngine(t, testResolvers)
			e.Enqueue(turnCandidate("sess-1", "turn-1", tt.origin, "completed", ""))
			if tt.delivered {
				require.Len(t, n.payloads(), 1)
			} else {
				require.Empty(t, n.payloads())
			}
		})
	}
}

// TestWebhookCategory_ResolveRoute pins click-to-open: a delivered webhook
// candidate resolves to its fire session.
func TestWebhookCategory_ResolveRoute(t *testing.T) {
	e, _, _ := newTestEngine(t, testResolvers)
	c := webhookCandidate("sess-7", "turn-2", OriginWebhook)
	e.Enqueue(c)

	route, ok := e.ResolveRoute(c.ID)
	require.True(t, ok)
	assert.Equal(t, Route{Kind: RouteSession, ID: "sess-7"}, route)
}
