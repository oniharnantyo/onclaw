package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
)

// handle runs the accept pipeline for one resolved route
// (add-webhook-triggers tasks 4.1, design D5's status map). The gate order —
// resolve → enabled → paused → size cap → credentials → dedupe → dead
// target → dispatch — exists so cheap refusals precede body reads and
// authentication, and so a present-but-bad signature can never fall through
// to dispatch.
func (r *Receiver) handle(w http.ResponseWriter, req *http.Request, endpointID, pathToken string) {
	ctx := req.Context()

	ep, err := r.store.Webhooks().GetEndpoint(ctx, endpointID)
	if err != nil {
		// Unknown path: nothing to attribute the delivery to, nothing recorded.
		http.NotFound(w, req)
		return
	}

	if !ep.Enabled {
		// A disabled endpoint does not participate: 404 tells the sender to
		// stop retrying instead of queueing against a dead route.
		r.record(ctx, store.WebhookDelivery{
			EndpointID:       ep.ID,
			Status:           http.StatusNotFound,
			Outcome:          store.WebhookOutcomeFailed,
			SenderDeliveryID: r.senderDeliveryID(req),
			Note:             "disabled",
		})
		http.NotFound(w, req)
		return
	}

	if r.gatewayState() != gateway.StateRunning {
		// Paused (or down): the sender's retry policy is the queue
		// (add-webhook-triggers design D5). Answer before reading the body —
		// the delivery is refused unread, recorded as paused.
		r.record(ctx, store.WebhookDelivery{
			EndpointID:       ep.ID,
			Status:           http.StatusServiceUnavailable,
			Outcome:          store.WebhookOutcomePaused,
			SenderDeliveryID: r.senderDeliveryID(req),
		})
		w.Header().Set("Retry-After", retryAfterSeconds)
		writeJSON(w, http.StatusServiceUnavailable, `{"error":"gateway paused"}`)
		return
	}

	body, ok := readBody(req, r.bodyCap)
	if !ok {
		r.record(ctx, store.WebhookDelivery{
			EndpointID:       ep.ID,
			Status:           http.StatusRequestEntityTooLarge,
			Outcome:          store.WebhookOutcomeTooLarge,
			SenderDeliveryID: r.senderDeliveryID(req),
		})
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	label, verdict := Authorize(verifierConfigs(ep.Verifiers), VerifyInput{
		PathToken: pathToken,
		Header:    req.Header.Get,
		Body:      body,
		Now:       r.now(),
	})
	if verdict != VerdictPass {
		// Both refusals answer 401: a present-but-bad signature (Fail — no
		// fall-through to weaker credentials) and an unsigned request (Skip).
		r.record(ctx, store.WebhookDelivery{
			EndpointID:       ep.ID,
			Status:           http.StatusUnauthorized,
			Outcome:          store.WebhookOutcomeAuthFailed,
			SenderDeliveryID: r.senderDeliveryID(req),
		})
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	senderID := r.senderDeliveryID(req)
	if senderID != "" && r.seenDelivery(ctx, ep.ID, senderID) {
		// A redelivered sender delivery id within log retention: answer
		// duplicate without firing a second session or message (design D4).
		r.record(ctx, store.WebhookDelivery{
			EndpointID:       ep.ID,
			Status:           http.StatusOK,
			Outcome:          store.WebhookOutcomeDuplicate,
			Via:              r.viaLabel(label, req),
			SenderDeliveryID: senderID,
		})
		writeJSON(w, http.StatusOK, `{"duplicate":true}`)
		return
	}

	via := r.viaLabel(label, req)
	res, err := r.dispatch(ctx, ep, body)
	if err != nil {
		status, note := dispatchFailure(err)
		r.record(ctx, store.WebhookDelivery{
			EndpointID:       ep.ID,
			Status:           status,
			Outcome:          store.WebhookOutcomeFailed,
			Via:              via,
			SenderDeliveryID: senderID,
			Note:             note,
		})
		writeJSON(w, status, `{"error":"`+jsonEscape(note)+`"}`)
		return
	}

	r.record(ctx, store.WebhookDelivery{
		EndpointID:       ep.ID,
		Status:           http.StatusAccepted,
		Outcome:          res.outcome,
		Via:              via,
		SenderDeliveryID: senderID,
		SessionID:        res.sessionID,
		MessageID:        res.messageID,
	})
	writeJSON(w, http.StatusAccepted, `{"status":"accepted"}`)
}

// dispatchResult is what a dispatch produced: the delivery-log coordinates
// plus, for agent targets, the turn handle the cli-side completion announcer
// waits on.
type dispatchResult struct {
	outcome   string
	sessionID string
	messageID string
	turn      TurnView
}

// dispatch routes the delivery to its target: agent targets fire a fresh
// titled session (outcome accepted), group targets append one user-authored
// transcript message (outcome ran — the send path returns after the row is
// appended).
func (r *Receiver) dispatch(ctx context.Context, ep *store.WebhookEndpoint, body []byte) (dispatchResult, error) {
	if ep.AgentID != "" {
		return r.dispatchAgent(ctx, ep, body)
	}
	return r.dispatchGroup(ctx, ep, body)
}

// gatewayState reads the pause gate's input; a nil gateway (direct-http
// tests, pre-attach requests) reads as Running.
func (r *Receiver) gatewayState() gateway.State {
	if r.gw == nil {
		return gateway.StateRunning
	}
	return r.gw.State()
}

// senderDeliveryID extracts the sender's own delivery id — the Standard
// Webhooks webhook-id family — regardless of which verifier authenticates:
// the retry-dedupe marker rides the same header for every sender that
// provides one. Senders without it are never deduplicated.
func (r *Receiver) senderDeliveryID(req *http.Request) string {
	return strings.TrimSpace(req.Header.Get(StdWebIDHeader))
}

// seenDelivery reports whether the endpoint's log — bounded by retention —
// already holds a delivery with the sender's id. An unreadable log cannot
// prove a duplicate, so it delivers.
func (r *Receiver) seenDelivery(ctx context.Context, endpointID, senderID string) bool {
	rows, err := r.store.Webhooks().ListDeliveries(ctx, endpointID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.SenderDeliveryID == senderID {
			return true
		}
	}
	return false
}

// viaLabel names the authenticating verifier for the delivery log, or
// "self-test" for a delivery that rode the self-test marker
// (add-webhook-triggers design D8).
func (r *Receiver) viaLabel(label string, req *http.Request) string {
	if req.Header.Get(selfTestHeader) == "1" {
		return "self-test"
	}
	return label
}

// dispatchFailure maps a dispatch error onto its HTTP answer and the
// delivery log's Note: a target that no longer resolves is a misconfiguration
// the sender must stop retrying (410 Gone), anything else is transient and
// worth a retry (503 — the sender's retry policy is the queue).
func dispatchFailure(err error) (status int, note string) {
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusGone, err.Error()
	}
	return http.StatusServiceUnavailable, err.Error()
}

// jsonEscape renders s as the body of a JSON string literal — the notes are
// plain error text.
func jsonEscape(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(b[1 : len(b)-1])
}

// logRecordFailure reports a dropped delivery-log row.
func logRecordFailure(ctx context.Context, endpointID, outcome string, err error) {
	slog.WarnContext(ctx, "webhooks: delivery record failed",
		"endpoint_id", endpointID, "outcome", outcome, "error", err)
}
