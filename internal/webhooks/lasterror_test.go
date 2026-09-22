package webhooks_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// brokenPayload is a signed pull_request.opened delivery whose payload omits
// the whitelisted sender.login field — the fail-closed render drop (design.md
// risks: template/payload drift never silently emits an empty turn).
const brokenPayload = `{
	"action": "opened",
	"repository": {"full_name": "acme/api"},
	"pull_request": {"number": 7, "title": "No sender here", "html_url": "https://github.com/acme/api/pull/7"}
}`

// TestBuildView_LastErrorShape: the view carries last_error as null when
// clear — the key present, the value null — as {event, error, at} when a
// render drop is on record, and as null again (never an error) for a corrupt
// residue.
func TestBuildView_LastErrorShape(t *testing.T) {
	clear := webhooks.BuildView(&domain.ConnectionWebhook{
		WorkspaceID:  "ws-1",
		ConnectionID: "conn-1",
		Enabled:      true,
		Events:       []string{"push"},
	}, "acme", "")
	encoded, err := json.Marshal(clear)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"last_error":null`) {
		t.Fatalf("a clean connection must serve last_error:null, got %s", encoded)
	}

	corrupt := webhooks.BuildView(&domain.ConnectionWebhook{
		WorkspaceID:  "ws-1",
		ConnectionID: "conn-1",
		LastError:    "not json at all",
	}, "acme", "")
	if corrupt.LastError != nil {
		t.Fatalf("a corrupt residue must degrade to null, got %+v", corrupt.LastError)
	}

	failed := webhooks.BuildView(&domain.ConnectionWebhook{
		WorkspaceID:  "ws-1",
		ConnectionID: "conn-1",
		Enabled:      true,
		Events:       []string{"push"},
		LastError:    `{"event":"pull_request.opened","error":"webhook event render failed: payload field \"sender.login\" is missing","at":"2026-09-23T10:30:00Z"}`,
	}, "acme", "")
	encoded, err = json.Marshal(failed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var view struct {
		LastError *struct {
			Event string `json:"event"`
			Error string `json:"error"`
			At    string `json:"at"`
		} `json:"last_error"`
	}
	if err := json.Unmarshal(encoded, &view); err != nil {
		t.Fatalf("decode: %v (%s)", err, encoded)
	}
	if view.LastError == nil {
		t.Fatalf("expected the residue to surface, got %s", encoded)
	}
	if view.LastError.Event != "pull_request.opened" ||
		!strings.Contains(view.LastError.Error, "sender.login") ||
		view.LastError.At != "2026-09-23T10:30:00Z" {
		t.Fatalf("last_error fields wrong: %+v", view.LastError)
	}
}

// TestIngress_RenderFailurePersistsAndSuccessClears is the task 2.4 loop:
// a fail-closed render drop persists the residue on the connection's webhook
// state, the view shows it, and the next successful delivery render clears
// it. Store failures in the persistence path never fail the pipeline.
func TestIngress_RenderFailurePersistsAndSuccessClears(t *testing.T) {
	df := newDeliveryFixture(t, "wh-lasterror", domain.ConnectionWebhookTargetChannel, "")
	ctx := context.Background()

	// 1. The broken delivery acks (recorded, then dropped fail-closed) and
	// the residue lands on the connection.
	outcome, err := df.ing.Deliver(ctx, df.signedGithub(t, "pull_request", "le-1", brokenPayload))
	if err != nil || outcome != webhooks.OutcomeDelivered {
		t.Fatalf("deliver broken payload: %v (%v)", outcome, err)
	}
	var state *domain.ConnectionWebhook
	waitFor(t, 2*time.Second, func() bool {
		state, err = df.sd.webhooks.GetState(ctx, df.sd.WS.ID, df.sd.Connection.ID)
		return err == nil && state.LastError != ""
	})
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if !strings.Contains(state.LastError, `"pull_request.opened"`) || !strings.Contains(state.LastError, "missing") {
		t.Fatalf("residue must name the event and the failure, got %s", state.LastError)
	}
	// The envelope's timestamp is RFC3339 — the view surfaces it verbatim.
	var env struct {
		At string `json:"at"`
	}
	if err := json.Unmarshal([]byte(state.LastError), &env); err != nil {
		t.Fatalf("residue is not the JSON envelope: %v (%s)", err, state.LastError)
	}
	if _, err := time.Parse(time.RFC3339Nano, env.At); err != nil {
		t.Fatalf("residue timestamp %q is not RFC3339: %v", env.At, err)
	}
	view := webhooks.BuildView(state, df.sd.WS.Slug, "")
	if view.LastError == nil || view.LastError.Event != "pull_request.opened" {
		t.Fatalf("view must surface the residue, got %+v", view.LastError)
	}

	// 2. No turn was produced for the dropped delivery.
	time.Sleep(100 * time.Millisecond)
	if got := df.runner.count(); got != 0 {
		t.Fatalf("a fail-closed drop must not produce a turn (%d runs)", got)
	}

	// 3. The next successful delivery render clears the residue.
	outcome, err = df.ing.Deliver(ctx, df.signedGithub(t, "pull_request", "le-2", prOpenedPayload))
	if err != nil || outcome != webhooks.OutcomeDelivered {
		t.Fatalf("deliver valid payload: %v (%v)", outcome, err)
	}
	df.runner.waitRun(t)
	waitFor(t, 2*time.Second, func() bool {
		state, err = df.sd.webhooks.GetState(ctx, df.sd.WS.ID, df.sd.Connection.ID)
		return err == nil && state.LastError == ""
	})
	if err != nil {
		t.Fatalf("the residue must clear after a successful render, still %s (%v)", state.LastError, err)
	}
}

// waitFor polls until check passes or the deadline expires — the residue
// writes ride the queue's asynchronous dispatcher.
func waitFor(t *testing.T, within time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
