package webhooks

import (
	"net/http"
	"testing"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDispatchAgent_FreshTitledSession pins the agent-target adapter
// (add-webhook-triggers tasks 5.1): every delivery creates a fresh session
// titled "<endpoint> — <delivery time>" and submits the template with the
// payload block appended under the webhook origin.
func TestDispatchAgent_FreshTitledSession(t *testing.T) {
	p := newPipe(t)
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "Nightly CI", AgentID: p.agentID(t), Prompt: "Summarize the build:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})

	rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")
	require.Equal(t, http.StatusAccepted, rec.Code)

	creates := p.runs.createdCalls()
	require.Len(t, creates, 1, "one delivery, one fresh session")
	assert.Equal(t, ep.AgentID, creates[0].AgentID)
	assert.Equal(t, "Nightly CI — 2026-03-04 09:30", creates[0].Title,
		"the session title mirrors the scheduler fire shape: <endpoint> — <time>")

	submits := p.runs.submitCalls()
	require.Len(t, submits, 1)
	assert.Equal(t, "session-1", submits[0].SessionID, "the submit rides the created session")
	assert.Equal(t, hooks.OriginWebhook, submits[0].Origin, "the turn is stamped with the webhook origin")
	assert.Equal(t, "Summarize the build:\n\n```json\n"+string(jsonBody)+"\n```", submits[0].Prompt,
		"the prompt is the template with the payload block appended")
}

// TestDispatchAgent_FormBodyPrompt pins the non-JSON sender's prompt shape:
// the raw body rides a plain fence.
func TestDispatchAgent_FormBodyPrompt(t *testing.T) {
	p := newPipe(t)
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Build:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})

	formBody := []byte("status=ok&build=1234")
	rec := p.deliver(ep.ID, formBody, tokenHeaders("tok"), "")
	require.Equal(t, http.StatusAccepted, rec.Code)

	submits := p.runs.submitCalls()
	require.Len(t, submits, 1)
	assert.Equal(t, "Build:\n\n```\n"+string(formBody)+"\n```", submits[0].Prompt)
}

// TestDispatchAgent_FreshSessionPerDelivery pins the 1:1 delivery-to-session
// rule: two deliveries create two sessions (no persistent per-endpoint
// session, design D2).
func TestDispatchAgent_FreshSessionPerDelivery(t *testing.T) {
	p := newPipe(t)
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Go:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})

	for i := 0; i < 2; i++ {
		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")
		require.Equal(t, http.StatusAccepted, rec.Code)
	}

	creates := p.runs.createdCalls()
	require.Len(t, creates, 2)
	assert.Equal(t, "session-1", p.runs.submitCalls()[0].SessionID)
	assert.Equal(t, "session-2", p.runs.submitCalls()[1].SessionID, "each delivery fires its own session")
}

// TestDispatchGroup_OneUserSend pins the group-target adapter
// (add-webhook-triggers tasks 5.2): exactly one send of the rendered text,
// recorded with the transcript message id.
func TestDispatchGroup_OneUserSend(t *testing.T) {
	p := newPipe(t)
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "Ops", GroupID: p.groupID(t), Prompt: "@Scout deploy went out:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})

	rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")
	require.Equal(t, http.StatusAccepted, rec.Code)

	sent := p.groups.sentCalls()
	require.Len(t, sent, 1, "one delivery, one user-authored send")
	assert.Equal(t, ep.GroupID, sent[0].GroupID)
	assert.Equal(t, "@Scout deploy went out:\n\n```json\n"+string(jsonBody)+"\n```", sent[0].Text,
		"the rendered text is the template with the payload block appended")

	rows := p.deliveries(t, ep.ID)
	require.Len(t, rows, 1)
	assert.Equal(t, "message-1", rows[0].MessageID, "the transcript message id rides the delivery row")
	assert.Equal(t, store.WebhookOutcomeRan, rows[0].Outcome)
}

// TestDispatchDeadTarget_NeverFires pins task 5.4's dead-target rule for
// both target kinds through the dispatch path: the alive check refuses
// before the seam is touched, the row records failed with the reason.
func TestDispatchDeadTarget_NeverFires(t *testing.T) {
	t.Run("agent target", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: "gone-agent", Prompt: "Go:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")
		require.Equal(t, http.StatusGone, rec.Code)
		require.Empty(t, p.runs.createdCalls(), "a dead agent target must not create a session")
	})

	t.Run("group target", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "Ops", GroupID: "gone-group", Prompt: "Ops:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")
		require.Equal(t, http.StatusGone, rec.Code)
		require.Empty(t, p.groups.sentCalls(), "a dead group target must not send")
	})
}

// TestDispatchPathToken pins the path-token route: the token segment
// authenticates and the delivery dispatches; a wrong token fails closed.
func TestDispatchPathToken(t *testing.T) {
	t.Run("correct token dispatches", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "Bare", AgentID: p.agentID(t), Prompt: "Go:", Enabled: true,
		}, []store.WebhookVerifier{{Kind: store.WebhookVerifierPath, Secret: "path-tok"}})

		rec := p.deliver(ep.ID, jsonBody, nil, "path-tok")
		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Len(t, p.runs.createdCalls(), 1)
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, "path", rows[0].Via)
	})

	t.Run("wrong token refuses", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "Bare", AgentID: p.agentID(t), Prompt: "Go:", Enabled: true,
		}, []store.WebhookVerifier{{Kind: store.WebhookVerifierPath, Secret: "path-tok"}})

		rec := p.deliver(ep.ID, jsonBody, nil, "wrong-token")
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Empty(t, p.runs.createdCalls())
	})
}
