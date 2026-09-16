package server_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
)

// -----------------------------------------------------------------------------
// WhatsApp gateway surface (add-whatsapp-gateway tasks 4.2/6.3)
// -----------------------------------------------------------------------------

const (
	waStubPhoneID   = "123456789012345"
	waStubAppSecret = "smoke-app-secret-123"
	waStubVerify    = "smoke-verify-token-123"
	waStubToken     = "EAAG-smoke-access-token"
)

// newWhatsAppEnv builds a test env whose cloud-lane adapters (factory and the
// pre-save credential probe) point at a stub Meta Graph endpoint: the
// phone-number probe succeeds and message sends are counted.
func newWhatsAppEnv(t *testing.T) (*testEnv, *httptest.Server, *atomic.Int64) {
	t.Helper()
	var sends atomic.Int64
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/"+waStubPhoneID):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"` + waStubPhoneID + `","verified_name":"Smoke Cloud","display_phone_number":"+1 555 000 1111","quality_rating":"GREEN"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			n := sends.Add(1)
			w.Header().Set("Content-Type", applicationJSON)
			_, _ = fmt.Fprintf(w, `{"messaging_product":"whatsapp","contacts":[],"messages":[{"id":"wamid.out-%d"}]}`, n)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
		}
	}))
	t.Cleanup(stub.Close)

	env := setupTestEnv(t, func(o *server.RouterOptions) {
		o.WhatsAppCloudAPIBase = stub.URL
	})
	return env, stub, &sends
}

const applicationJSON = "application/json"

// seedWhatsAppWorkspace creates an owner, a plain member, and a default agent on one workspace.
func seedWhatsAppWorkspace(t *testing.T, env *testEnv) (ownerToken, memberToken string, ws *domain.Workspace, agent *domain.Agent) {
	t.Helper()
	ctx := context.Background()
	owner, ownerToken := createTestUser(t, env, "wa-owner@example.com", "WA Owner", "pwd")
	member, memberToken := createTestUser(t, env, "wa-member@example.com", "WA Member", "pwd")
	ws, ownerRole, _, memberRole := createTestWorkspaceWithRoles(t, env, "wa-ws", "WA WS")
	addMember(t, env, ws.ID, owner.ID, ownerRole.ID)
	addMember(t, env, ws.ID, member.ID, memberRole.ID)

	provider := &domain.ProviderConfig{
		ID:          "p-wa-test-1",
		WorkspaceID: ws.ID,
		Type:        "openai",
		Name:        "OpenAI",
	}
	if err := env.store.Providers().Create(ctx, provider); err != nil {
		t.Fatalf("failed to create test provider: %v", err)
	}

	agent = &domain.Agent{
		ID:          "agent-wa-test-1",
		WorkspaceID: ws.ID,
		Name:        "Atlas",
		Slug:        "atlas",
		ProviderID:  provider.ID,
		Model:       "gpt-4",
	}
	if err := env.store.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("failed to create test agent: %v", err)
	}

	return ownerToken, memberToken, ws, agent
}

