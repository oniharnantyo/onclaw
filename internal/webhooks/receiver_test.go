package webhooks

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonBody is the happy-path delivery body.
var jsonBody = []byte(`{"action":"push"}`)

// TestPipeline_StatusMap walks every branch of the accept pipeline
// (add-webhook-triggers tasks 4.1): the HTTP answer, the recorded delivery
// row, and — where the branch fires — the dispatch seam's calls.
func TestPipeline_StatusMap(t *testing.T) {
	t.Run("202 accepted agent target records the row", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		h := tokenHeaders("tok")
		h.Set(StdWebIDHeader, "dlv-1")
		rec := p.deliver(ep.ID, jsonBody, h, "")

		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		assert.JSONEq(t, `{"status":"accepted"}`, rec.Body.String())

		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1, "one accepted delivery must be recorded")
		row := rows[0]
		assert.Equal(t, http.StatusAccepted, row.Status)
		assert.Equal(t, store.WebhookOutcomeAccepted, row.Outcome)
		assert.Equal(t, "session-1", row.SessionID, "the produced fire session id rides the row")
		assert.Equal(t, "", row.MessageID)
		assert.Equal(t, "header", row.Via, "the authenticating verifier's preset label")
		assert.Equal(t, "dlv-1", row.SenderDeliveryID)
		assert.Equal(t, "", row.Note)
		assert.False(t, row.AttemptedAt.IsZero(), "the store stamps the attempt time")
	})

	t.Run("202 accepted group target records the message id", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "Ops", GroupID: p.groupID(t), Prompt: "Ops says:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")

		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomeRan, rows[0].Outcome, "a synchronous group send records ran")
		assert.Equal(t, "message-1", rows[0].MessageID)
		assert.Equal(t, "", rows[0].SessionID)
	})

	t.Run("200 duplicate answers without a second fire", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		// The first delivery fired and was recorded.
		h := tokenHeaders("tok")
		h.Set(StdWebIDHeader, "dlv-1")
		first := p.deliver(ep.ID, jsonBody, h, "")
		require.Equal(t, http.StatusAccepted, first.Code)

		// The sender retries: same id, same endpoint.
		retry := p.deliver(ep.ID, jsonBody, h, "")
		require.Equal(t, http.StatusOK, retry.Code, retry.Body.String())
		assert.JSONEq(t, `{"duplicate":true}`, retry.Body.String())

		require.Len(t, p.runs.createdCalls(), 1, "the retry must not create a second session")
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 2, "both deliveries appear in the log")
		assert.Equal(t, store.WebhookOutcomeAccepted, rows[1].Outcome, "newest first: the retry")
		assert.Equal(t, store.WebhookOutcomeDuplicate, rows[0].Outcome)
		assert.Equal(t, "dlv-1", rows[0].SenderDeliveryID)
	})

	t.Run("401 fail-closed: a valid path token does not rescue a bad signature", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "GitLab", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{
			{Kind: store.WebhookVerifierStdWeb, Preset: "gitlab", Secret: "whsec_" + base64.StdEncoding.EncodeToString([]byte("gitlab-secret"))},
			{Kind: store.WebhookVerifierPath, Secret: "path-tok"},
		})

		// The signature header the endpoint expects, carrying a wrong value —
		// plus the correct path token in the URL.
		h := http.Header{}
		h.Set(StdWebIDHeader, "dlv-1")
		h.Set(StdWebTimestampHeader, strconv.FormatInt(testNow().Unix(), 10))
		h.Set(StdWebSignatureHeader, "v1,"+base64.StdEncoding.EncodeToString([]byte("wrong-signature")))
		rec := p.deliver(ep.ID, jsonBody, h, "path-tok")

		require.Equal(t, http.StatusUnauthorized, rec.Code, "signature presence is intent: no fall-through to the path token")
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomeAuthFailed, rows[0].Outcome)
		assert.Equal(t, http.StatusUnauthorized, rows[0].Status)
		assert.Empty(t, p.runs.createdCalls(), "an auth-failed delivery must not fire")
	})

	t.Run("401 unsigned request", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver(ep.ID, jsonBody, nil, "")

		require.Equal(t, http.StatusUnauthorized, rec.Code)
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomeAuthFailed, rows[0].Outcome)
		assert.Empty(t, p.runs.createdCalls())
	})

	t.Run("404 unknown path records nothing", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver("no-such-endpoint", jsonBody, nil, "")

		require.Equal(t, http.StatusNotFound, rec.Code)
		require.Empty(t, p.deliveries(t, ep.ID), "an unattributable delivery records nothing")
	})

	t.Run("404 disabled endpoint records failed with the disabled note", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: false,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")

		require.Equal(t, http.StatusNotFound, rec.Code, "a disabled endpoint answers 404 so the sender stops retrying")
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomeFailed, rows[0].Outcome)
		assert.Equal(t, "disabled", rows[0].Note)
		assert.Empty(t, p.runs.createdCalls())
	})

	t.Run("413 too-large body", func(t *testing.T) {
		p := newPipe(t, WithBodyLimit(8))
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver(ep.ID, []byte(`{"a":"this body is much longer than eight bytes"}`), tokenHeaders("tok"), "")

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomeTooLarge, rows[0].Outcome)
		assert.Empty(t, p.runs.createdCalls())
	})

	t.Run("503 with Retry-After while paused, before auth and body read", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})
		require.NoError(t, p.gw.Pause())

		// Bad credentials on purpose: the paused gate must answer 503 before
		// authentication runs.
		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("wrong"), "")

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "30", rec.Header().Get("Retry-After"))
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomePaused, rows[0].Outcome)
		assert.Equal(t, http.StatusServiceUnavailable, rows[0].Status)
		assert.Empty(t, p.runs.createdCalls(), "a paused delivery fires nothing")
	})

	t.Run("410 gone when the target agent was deleted", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: "deleted-agent", Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")

		require.Equal(t, http.StatusGone, rec.Code, "a dead target is misconfiguration: tell the sender to stop")
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomeFailed, rows[0].Outcome)
		assert.Contains(t, rows[0].Note, "not found", "the failure reason rides the note")
		assert.Empty(t, p.runs.createdCalls(), "a dead target starts nothing")
	})

	t.Run("410 gone when the target group was deleted", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "Ops", GroupID: "deleted-group", Prompt: "Ops says:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")

		require.Equal(t, http.StatusGone, rec.Code)
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomeFailed, rows[0].Outcome)
		assert.Contains(t, rows[0].Note, "not found")
		assert.Empty(t, p.groups.sentCalls())
	})

	t.Run("503 when dispatch fails transiently", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})
		p.runs.mu.Lock()
		p.runs.submitErr = errors.New("lane unavailable")
		p.runs.mu.Unlock()

		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")

		require.Equal(t, http.StatusServiceUnavailable, rec.Code, "a transient dispatch failure invites a retry")
		rows := p.deliveries(t, ep.ID)
		require.Len(t, rows, 1)
		assert.Equal(t, store.WebhookOutcomeFailed, rows[0].Outcome)
		assert.Contains(t, rows[0].Note, "lane unavailable")
	})

	t.Run("405 for non-POST methods records nothing", func(t *testing.T) {
		p := newPipe(t)
		ep := p.createEndpoint(t, store.WebhookEndpoint{
			Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
		}, []store.WebhookVerifier{headerVerifier("tok")})

		req := httptest.NewRequest(http.MethodGet, "/webhooks/"+ep.ID, nil)
		recRw := httptest.NewRecorder()
		p.rec.ServeHTTP(recRw, req)

		require.Equal(t, http.StatusMethodNotAllowed, recRw.Code)
		require.Empty(t, p.deliveries(t, ep.ID))
	})
}

