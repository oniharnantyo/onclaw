package server_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// -----------------------------------------------------------------------------
// Connection webhook HTTP surface (add-connection-webhooks 5.2): the
// management sub-resource guards (integrations.write across the board —
// Member is 403) and the public ingest route's non-enumerating rejections.
// Fake stores, the real fallback runtime assembly.
// -----------------------------------------------------------------------------

type webhookEnv struct {
	env       *testEnv
	ws        *domain.Workspace
	conn      *domain.Connection
	agent     *domain.Agent
	ownerTok  string
	memberTok string
}

func newWebhookEnv(t *testing.T, slug string) *webhookEnv {
	t.Helper()
	env := setupTestEnv(t)

	ownerUser, ownerToken := createTestUser(t, env, slug+"-owner@example.com", "Owner", "pwd")
	memberUser, memberToken := createTestUser(t, env, slug+"-member@example.com", "Member", "pwd")
	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, slug, "Webhook WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	// The github connection and the bound agent land directly in the store
	// (the connect flow's probe gating is another change's concern; the
	// webhook surface starts from an existing connection).
	conn := &domain.Connection{WorkspaceID: ws.ID, Service: "github", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := env.store.Connections().Create(context.Background(), conn); err != nil {
		t.Fatalf("create connection: %v", err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Name: "Atlas", Slug: "atlas", CreatedBy: &ownerUser.ID}
	if err := env.store.Agents().Create(context.Background(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	return &webhookEnv{
		env:       env,
		ws:        ws,
		conn:      conn,
		agent:     agent,
		ownerTok:  ownerToken,
		memberTok: memberToken,
	}
}

func (we *webhookEnv) managementBase() string {
	return "/api/v1/workspaces/" + we.ws.Slug + "/integrations/connections/" + we.conn.ID + "/webhook"
}

func TestWebhookManagement_Guards(t *testing.T) {
	we := newWebhookEnv(t, "wh-guard")
	base := we.managementBase()

	// Every management route is integrations.write: Member is 403 on reads
	// too (the secret's reveal-once makes this the connect trust tier).
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, base, nil},
		{http.MethodPost, base + "/enable", map[string]any{}},
		{http.MethodPost, base + "/disable", nil},
		{http.MethodPost, base + "/rotate", nil},
		{http.MethodPut, base + "/target", map[string]any{}},
		{http.MethodPut, base + "/events", map[string]any{}},
	} {
		w := doRequest(we.env.router, tc.method, tc.path, we.memberTok, tc.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: member got %d, want 403 (%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	// An unauthenticated caller is 401, not 404 — the route exists behind
	// the workspace gate.
	w := doRequest(we.env.router, http.MethodGet, base, "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous got %d, want 401", w.Code)
	}

	// The owner reads the inert default.
	w = doRequest(we.env.router, http.MethodGet, base, we.ownerTok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("owner GET got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Webhook struct {
			Enabled    bool     `json:"enabled"`
			SecretHint string   `json:"secret_hint"`
			IngestURL  string   `json:"ingest_url"`
			Events     []string `json:"events"`
		} `json:"webhook"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if res.Webhook.Enabled {
		t.Error("a fresh connection reads disabled")
	}
	if res.Webhook.IngestURL != "/api/ingest/webhooks/"+we.ws.Slug+"/"+we.conn.ID {
		t.Errorf("derived ingest URL wrong: %q", res.Webhook.IngestURL)
	}

	// A connection whose recipe declares no webhooks is invalid on this
	// sub-resource.
	figma := &domain.Connection{WorkspaceID: we.ws.ID, Service: "figma", AccessLevel: domain.ConnectionAccessReadOnly}
	if err := we.env.store.Connections().Create(context.Background(), figma); err != nil {
		t.Fatalf("create figma connection: %v", err)
	}
	w = doRequest(we.env.router, http.MethodGet, "/api/v1/workspaces/"+we.ws.Slug+"/integrations/connections/"+figma.ID+"/webhook", we.ownerTok, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("GET webhook on a non-webhook recipe got %d, want 400", w.Code)
	}
}

func TestWebhookManagement_EnableRotateDisplayOnce(t *testing.T) {
	we := newWebhookEnv(t, "wh-manage")
	base := we.managementBase()
	ctx := context.Background()

	var secret string
	t.Run("enable reveals the secret exactly once", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodPost, base+"/enable", we.ownerTok, map[string]any{
			"agent_id":    we.agent.ID,
			"target_kind": "channel",
			"target_id":   "no-such-channel",
			"events":      []string{"pull_request.opened"},
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("enable with an unknown channel got %d, want 400: %s", w.Code, w.Body.String())
		}

		channel := &domain.Channel{WorkspaceID: we.ws.ID, Name: "incidents", Slug: "incidents"}
		if err := we.env.store.Channels().CreateChannel(ctx, channel); err != nil {
			t.Fatalf("create channel: %v", err)
		}

		w = doRequest(we.env.router, http.MethodPost, base+"/enable", we.ownerTok, map[string]any{
			"agent_id":    we.agent.ID,
			"target_kind": "channel",
			"target_id":   channel.ID,
			"events":      []string{"pull_request.opened", "not-in-catalog"},
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("out-of-catalog events got %d, want 400: %s", w.Code, w.Body.String())
		}

		w = doRequest(we.env.router, http.MethodPost, base+"/enable", we.ownerTok, map[string]any{
			"agent_id":    we.agent.ID,
			"target_kind": "channel",
			"target_id":   channel.ID,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("enable got %d: %s", w.Code, w.Body.String())
		}
		secret = decodeSecret(t, w.Body.String())

		// The response never leaks the secret in any other field.
		if strings.Count(w.Body.String(), secret) != 1 {
			t.Fatalf("the plaintext secret must appear exactly once: %s", w.Body.String())
		}
	})

	t.Run("later reads carry only the hint", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodGet, base, we.ownerTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET got %d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("the plaintext secret leaked in a later read")
		}
		var res struct {
			Webhook struct {
				Enabled    bool   `json:"enabled"`
				SecretHint string `json:"secret_hint"`
			} `json:"webhook"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !res.Webhook.Enabled || res.Webhook.SecretHint != secret[len(secret)-4:] {
			t.Errorf("expected enabled + the last-4 hint, got %+v", res.Webhook)
		}
	})

	t.Run("rotate replaces the secret immediately", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodPost, base+"/rotate", we.ownerTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("rotate got %d: %s", w.Code, w.Body.String())
		}
		newSecret := decodeSecret(t, w.Body.String())
		if newSecret == secret {
			t.Fatal("rotation must mint a fresh secret")
		}

		// The old secret no longer verifies at the ingest route; the new
		// one does.
		payload := `{"action":"opened"}`
		if code, body := we.ingestSigned("push", "rotate-old", payload, secret); code != http.StatusNotFound {
			t.Fatalf("the rotated-out secret must be rejected, got %d %s", code, body)
		}
		if code, body := we.ingestSigned("push", "rotate-new", payload, newSecret); code != http.StatusOK {
			t.Fatalf("the rotated-in secret must verify, got %d %s", code, body)
		}
	})

	t.Run("target and events PUT", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodPut, base+"/events", we.ownerTok, map[string]any{
			"events": []string{"issues.opened", "push"},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("events PUT got %d: %s", w.Code, w.Body.String())
		}
		w = doRequest(we.env.router, http.MethodPut, base+"/target", we.ownerTok, map[string]any{
			"agent_id":    we.agent.ID,
			"target_kind": "thread",
			"target_id":   "sess_thread_1",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("target PUT got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Webhook struct {
				Target struct {
					TargetKind string `json:"target_kind"`
					TargetID   string `json:"target_id"`
				} `json:"target"`
				Events []string `json:"events"`
			} `json:"webhook"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if res.Webhook.Target.TargetKind != "thread" || res.Webhook.Target.TargetID != "sess_thread_1" {
			t.Errorf("target not rebound: %+v", res.Webhook)
		}
	})
}

func decodeSecret(t *testing.T, body string) string {
	t.Helper()
	var res struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode secret from %s: %v", body, err)
	}
	if res.Secret == "" {
		t.Fatalf("no secret in response: %s", body)
	}
	return res.Secret
}

// ingestSigned delivers a GitHub-style signed payload to the public route.
func (we *webhookEnv) ingestSigned(eventType, deliveryID, payload, secret string) (int, string) {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	req := httptest.NewRequest(http.MethodPost,
		"/api/ingest/webhooks/"+we.ws.Slug+"/"+we.conn.ID, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	w := httptest.NewRecorder()
	we.env.router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// The ingest rejection tests (5.2): unsigned/unknown are indistinguishable.
func TestWebhookIngest_NonEnumeratingRejections(t *testing.T) {
	we := newWebhookEnv(t, "wh-ingest")
	base := "/api/ingest/webhooks/" + we.ws.Slug + "/" + we.conn.ID

	// Unknown workspace.
	wUnknownWS := doRequest(we.env.router, http.MethodPost, "/api/ingest/webhooks/no-such-ws/"+we.conn.ID, "", map[string]any{})
	// Unknown connection.
	wUnknownConn := doRequest(we.env.router, http.MethodPost, "/api/ingest/webhooks/"+we.ws.Slug+"/no-such-conn", "", map[string]any{})
	// Unsigned delivery to a real (but disabled) connection.
	wUnsigned := doRequest(we.env.router, http.MethodPost, base, "", map[string]any{})

	if wUnknownWS.Code != http.StatusNotFound || wUnknownConn.Code != http.StatusNotFound || wUnsigned.Code != http.StatusNotFound {
		t.Fatalf("expected 404s, got %d/%d/%d", wUnknownWS.Code, wUnknownConn.Code, wUnsigned.Code)
	}
	// Identical error code and message (request ids are per-request by
	// design): no enumeration oracle between unknown targets and unsigned
	// deliveries.
	errOf := func(body string) (string, string) {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(body), &envelope); err != nil {
			t.Fatalf("decode error envelope: %v (%s)", err, body)
		}
		return envelope.Error.Code, envelope.Error.Message
	}
	wsCode, wsMsg := errOf(wUnknownWS.Body.String())
	connCode, connMsg := errOf(wUnknownConn.Body.String())
	unsignedCode, unsignedMsg := errOf(wUnsigned.Body.String())
	if wsCode != connCode || connCode != unsignedCode || wsMsg != connMsg || connMsg != unsignedMsg {
		t.Fatalf("rejection bodies must be identical: %s | %s | %s",
			wUnknownWS.Body.String(), wUnknownConn.Body.String(), wUnsigned.Body.String())
	}

	// A signed delivery to a disabled connection is also the generic 404.
	if code, body := we.ingestSigned("pull_request", "disabled-1", prOpenedPayloadHTTP, "any-secret"); code != http.StatusNotFound {
		t.Fatalf("a disabled connection must reject generically, got %d %s", code, body)
	}
}

func TestWebhookIngest_SignedDeliveryAcksAndDedupes(t *testing.T) {
	we := newWebhookEnv(t, "wh-signed")
	ctx := context.Background()

	// Enable through the management API to obtain the secret (the full
	// round trip: enable → signed delivery).
	channel := &domain.Channel{WorkspaceID: we.ws.ID, Name: "incidents", Slug: "incidents"}
	if err := we.env.store.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	w := doRequest(we.env.router, http.MethodPost, we.managementBase()+"/enable", we.ownerTok, map[string]any{
		"agent_id":    we.agent.ID,
		"target_kind": "channel",
		"target_id":   channel.ID,
		"events":      []string{"pull_request.opened"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("enable got %d: %s", w.Code, w.Body.String())
	}
	secret := decodeSecret(t, w.Body.String())

	// A signed, selected delivery acks — the queued run is processed
	// asynchronously (its agent turn is covered by the webhooks package's
	// end-to-end tests with a stubbed runner; here the provider-facing
	// contract is what the HTTP layer owns).
	code, body := we.ingestSigned("pull_request", "delivery-1", prOpenedPayloadHTTP, secret)
	if code != http.StatusOK || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("expected a 2xx ack, got %d %s", code, body)
	}

	// The redelivery is deduped: another ack, still the provider contract.
	code, _ = we.ingestSigned("pull_request", "delivery-1", prOpenedPayloadHTTP, secret)
	if code != http.StatusOK {
		t.Fatalf("the replay must also ack, got %d", code)
	}

	// An unselected event acks too.
	time.Sleep(50 * time.Millisecond)
	code, _ = we.ingestSigned("release", "delivery-2", `{"action":"published"}`, secret)
	if code != http.StatusOK {
		t.Fatalf("an unselected event must ack, got %d", code)
	}
}

const prOpenedPayloadHTTP = `{
	"action": "opened",
	"repository": {"full_name": "acme/api"},
	"pull_request": {"number": 42, "title": "Fix the login race", "html_url": "https://github.com/acme/api/pull/42"},
	"sender": {"login": "alice"}
}`

// prOpenedBrokenPayloadHTTP omits the whitelisted sender.login field: the
// fail-closed render drop whose residue task 2.4 surfaces on the connection.
const prOpenedBrokenPayloadHTTP = `{
	"action": "opened",
	"repository": {"full_name": "acme/api"},
	"pull_request": {"number": 7, "title": "No sender here", "html_url": "https://github.com/acme/api/pull/7"}
}`

// TestWebhookIngest_RenderFailureSurfacesOnConnection (task 2.4): a
// fail-closed render drop shows up as webhook.last_error on the connection's
// webhook view — event and timestamp named — and the next successful delivery
// render clears it back to null.
func TestWebhookIngest_RenderFailureSurfacesOnConnection(t *testing.T) {
	we := newWebhookEnv(t, "wh-lasterr")
	ctx := context.Background()

	channel := &domain.Channel{WorkspaceID: we.ws.ID, Name: "incidents", Slug: "incidents"}
	if err := we.env.store.Channels().CreateChannel(ctx, channel); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	w := doRequest(we.env.router, http.MethodPost, we.managementBase()+"/enable", we.ownerTok, map[string]any{
		"agent_id":    we.agent.ID,
		"target_kind": "channel",
		"target_id":   channel.ID,
		"events":      []string{"pull_request.opened"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("enable got %d: %s", w.Code, w.Body.String())
	}
	secret := decodeSecret(t, w.Body.String())

	lastErrorOf := func(t *testing.T) (event, at string, present bool) {
		t.Helper()
		w := doRequest(we.env.router, http.MethodGet, we.managementBase(), we.ownerTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET webhook view got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Webhook struct {
				LastError *struct {
					Event string `json:"event"`
					At    string `json:"at"`
				} `json:"last_error"`
			} `json:"webhook"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode: %v (%s)", err, w.Body.String())
		}
		if res.Webhook.LastError == nil {
			return "", "", false
		}
		return res.Webhook.LastError.Event, res.Webhook.LastError.At, true
	}

	// The clean connection serves last_error:null — the key present.
	if ev, _, present := lastErrorOf(t); present {
		t.Fatalf("a fresh connection must serve last_error:null, got event %q", ev)
	}

	// The broken delivery acks (recorded, dropped fail-closed) and the
	// residue surfaces on the view.
	if code, body := we.ingestSigned("pull_request", "le-http-1", prOpenedBrokenPayloadHTTP, secret); code != http.StatusOK {
		t.Fatalf("broken delivery must ack, got %d %s", code, body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		ev, at, present := lastErrorOf(t)
		if present {
			if ev != "pull_request.opened" {
				t.Fatalf("residue event = %q, want pull_request.opened", ev)
			}
			if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
				t.Fatalf("residue timestamp %q is not RFC3339: %v", at, err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the render failure never surfaced on the connection view")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The next successful delivery render clears the residue.
	if code, body := we.ingestSigned("pull_request", "le-http-2", prOpenedPayloadHTTP, secret); code != http.StatusOK {
		t.Fatalf("valid delivery must ack, got %d %s", code, body)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		_, _, present := lastErrorOf(t)
		if !present {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the residue must clear after a successful delivery render")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
