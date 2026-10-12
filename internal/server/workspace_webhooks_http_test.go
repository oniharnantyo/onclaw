package server_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// -----------------------------------------------------------------------------
// Standalone workspace webhook HTTP surface (add-workspace-webhooks 5.2): the
// management sub-resource guards (integrations.write across the board —
// Member is 403), the CRUD + reveal-once lifecycle, the save-time filter
// rejection, the dry-run verdicts, the per-endpoint delivery log, and the
// public ingest route's ack bodies and no-enumeration parity.
// Fake stores, the real fallback runtime assembly.
// -----------------------------------------------------------------------------

type workspaceWebhookEnv struct {
	env       *testEnv
	ws        *domain.Workspace
	agent     *domain.Agent
	ownerTok  string
	memberTok string
}

func newWorkspaceWebhookEnv(t *testing.T, slug string) *workspaceWebhookEnv {
	t.Helper()
	env := setupTestEnv(t)

	ownerUser, ownerToken := createTestUser(t, env, slug+"-owner@example.com", "Owner", "pwd")
	memberUser, memberToken := createTestUser(t, env, slug+"-member@example.com", "Member", "pwd")
	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, slug, "Standalone WS")
	addMember(t, env, ws.ID, ownerUser.ID, ownerRole.ID)
	addMember(t, env, ws.ID, memberUser.ID, memberRole.ID)

	// The bound agent: the target binding validates uuid shape only at this
	// surface (existence is the runner's runtime concern).
	agent := &domain.Agent{WorkspaceID: ws.ID, Name: "Atlas", Slug: "atlas", CreatedBy: &ownerUser.ID}
	if err := env.store.Agents().Create(context.Background(), agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	return &workspaceWebhookEnv{
		env:       env,
		ws:        ws,
		agent:     agent,
		ownerTok:  ownerToken,
		memberTok: memberToken,
	}
}

func (we *workspaceWebhookEnv) managementBase() string {
	return "/api/v1/workspaces/" + we.ws.Slug + "/integrations/webhooks"
}

// createBody is a valid enabled create payload (hmac scheme unless mutated).
func (we *workspaceWebhookEnv) createBody() map[string]any {
	return map[string]any{
		"name":   "gitlab-mr-review",
		"scheme": domain.WorkspaceWebhookSchemeHmacSha256,
		"event_source": map[string]any{
			"kind": domain.WorkspaceWebhookEventSourceHeader,
			"name": "X-Gitlab-Event",
		},
		"delivery_id_source": "X-Gitlab-Event-UUID",
		"target_agent_id":    we.agent.ID,
		"target_kind":        "channel",
		"target_id":          "chan-code-review",
		"enabled":            true,
	}
}