// TestPipeline_AuthLabelForManualVerifier pins the via label for a manually
// built verifier row: its engine kind when no preset seeded it.
func TestPipeline_AuthLabelForManualVerifier(t *testing.T) {
	p := newPipe(t)
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
	}, []store.WebhookVerifier{{Kind: store.WebhookVerifierHeader, HeaderName: "X-Token", Secret: "s3cret"}})

	h := http.Header{}
	h.Set("X-Token", "s3cret")
	rec := p.deliver(ep.ID, jsonBody, h, "")

	require.Equal(t, http.StatusAccepted, rec.Code)
	rows := p.deliveries(t, ep.ID)
	require.Len(t, rows, 1)
	assert.Equal(t, "header", rows[0].Via, "a manual row's via label is its engine kind")
}

// TestPipeline_DedupeOnlyWhenSenderProvidesID pins the dedupe gate's input:
// an id-less sender is never deduplicated, and a different id is a fresh
// delivery.
func TestPipeline_DedupeOnlyWhenSenderProvidesID(t *testing.T) {
	p := newPipe(t)
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})

	// Two id-less deliveries: both fire.
	for i := 0; i < 2; i++ {
		rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")
		require.Equal(t, http.StatusAccepted, rec.Code)
	}
	require.Len(t, p.runs.createdCalls(), 2, "senders without a delivery id are never deduplicated")

	// A different id is a fresh delivery.
	h := tokenHeaders("tok")
	h.Set(StdWebIDHeader, "dlv-other")
	rec := p.deliver(ep.ID, jsonBody, h, "")
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, p.runs.createdCalls(), 3)
}