// rawRequest issues a request with explicit extra headers and a raw body —
// the webhook ingress and the X-Forwarded-Proto derivation need byte-level
// control (doRequest JSON-encodes).
func rawRequest(r interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, method, path, token string, rawBody []byte, headers map[string]string) *httptest.ResponseRecorder {
	var reader io.Reader
	if rawBody != nil {
		reader = bytes.NewReader(rawBody)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if rawBody != nil {
		req.Header.Set("Content-Type", applicationJSON)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// waHubSignature computes the X-Hub-Signature-256 value for a raw body.
func waHubSignature(body []byte) string {
	mac := hmac.New(sha256.New, []byte(waStubAppSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// createWhatsAppCloud sends a valid cloud-lane POST behind an https proxy header,
// so the derived webhook URL satisfies validation.
func createWhatsAppCloud(t *testing.T, env *testEnv, token, slug, agentID string) (handlers.ApiWhatsAppGatewayConfig, *httptest.ResponseRecorder) {
	t.Helper()
	w := rawRequest(env.router, http.MethodPost, "/api/v1/workspaces/"+slug+"/gateways/whatsapp", token, []byte(`{
		"lane": "cloud_api",
		"agent_id": "`+agentID+`",
		"access_token": "`+waStubToken+`",
		"phone_number_id": "`+waStubPhoneID+`",
		"app_secret": "`+waStubAppSecret+`",
		"verify_token": "`+waStubVerify+`"
	}`), map[string]string{"X-Forwarded-Proto": "https"})
	var created handlers.ApiWhatsAppGatewayConfig
	if w.Code == http.StatusCreated {
		_ = json.Unmarshal(w.Body.Bytes(), &created)
	}
	return created, w
}

// validationDetails decodes the field-level 422 envelope.
func validationDetails(t *testing.T, body []byte) map[string]bool {
	t.Helper()
	var decoded struct {
		Error struct {
			Details []struct {
				Field string `json:"field"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode validation error: %v (%s)", err, body)
	}
	fields := make(map[string]bool, len(decoded.Error.Details))
	for _, d := range decoded.Error.Details {
		fields[d.Field] = true
	}
	return fields
}

func TestWhatsAppGateway_AdminGates(t *testing.T) {
	env, _, _ := newWhatsAppEnv(t)
	_, memberToken, _, _ := seedWhatsAppWorkspace(t, env)
	base := "/api/v1/workspaces/wa-ws/gateways/whatsapp"

	// Unauthenticated reads are 401.
	if w := doRequest(env.router, http.MethodGet, base, "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated config read: status = %d", w.Code)
	}

	// A plain Member holds no gateways.write: every admin op is 403.
	adminPaths := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, base, nil},
		{http.MethodPost, base, map[string]any{"lane": "multi_device"}},
		{http.MethodGet, base + "/some-id", nil},
		{http.MethodPut, base + "/some-id", map[string]any{"lane": "multi_device"}},
		{http.MethodPost, base + "/some-id/enable", map[string]any{}},
		{http.MethodPost, base + "/some-id/disable", map[string]any{}},
		{http.MethodGet, base + "/some-id/health", nil},
		{http.MethodPost, base + "/some-id/pairing/start", map[string]any{}},
		{http.MethodGet, base + "/some-id/pairing/status", nil},
		{http.MethodPost, base + "/some-id/pairing/regenerate", map[string]any{}},
		{http.MethodPost, base + "/some-id/pairing/logout", map[string]any{}},
		{http.MethodDelete, base + "/some-id", nil},
	}
	for _, p := range adminPaths {
		if w := doRequest(env.router, p.method, p.path, memberToken, p.body); w.Code != http.StatusForbidden {
			t.Fatalf("member %s %s: status = %d, want 403", p.method, p.path, w.Code)
		}
	}

	// The member-level split mirrors Telegram: pairing tokens and the self
	// link ride membership only.
	if w := doRequest(env.router, http.MethodPost, base+"/pairing-tokens", memberToken, map[string]any{}); w.Code != http.StatusCreated {
		t.Fatalf("member mint pairing token: status = %d (%s)", w.Code, w.Body.String())
	}
	if w := doRequest(env.router, http.MethodGet, base+"/links/me", memberToken, nil); w.Code != http.StatusOK {
		t.Fatalf("member read own link: status = %d", w.Code)
	}
}

func TestWhatsAppGateway_LaneValidation(t *testing.T) {
	env, _, _ := newWhatsAppEnv(t)
	ownerToken, _, _, agent := seedWhatsAppWorkspace(t, env)
	base := "/api/v1/workspaces/wa-ws/gateways/whatsapp"

	// Unconfigured reads as empty list [], never 404.
	w := doRequest(env.router, http.MethodGet, base, ownerToken, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `[]`) {
		t.Fatalf("unconfigured read: status = %d body = %s", w.Code, w.Body.String())
	}

	// Missing lane.
	w = doRequest(env.router, http.MethodPost, base, ownerToken, map[string]any{"agent_id": agent.ID})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("lane-less POST: status = %d (%s)", w.Code, w.Body.String())
	}
	if fields := validationDetails(t, w.Body.Bytes()); !fields["lane"] {
		t.Fatalf("lane-less POST must field lane, got %s", w.Body.String())
	}

	// Missing agent_id.
	w = doRequest(env.router, http.MethodPost, base, ownerToken, map[string]any{"lane": "multi_device"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("agent-less POST: status = %d (%s)", w.Code, w.Body.String())
	}
	if fields := validationDetails(t, w.Body.Bytes()); !fields["agent_id"] {
		t.Fatalf("agent-less POST must field agent_id, got %s", w.Body.String())
	}

	// Unknown lane.
	w = doRequest(env.router, http.MethodPost, base, ownerToken, map[string]any{"lane": "sms", "agent_id": agent.ID})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown lane POST: status = %d", w.Code)
	}
	if fields := validationDetails(t, w.Body.Bytes()); !fields["lane"] {
		t.Fatalf("unknown lane POST must field lane, got %s", w.Body.String())
	}

	// Cloud lane with every credential missing: all four fields named.
	w = doRequest(env.router, http.MethodPost, base, ownerToken, map[string]any{"lane": "cloud_api", "agent_id": agent.ID})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("credential-less cloud POST: status = %d (%s)", w.Code, w.Body.String())
	}
	fields := validationDetails(t, w.Body.Bytes())
	for _, f := range []string{"access_token", "phone_number_id", "app_secret", "verify_token"} {
		if !fields[f] {
			t.Fatalf("cloud POST missing %s not reported, got %s", f, w.Body.String())
		}
	}

	// Long-polling transport on the cloud lane is rejected (webhook-only).
	w = doRequest(env.router, http.MethodPost, base, ownerToken, map[string]any{
		"lane":      "cloud_api",
		"agent_id":  agent.ID,
		"transport": "long_polling",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("long-polling cloud POST: status = %d (%s)", w.Code, w.Body.String())
	}
	if fields := validationDetails(t, w.Body.Bytes()); !fields["transport"] {
		t.Fatalf("long-polling cloud POST must field transport, got %s", w.Body.String())
	}
}

func TestWhatsAppGateway_EnvelopeNeverEchoed(t *testing.T) {
	env, _, _ := newWhatsAppEnv(t)
	ownerToken, _, _, agent := seedWhatsAppWorkspace(t, env)
	base := "/api/v1/workspaces/wa-ws/gateways/whatsapp"

	// Save the cloud config: the probe hits the stub and stores the verified
	// display identity.
	created, w := createWhatsAppCloud(t, env, ownerToken, "wa-ws", agent.ID)
	if w.Code != http.StatusCreated {
		t.Fatalf("cloud POST: status = %d (%s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !created.HasCredentials || created.Lane != "cloud_api" {
		t.Fatalf("cloud view wrong: %+v", created)
	}
	if created.BotUsername != "Smoke Cloud" {
		t.Fatalf("expected the probed display identity, got %+v", created.BotUsername)
	}
	if created.StatusError != "" {
		t.Fatalf("probe against the stub must succeed, got %s", created.StatusError)
	}
	if !strings.HasPrefix(created.WebhookURL, "https://example.com/api/v1/webhooks/whatsapp/") {
		t.Fatalf("webhook URL wrong: %q", created.WebhookURL)
	}
	for _, secret := range []string{waStubToken, waStubAppSecret, waStubVerify, `"v1:`} {
		if strings.Contains(body, secret) {
			t.Fatalf("cloud POST echoed secret %q: %s", secret, body)
		}
	}

	// GET: the same guarantees hold on reads.
	w = doRequest(env.router, http.MethodGet, base+"/"+created.ID, ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("config GET: status = %d", w.Code)
	}
	body = w.Body.String()
	for _, secret := range []string{waStubToken, waStubAppSecret, waStubVerify, `"v1:`} {
		if strings.Contains(body, secret) {
			t.Fatalf("config GET echoed secret %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, `"has_credentials":true`) {
		t.Fatalf("config GET lost has_credentials: %s", body)
	}
}

func TestWhatsAppWebhook_ChallengeAndSignature(t *testing.T) {
	env, _, _ := newWhatsAppEnv(t)
	ownerToken, _, _, agent := seedWhatsAppWorkspace(t, env)
	created, w := createWhatsAppCloud(t, env, ownerToken, "wa-ws", agent.ID)
	if w.Code != http.StatusCreated {
		t.Fatalf("cloud POST: status = %d (%s)", w.Code, w.Body.String())
	}

	verifyPath := fmt.Sprintf("/api/v1/webhooks/whatsapp/%s", created.ID)

	// Handshake: hub.mode=subscribe with the stored verify token echoes the
	// challenge verbatim.
	challenge := rawRequest(env.router, http.MethodGet,
		verifyPath+"?hub.mode=subscribe&hub.verify_token="+waStubVerify+"&hub.challenge=chall-42",
		"", nil, nil)
	if challenge.Code != http.StatusOK || challenge.Body.String() != "chall-42" {
		t.Fatalf("challenge echo: status = %d body = %q", challenge.Code, challenge.Body.String())
	}

	// Wrong token and wrong mode are bare 403s.
	w = rawRequest(env.router, http.MethodGet,
		verifyPath+"?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=chall-42", "", nil, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("wrong verify token: status = %d", w.Code)
	}
	w = rawRequest(env.router, http.MethodGet, verifyPath+"?hub.challenge=x", "", nil, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing hub.mode: status = %d", w.Code)
	}

	// POST: missing and bad signatures are rejected unauthenticated BEFORE
	// parsing — including for a gateway without a stored envelope (no
	// existence leak).
	statuses := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{"statuses":[{"id":"wamid.x","status":"delivered"}]}}]}]}`)
	for _, tc := range []struct {
		name    string
		wsPath  string
		headers map[string]string
	}{
		{"missing signature", verifyPath, nil},
		{"bad signature", verifyPath, map[string]string{"X-Hub-Signature-256": "sha256=deadbeef"}},
		{"unknown gateway", "/api/v1/webhooks/whatsapp/00000000-0000-0000-0000-000000000000", map[string]string{"X-Hub-Signature-256": waHubSignature(statuses)}},
	} {
		w = rawRequest(env.router, http.MethodPost, tc.wsPath, "", statuses, tc.headers)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", tc.name, w.Code)
		}
	}
}

func TestWhatsAppWebhook_Ingest(t *testing.T) {
	env, _, sends := newWhatsAppEnv(t)
	ownerToken, _, _, agent := seedWhatsAppWorkspace(t, env)
	created, w := createWhatsAppCloud(t, env, ownerToken, "wa-ws", agent.ID)
	if w.Code != http.StatusCreated {
		t.Fatalf("cloud POST: status = %d (%s)", w.Code, w.Body.String())
	}
	if w := doRequest(env.router, http.MethodPost, "/api/v1/workspaces/wa-ws/gateways/whatsapp/"+created.ID+"/enable", ownerToken, map[string]any{}); w.Code != http.StatusOK {
		t.Fatalf("enable: status = %d (%s)", w.Code, w.Body.String())
	}

	verifyPath := fmt.Sprintf("/api/v1/webhooks/whatsapp/%s", created.ID)

	// A statuses-only payload is a no-op: accepted, nothing sent.
	statuses := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"` + waStubPhoneID + `"},"statuses":[{"id":"wamid.s1","status":"delivered"}]}}]}]}`)
	w = rawRequest(env.router, http.MethodPost, verifyPath, "", statuses, map[string]string{"X-Hub-Signature-256": waHubSignature(statuses)})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("statuses-only: status = %d body = %s", w.Code, w.Body.String())
	}
	if n := sends.Load(); n != 0 {
		t.Fatalf("statuses-only payload must send nothing, got %d sends", n)
	}

	// A batched message from an unpaired sender gets the pairing hint once;
	// the duplicated id inside the batch (and the payload redelivery) drop
	// through the dedup ring — exactly one send total.
	message := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"` + waStubPhoneID + `"},"contacts":[{"wa_id":"15559998888","profile":{"name":"Sam"}}],"messages":[
		{"from":"15559998888","id":"wamid.in-1","timestamp":"1","type":"text","text":{"body":"hello"}},
		{"from":"15559998888","id":"wamid.in-1","timestamp":"1","type":"text","text":{"body":"hello"}}
	]}}]}]}`)
	for i := 0; i < 2; i++ {
		w = rawRequest(env.router, http.MethodPost, verifyPath, "", message, map[string]string{"X-Hub-Signature-256": waHubSignature(message)})
		if w.Code != http.StatusOK {
			t.Fatalf("message delivery %d: status = %d (%s)", i, w.Code, w.Body.String())
		}
	}
	if n := sends.Load(); n != 1 {
		t.Fatalf("duplicate message ids must dedup to one reply, got %d sends", n)
	}
}

func TestWhatsAppGateway_MDLaneShallow(t *testing.T) {
	env, _, _ := newWhatsAppEnv(t)
	ownerToken, _, _, agent := seedWhatsAppWorkspace(t, env)
	base := "/api/v1/workspaces/wa-ws/gateways/whatsapp"

	// The md lane stores no credential and starts running (the pane pairs
	// right after the lane save). In this env there is no device-store DSN,
	// so the sync surfaces its failure on status_error while the config
	// still saves — the saved shape is what this test pins.
	w := doRequest(env.router, http.MethodPost, base, ownerToken, map[string]any{"lane": "multi_device", "agent_id": agent.ID})
	if w.Code != http.StatusCreated {
		t.Fatalf("md POST: status = %d (%s)", w.Code, w.Body.String())
	}
	var created handlers.ApiWhatsAppGatewayConfig
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode md POST: %v", err)
	}
	if created.Lane != "multi_device" || !created.Enabled || created.HasCredentials {
		t.Fatalf("md view wrong: %+v", created)
	}
	if created.StatusError == "" {
		t.Fatal("md sync without a device-store DSN must surface status_error")
	}

	// Pairing status with no running adapter reads not_started — never a
	// phantom QR.
	w = doRequest(env.router, http.MethodGet, base+"/"+created.ID+"/pairing/status", ownerToken, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"not_started"`) {
		t.Fatalf("pairing status: status = %d body = %s", w.Code, w.Body.String())
	}

	// Pairing start without a running gateway is a 409 conflict.
	w = doRequest(env.router, http.MethodPost, base+"/"+created.ID+"/pairing/start", ownerToken, map[string]any{})
	if w.Code != http.StatusConflict {
		t.Fatalf("pairing start without adapter: status = %d (%s)", w.Code, w.Body.String())
	}

	// Health on an unrunning md lane reports error (not ok).
	w = doRequest(env.router, http.MethodGet, base+"/"+created.ID+"/health", ownerToken, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"error"`) {
		t.Fatalf("md health: status = %d body = %s", w.Code, w.Body.String())
	}
}

// TestWhatsAppGateway_HealthSurfacesDeadOutbox pins the health probe's
// dead-delivery signal (add-whatsapp-gateway design D4): window-expired
// deliveries die observably — in health, not only in logs. The count is
// scoped to the workspace's whatsapp gateway via the payload's gateway_id.
func TestWhatsAppGateway_HealthSurfacesDeadOutbox(t *testing.T) {
	env, _, _ := newWhatsAppEnv(t)
	ownerToken, _, ws, agent := seedWhatsAppWorkspace(t, env)
	base := "/api/v1/workspaces/wa-ws/gateways/whatsapp"

	// Save the cloud lane (the stub answers the probe) to get a gateway id.
	created, w := createWhatsAppCloud(t, env, ownerToken, "wa-ws", agent.ID)
	if w.Code != http.StatusCreated {
		t.Fatalf("cloud POST: status = %d (%s)", w.Code, w.Body.String())
	}
	if created.ID == "" {
		t.Fatalf("expected a gateway id, got %s", w.Body.String())
	}

	// Enable the gateway so the lane probe answers (the ok path degrades the
	// detail line; the count rides every response).
	w = doRequest(env.router, http.MethodPost, base+"/"+created.ID+"/enable", ownerToken, map[string]any{})
	if w.Code != http.StatusOK {
		t.Fatalf("enable: status = %d (%s)", w.Code, w.Body.String())
	}

	// No dead entries yet: the field exists with a zero count.
	w = doRequest(env.router, http.MethodGet, base+"/"+created.ID+"/health", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("health: status = %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"dead_outbox":0`) {
		t.Fatalf("expected dead_outbox 0, got %s", w.Body.String())
	}

	// Two dead deliveries for this gateway, one for another gateway, one
	// still pending — only the first two may surface.
	outbox := env.store.GatewayOutbox()
	ctx := context.Background()
	for i, payload := range []string{
		`{"gateway_id":"` + created.ID + `","chat_id":"111","body":"a","flavor":"whatsapp_text"}`,
		`{"gateway_id":"` + created.ID + `","chat_id":"112","body":"b","flavor":"whatsapp_text"}`,
		`{"gateway_id":"another-gateway","chat_id":"121","body":"c","flavor":"whatsapp_text"}`,
	} {
		e := &domain.OutboxEntry{WorkspaceID: ws.ID, SessionID: fmt.Sprintf("wa_dm_%d", i), Payload: []byte(payload)}
		if err := outbox.Enqueue(ctx, e); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if i < 2 {
			if err := outbox.MarkDead(ctx, ws.ID, e.ID); err != nil {
				t.Fatalf("mark dead: %v", err)
			}
		}
	}

	w = doRequest(env.router, http.MethodGet, base+"/"+created.ID+"/health", ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("health after dead: status = %d (%s)", w.Code, w.Body.String())
	}
	var health struct {
		Status     string `json:"status"`
		Detail     string `json:"detail"`
		DeadOutbox int64  `json:"dead_outbox"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health: %v (%s)", err, w.Body.String())
	}
	if health.DeadOutbox != 2 {
		t.Fatalf("expected dead_outbox 2, got %d (%s)", health.DeadOutbox, w.Body.String())
	}
	if !strings.Contains(health.Detail, "2 dead deliveries") {
		t.Fatalf("expected the degraded detail line, got %q", health.Detail)
	}
}