// createEndpoint drives the management API's create and decodes the response
// (the reveal-once secret included).
func (we *workspaceWebhookEnv) createEndpoint(t *testing.T, body map[string]any) (id, secret string) {
	t.Helper()
	w := doRequest(we.env.router, http.MethodPost, we.managementBase(), we.ownerTok, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Secret    string `json:"secret"`
		IngestURL string `json:"ingest_url"`
		Webhook   struct {
			ID         string `json:"id"`
			SecretHint string `json:"secret_hint"`
		} `json:"webhook"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode create response: %v (%s)", err, w.Body.String())
	}
	if res.Webhook.ID == "" {
		t.Fatalf("no endpoint id in response: %s", w.Body.String())
	}
	if want := "/api/ingest/hooks/" + we.ws.Slug + "/" + res.Webhook.ID; res.IngestURL != want {
		t.Errorf("ingest URL = %q, want %q", res.IngestURL, want)
	}
	return res.Webhook.ID, res.Secret
}

// TestWorkspaceWebhookManagement_Guards: every management route is
// integrations.write (Member is 403 on reads too — the secret's reveal-once
// makes this the connect trust tier); an unauthenticated caller is 401.
func TestWorkspaceWebhookManagement_Guards(t *testing.T) {
	we := newWorkspaceWebhookEnv(t, "wswh-guard")
	ghostID := uuid.NewString()

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, we.managementBase(), nil},
		{http.MethodPost, we.managementBase(), we.createBody()},
		{http.MethodGet, we.managementBase() + "/" + ghostID, nil},
		{http.MethodPut, we.managementBase() + "/" + ghostID, map[string]any{}},
		{http.MethodDelete, we.managementBase() + "/" + ghostID, nil},
		{http.MethodPost, we.managementBase() + "/" + ghostID + "/rotate", nil},
		{http.MethodPost, we.managementBase() + "/" + ghostID + "/enable", nil},
		{http.MethodPost, we.managementBase() + "/" + ghostID + "/disable", nil},
		{http.MethodPost, we.managementBase() + "/" + ghostID + "/dry-run", map[string]any{"payload": map[string]any{}}},
		{http.MethodGet, we.managementBase() + "/" + ghostID + "/deliveries", nil},
	} {
		w := doRequest(we.env.router, tc.method, tc.path, we.memberTok, tc.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: member got %d, want 403 (%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	w := doRequest(we.env.router, http.MethodGet, we.managementBase(), "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous got %d, want 401", w.Code)
	}
}

// TestWorkspaceWebhookManagement_CRUD is the lifecycle round trip: create
// (reveal-once), read (hint only), update, enable/pause, rotate, delivery
// log, delete.
func TestWorkspaceWebhookManagement_CRUD(t *testing.T) {
	we := newWorkspaceWebhookEnv(t, "wswh-crud")
	ctx := context.Background()

	id, secret := we.createEndpoint(t, we.createBody())
	if secret == "" {
		t.Fatal("a generated secret must ride the create response")
	}

	t.Run("later reads carry only the hint", func(t *testing.T) {
		body := readBody(t, we.env.router, http.MethodGet, we.managementBase()+"/"+id, we.ownerTok, nil)
		if strings.Contains(body, secret) {
			t.Fatal("the plaintext secret leaked in a later read")
		}
		var res struct {
			Webhook struct {
				Enabled    bool   `json:"enabled"`
				SecretHint string `json:"secret_hint"`
			} `json:"webhook"`
		}
		if err := json.Unmarshal([]byte(body), &res); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
		}
		if !res.Webhook.Enabled || res.Webhook.SecretHint != secret[len(secret)-4:] {
			t.Errorf("expected enabled + the last-4 hint, got %+v", res.Webhook)
		}
	})

	t.Run("list flags the standalone row", func(t *testing.T) {
		body := readBody(t, we.env.router, http.MethodGet, we.managementBase(), we.ownerTok, nil)
		var res struct {
			Webhooks []struct {
				Source  string `json:"source"`
				Webhook struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"webhook"`
			} `json:"webhooks"`
		}
		if err := json.Unmarshal([]byte(body), &res); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
		}
		if len(res.Webhooks) != 1 || res.Webhooks[0].Source != "standalone" || res.Webhooks[0].Webhook.ID != id {
			t.Fatalf("expected one standalone row for the endpoint, got %s", body)
		}
	})

	t.Run("update is full-state", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodPut, we.managementBase()+"/"+id, we.ownerTok, map[string]any{
			"name":   "renamed-hook",
			"scheme": domain.WorkspaceWebhookSchemePlainToken,
			"event_source": map[string]any{
				"kind": domain.WorkspaceWebhookEventSourceHeader,
				"name": "X-Gitlab-Event",
			},
			"delivery_id_source": "X-Gitlab-Event-UUID",
			"target_agent_id":    we.agent.ID,
			"target_kind":        "channel",
			"target_id":          "chan-code-review",
			"enabled":            false,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("update got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Webhook struct {
				Name       string `json:"name"`
				Scheme     string `json:"scheme"`
				Enabled    bool   `json:"enabled"`
				SecretHint string `json:"secret_hint"`
			} `json:"webhook"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode: %v (%s)", err, w.Body.String())
		}
		if res.Webhook.Name != "renamed-hook" || res.Webhook.Scheme != domain.WorkspaceWebhookSchemePlainToken || res.Webhook.Enabled {
			t.Errorf("update not applied: %+v", res.Webhook)
		}
		if res.Webhook.SecretHint != secret[len(secret)-4:] {
			t.Error("update must not touch the credential")
		}
	})

	t.Run("enable and disable", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodPost, we.managementBase()+"/"+id+"/enable", we.ownerTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("enable got %d: %s", w.Code, w.Body.String())
		}
		w = doRequest(we.env.router, http.MethodPost, we.managementBase()+"/"+id+"/disable", we.ownerTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("disable got %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Webhook struct {
				Enabled bool `json:"enabled"`
			} `json:"webhook"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if res.Webhook.Enabled {
			t.Error("disable must pause the endpoint")
		}
		if err := we.env.store.WorkspaceWebhooks().Delete(ctx, we.ws.ID, id); err != nil {
			t.Fatalf("cleanup delete: %v", err)
		}
		// Recreate for the rest of the lifecycle (enabled, same name).
		body := we.createBody()
		body["name"] = "renamed-hook"
		id, secret = we.createEndpoint(t, body)
	})

	t.Run("rotate reveals once and replaces", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodPost, we.managementBase()+"/"+id+"/rotate", we.ownerTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("rotate got %d: %s", w.Code, w.Body.String())
		}
		newSecret := decodeJSONSecret(t, w.Body.String())
		if newSecret == "" || newSecret == secret {
			t.Fatalf("rotate must mint a fresh secret, got %q", newSecret)
		}
		if strings.Count(w.Body.String(), newSecret) != 1 {
			t.Fatal("the plaintext secret must appear exactly once")
		}
	})

	t.Run("delivery log reads empty", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodGet, we.managementBase()+"/"+id+"/deliveries", we.ownerTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("deliveries got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"entries":[]`) {
			t.Errorf("expected an empty entries array, got %s", w.Body.String())
		}
	})

	t.Run("delete removes the endpoint", func(t *testing.T) {
		w := doRequest(we.env.router, http.MethodDelete, we.managementBase()+"/"+id, we.ownerTok, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("delete got %d: %s", w.Code, w.Body.String())
		}
		w = doRequest(we.env.router, http.MethodGet, we.managementBase()+"/"+id, we.ownerTok, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET after delete got %d, want 404", w.Code)
		}
	})
}