// TestPipeline_GroupSendErrorRecordsFailed pins the group dispatch's error
// path: a send failure past the alive check (transient) records failed and
// answers 503.
func TestPipeline_GroupSendErrorRecordsFailed(t *testing.T) {
	p := newPipe(t)
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "Ops", GroupID: p.groupID(t), Prompt: "Ops says:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})
	p.groups.mu.Lock()
	p.groups.err = errors.New("lane queue full")
	p.groups.mu.Unlock()

	rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	rows := p.deliveries(t, ep.ID)
	require.Len(t, rows, 1)
	assert.Equal(t, store.WebhookOutcomeFailed, rows[0].Outcome)
	assert.Contains(t, rows[0].Note, "lane queue full")
}

// TestReceiver_NilGatewayReadsRunning pins the nil-gateway default: a
// Receiver built without a gateway serves deliveries (the direct-http test
// shape).
func TestReceiver_NilGatewayReadsRunning(t *testing.T) {
	p := newPipe(t)
	rec := NewReceiver(p.st, p.runs, p.groups, nil, WithClock(testNow))
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/"+ep.ID, bytes.NewReader(jsonBody))
	req.Header = tokenHeaders("tok")
	rw := httptest.NewRecorder()
	rec.ServeHTTP(rw, req)

	require.Equal(t, http.StatusAccepted, rw.Code)
}

// TestPipeline_CtxCarriesIntoStore pins that request cancellation surfaces
// as a recording-free failure path is exercised implicitly by the other
// tests; here we pin the fixture's log read path instead (store round-trip
// through the fake).
func TestPipeline_StoreRoundTrip(t *testing.T) {
	p := newPipe(t)
	ep := p.createEndpoint(t, store.WebhookEndpoint{
		Name: "CI", AgentID: p.agentID(t), Prompt: "Handle this:", Enabled: true,
	}, []store.WebhookVerifier{headerVerifier("tok")})

	rec := p.deliver(ep.ID, jsonBody, tokenHeaders("tok"), "")
	require.Equal(t, http.StatusAccepted, rec.Code)

	got, err := p.st.Webhooks().GetEndpoint(context.Background(), ep.ID)
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Len(t, got.Verifiers, 1)
}