// TestWorkspaceWebhookCreate_InvalidFilterRejected is the spec scenario at
// the API: the save rejects 400 with the parser's message and persists
// nothing; a BYO secret is never re-revealed.
func TestWorkspaceWebhookCreate_InvalidFilterRejected(t *testing.T) {
	we := newWorkspaceWebhookEnv(t, "wswh-filter")

	body := we.createBody()
	body["filter_expression"] = `event == ` // parse error
	w := doRequest(we.env.router, http.MethodPost, we.managementBase(), we.ownerTok, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter got %d, want 400: %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if !strings.Contains(envelope.Error.Message, "invalid filter expression") {
		t.Errorf("the rejection must carry the parser's message, got %q", envelope.Error.Message)
	}

	// Nothing persisted.
	listBody := readBody(t, we.env.router, http.MethodGet, we.managementBase(), we.ownerTok, nil)
	if !strings.Contains(listBody, `"webhooks":[]`) {
		t.Errorf("nothing may persist on a rejected create, got %s", listBody)
	}

	// BYO secret: stored, hinted, never re-revealed.
	byo := we.createBody()
	byo["secret"] = "my-own-shared-secret"
	byo["enabled"] = false
	w = doRequest(we.env.router, http.MethodPost, we.managementBase(), we.ownerTok, byo)
	if w.Code != http.StatusCreated {
		t.Fatalf("BYO create got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "my-own-shared-secret") {
		t.Fatal("a BYO secret must never be re-revealed")
	}
	if !strings.Contains(w.Body.String(), `"secret_hint":"cret"`) {
		t.Errorf("expected the last-4 hint, got %s", w.Body.String())
	}
}

// ingestStandalone delivers a payload to the public route.
func (we *workspaceWebhookEnv) ingestStandalone(endpointID string, header http.Header, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/ingest/hooks/"+we.ws.Slug+"/"+endpointID, strings.NewReader(body))
	req.Header = header
	w := httptest.NewRecorder()
	we.env.router.ServeHTTP(w, req)
	return w
}

// plainTokenDelivery builds a plain_token delivery's headers.
func plainTokenDelivery(token, event, deliveryID string) http.Header {
	header := http.Header{}
	header.Set("X-Gitlab-Token", token)
	header.Set("X-Gitlab-Event", event)
	header.Set("X-Gitlab-Event-UUID", deliveryID)
	header.Set("Content-Type", "application/json")
	return header
}

// hmacDelivery builds an hmac_sha256 delivery's headers.
func hmacDelivery(secret, event, deliveryID, payload string) http.Header {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	header := http.Header{}
	header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	header.Set("X-Gitlab-Event", event)
	header.Set("X-Gitlab-Event-UUID", deliveryID)
	header.Set("Content-Type", "application/json")
	return header
}

const wswhPayload = `{"object_kind":"merge_request","project":{"path_with_namespace":"team/api"}}`

// TestWorkspaceWebhookIngest_Parity is the D5 posture: unknown slug, unknown
// endpoint, bad signature, paused endpoint, and over-cap body all answer the
// SAME generic 404 with identical code and message — no enumeration oracle.
func TestWorkspaceWebhookIngest_Parity(t *testing.T) {
	we := newWorkspaceWebhookEnv(t, "wswh-parity")

	id, secret := we.createEndpoint(t, we.createBody())

	disabledBody := we.createBody()
	disabledBody["name"] = "paused-hook"
	disabledBody["enabled"] = false
	disabledID, disabledSecret := we.createEndpoint(t, disabledBody)

	cases := []struct {
		name       string
		slug       string
		endpointID string
		header     http.Header
		body       string
	}{
		{"unknown workspace slug", "no-such-ws", id, plainTokenDelivery("x", "push", "p1"), wswhPayload},
		{"unknown endpoint", we.ws.Slug, "no-such-endpoint", plainTokenDelivery("x", "push", "p2"), wswhPayload},
		{"bad signature", we.ws.Slug, id, hmacDelivery("wrong-secret", "push", "p3", wswhPayload), wswhPayload},
		{"missing signature", we.ws.Slug, id, plainTokenDelivery("", "push", "p4"), wswhPayload},
		{"paused endpoint", we.ws.Slug, disabledID, hmacDelivery(disabledSecret, "push", "p5", wswhPayload), wswhPayload},
		{"over-cap body", we.ws.Slug, id, hmacDelivery(secret, "push", "p6", wswhPayload), `{"blob":"` + strings.Repeat("a", 1<<20+64) + `"}`},
	}
	var wantCodeStr, wantMsg string
	for i, tc := range cases {
		w := we.ingestStandalone(tc.endpointID, tc.header, tc.body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d, want 404 (%s)", tc.name, w.Code, w.Body.String())
		}
		code, msg := errorEnvelopeFields(t, w.Body.String())
		if i == 0 {
			wantCodeStr, wantMsg = code, msg
			continue
		}
		if code != wantCodeStr || msg != wantMsg {
			t.Fatalf("%s: rejection body diverged: %q/%q vs %q/%q", tc.name, code, msg, wantCodeStr, wantMsg)
		}
	}
}

// TestWorkspaceWebhookIngest_Acks covers the ack bodies: the plain-token
// scheme end to end, the duplicate ack, the explicit filtered ack, and the
// GitHub-compat HMAC header.
func TestWorkspaceWebhookIngest_Acks(t *testing.T) {
	we := newWorkspaceWebhookEnv(t, "wswh-acks")

	// Plain-token endpoint with a filter: matching event submits, non-
	// matching event is the explicit ignored ack.
	plain := we.createBody()
	plain["scheme"] = domain.WorkspaceWebhookSchemePlainToken
	plain["filter_expression"] = `event == "merge_request"`
	plainID, plainSecret := we.createEndpoint(t, plain)

	w := we.ingestStandalone(plainID, plainTokenDelivery(plainSecret, "merge_request", "d1"), wswhPayload)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("a verified delivery must ack, got %d %s", w.Code, w.Body.String())
	}

	// The same delivery id redelivered: duplicate ack, still plain.
	w = we.ingestStandalone(plainID, plainTokenDelivery(plainSecret, "merge_request", "d1"), wswhPayload)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("a duplicate must ack, got %d %s", w.Code, w.Body.String())
	}

	// A non-matching event: the explicit ignored reason (spec), distinct
	// from the plain ack.
	w = we.ingestStandalone(plainID, plainTokenDelivery(plainSecret, "push", "d2"), wswhPayload)
	if w.Code != http.StatusOK {
		t.Fatalf("a filtered delivery must ack 200, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"ignored"`) || !strings.Contains(w.Body.String(), `"reason":"filter"`) {
		t.Fatalf("the filtered ack must carry the ignored reason, got %s", w.Body.String())
	}

	// The filter is visible in the delivery log with the resolved event.
	var entries struct {
		Entries []struct {
			Event   string `json:"event"`
			Outcome string `json:"outcome"`
		} `json:"entries"`
	}
	listBody := readBody(t, we.env.router, http.MethodGet, we.managementBase()+"/"+plainID+"/deliveries", we.ownerTok, nil)
	if err := json.Unmarshal([]byte(listBody), &entries); err != nil {
		t.Fatalf("decode deliveries: %v (%s)", err, listBody)
	}
	var sawFiltered bool
	for _, e := range entries.Entries {
		if e.Outcome == "filtered" {
			sawFiltered = true
			if e.Event != "push" {
				t.Errorf("filtered row event = %q, want the resolved push", e.Event)
			}
		}
	}
	if !sawFiltered {
		t.Fatalf("no filtered row in the delivery log: %s", listBody)
	}

	// GitHub-compat body HMAC endpoint (the proposal's X-Hub-Signature-256).
	hmacBody := we.createBody()
	hmacBody["name"] = "github-hook"
	hmacID, hmacSecret := we.createEndpoint(t, hmacBody)
	w = we.ingestStandalone(hmacID, hmacDelivery(hmacSecret, "merge_request", "h1", wswhPayload), wswhPayload)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("an HMAC-verified delivery must ack, got %d %s", w.Code, w.Body.String())
	}
}

// TestWorkspaceWebhookIngest_RateLimited pins the one enumerable rejection:
// an exhausted window answers 429 with a Retry-After header (spec: "answering
// excess with 429 and Retry-After").
func TestWorkspaceWebhookIngest_RateLimited(t *testing.T) {
	we := newWorkspaceWebhookEnv(t, "wswh-rl")

	// The bound agent does not exist: the queue's processing fails fast, so
	// the loop below exercises the limiter alone, not sixty real runs.
	body := we.createBody()
	body["target_agent_id"] = uuid.NewString()
	id, secret := we.createEndpoint(t, body)

	var got429 bool
	for i := 0; i < webhooks.StandaloneRateLimit+1; i++ {
		w := we.ingestStandalone(id, hmacDelivery(secret, "merge_request", "rl-"+strconv.Itoa(i), wswhPayload), wswhPayload)
		if w.Code == http.StatusTooManyRequests {
			got429 = true
			retryAfter := w.Header().Get("Retry-After")
			seconds, err := strconv.Atoi(retryAfter)
			if err != nil || seconds < 1 {
				t.Fatalf("the 429 must carry a numeric Retry-After >= 1, got %q", retryAfter)
			}
			break
		}
		if w.Code != http.StatusOK {
			t.Fatalf("delivery %d got %d, want 200: %s", i, w.Code, w.Body.String())
		}
	}
	if !got429 {
		t.Fatalf("the window must exhaust after %d deliveries", webhooks.StandaloneRateLimit)
	}
}

// TestWorkspaceWebhookDryRun covers the dry-run endpoint: match, ignored,
// error verdicts, and the missing-payload 400 — no side effects.
func TestWorkspaceWebhookDryRun(t *testing.T) {
	we := newWorkspaceWebhookEnv(t, "wswh-dryrun")

	body := we.createBody()
	body["event_source"] = map[string]any{
		"kind": domain.WorkspaceWebhookEventSourcePayloadField,
		"path": "object_kind",
	}
	body["filter_expression"] = `event == "merge_request" && project.path_with_namespace == "team/api"`
	id, _ := we.createEndpoint(t, body)

	dryRun := func(payload any) (int, string) {
		w := doRequest(we.env.router, http.MethodPost, we.managementBase()+"/"+id+"/dry-run", we.ownerTok, map[string]any{"payload": payload})
		return w.Code, w.Body.String()
	}

	code, bodyStr := dryRun(map[string]any{
		"object_kind": "merge_request",
		"project":     map[string]any{"path_with_namespace": "team/api"},
	})
	if code != http.StatusOK || !strings.Contains(bodyStr, `"outcome":"match"`) {
		t.Fatalf("expected a match verdict, got %d %s", code, bodyStr)
	}

	_, bodyStr = dryRun(map[string]any{
		"object_kind": "merge_request",
		"project":     map[string]any{"path_with_namespace": "team/frontend"},
	})
	if !strings.Contains(bodyStr, `"outcome":"ignored"`) {
		t.Fatalf("expected an ignored verdict, got %s", bodyStr)
	}

	_, bodyStr = dryRun(map[string]any{"object_kind": "merge_request"})
	if !strings.Contains(bodyStr, `"outcome":"error"`) {
		t.Fatalf("expected an error verdict for a missing field, got %s", bodyStr)
	}

	w := doRequest(we.env.router, http.MethodPost, we.managementBase()+"/"+id+"/dry-run", we.ownerTok, map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a dry run without a sample payload got %d, want 400", w.Code)
	}

	// The dry run never wrote: the delivery log stays empty.
	listBody := readBody(t, we.env.router, http.MethodGet, we.managementBase()+"/"+id+"/deliveries", we.ownerTok, nil)
	if !strings.Contains(listBody, `"entries":[]`) {
		t.Errorf("a dry run must have no side effects, got %s", listBody)
	}
}

// decodeJSONSecret decodes the reveal-once secret field.
func decodeJSONSecret(t *testing.T, body string) string {
	t.Helper()
	var res struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode secret from %s: %v", body, err)
	}
	return res.Secret
}

// errorEnvelopeFields extracts an error envelope's code and message.
func errorEnvelopeFields(t *testing.T, body string) (string, string) {
	t.Helper()
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

// readBody issues a request and returns the response body (status checked by
// the caller where it matters).
func readBody(t *testing.T, r *gin.Engine, method, path, token string, body any) string {
	t.Helper()
	return doRequest(r, method, path, token, body).Body.String()
}

// TestWorkspaceWebhookWorkspaceRuns: the workspace-wide run listing
// (add-webhook-channel-visibility 4.4) — submitted deliveries only,
// projected with the endpoint name, the bound agent, and the run status
// (legacy blank statuses read as completed). The feed rides
// integrations.write — the webhooks pane's trust tier — so the member
// token is 403 here, and unauthenticated is 401.
func TestWorkspaceWebhookWorkspaceRuns(t *testing.T) {
	we := newWorkspaceWebhookEnv(t, "wsruns")
	ctx := context.Background()

	id, _ := we.createEndpoint(t, we.createBody())

	// Seed the log directly: a submitted row finished completed, a
	// submitted legacy row with a blank status, and a rejected row.
	base := time.Now().UTC().Add(-time.Hour)
	entries := []domain.WebhookDeliveryLogEntry{
		{Event: "merge_request", Outcome: domain.WorkspaceWebhookDeliveryOutcomeSubmitted, RunID: "whstandalone_x_1", Status: domain.WorkspaceWebhookRunStatusCompleted, CreatedAt: base.Add(-time.Minute)},
		{Event: "push", Outcome: domain.WorkspaceWebhookDeliveryOutcomeSubmitted, RunID: "whstandalone_x_2", CreatedAt: base},
		{Event: "note", Outcome: domain.WorkspaceWebhookDeliveryOutcomeRejected, Detail: "bad signature", CreatedAt: base.Add(time.Minute)},
	}
	for _, e := range entries {
		if err := we.env.store.WorkspaceWebhooks().AppendDeliveryLog(ctx, we.ws.ID, id, e); err != nil {
			t.Fatalf("append log: %v", err)
		}
	}

	fetch := func(token string) *httptest.ResponseRecorder {
		return doRequest(we.env.router, http.MethodGet,
			"/api/v1/workspaces/"+we.ws.Slug+"/webhook-runs", token, nil)
	}

	w := fetch(we.ownerTok)
	if w.Code != http.StatusOK {
		t.Fatalf("owner got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Runs []struct {
			WebhookName string `json:"webhook_name"`
			Event       string `json:"event"`
			RunID       string `json:"run_id"`
			AgentID     string `json:"agent_id"`
			AgentName   string `json:"agent_name"`
			Status      string `json:"status"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(res.Runs) != 2 {
		t.Fatalf("expected the two submitted rows only, got %d: %s", len(res.Runs), w.Body.String())
	}
	// Newest first: the legacy row (no status) reads completed.
	if res.Runs[0].RunID != "whstandalone_x_2" || res.Runs[0].Status != "completed" {
		t.Fatalf("expected the legacy submitted row reading completed, got %+v", res.Runs[0])
	}
	if res.Runs[1].RunID != "whstandalone_x_1" || res.Runs[1].Status != "completed" || res.Runs[1].WebhookName == "" {
		t.Fatalf("expected the finished submitted row with endpoint name, got %+v", res.Runs[1])
	}
	if res.Runs[1].AgentID == "" || res.Runs[1].AgentName == "" {
		t.Fatalf("expected the bound agent projected, got %+v", res.Runs[1])
	}

	// integrations.write is the pane's trust tier: member 403, anonymous 401.
	if w := fetch(we.memberTok); w.Code != http.StatusForbidden {
		t.Fatalf("member got %d, want 403: %s", w.Code, w.Body.String())
	}
	if w := fetch(""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated got %d, want 401", w.Code)
	}
}
