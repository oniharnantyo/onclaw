package whatsappcloud

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/gateways"
)

// approvalInterrupt builds the approval payload the bridge hands the
// adapter.
func approvalInterrupt(id, command string) agents.ApprovalPayload {
	return agents.ApprovalPayload{InterruptID: id, Command: command}
}

// recordingHandler captures the normalized traffic the adapter delivers.
type recordingHandler struct {
	mu        sync.Mutex
	messages  []gateways.InboundMessage
	callbacks []gateways.Callback
}

func (h *recordingHandler) HandleMessage(_ context.Context, _ string, msg gateways.InboundMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg)
}

func (h *recordingHandler) HandleCallback(_ context.Context, _ string, cb gateways.Callback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callbacks = append(h.callbacks, cb)
}

func (h *recordingHandler) messageCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.messages)
}

func (h *recordingHandler) callbackCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.callbacks)
}

const testCredentialJSON = `{"access_token":"eaag-token","phone_number_id":"15550123456","app_secret":"app-secret","verify_token":"verify-token"}`

// newTestAdapter builds an adapter over the fake transport.
func newTestAdapter(t *testing.T) (*Adapter, *FakeTransport, *recordingHandler) {
	t.Helper()
	handler := &recordingHandler{}
	fake := NewFakeTransport()
	a, err := NewAdapter("gw-1", testCredentialJSON, handler, WithTransport(fake))
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return a, fake, handler
}

// TestParseCredentials validates the decrypted envelope contract (tasks.md
// 3.3, design D5): valid JSON with all four fields parses; anything else is
// a clear ErrInvalid.
func TestParseCredentials(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid", testCredentialJSON, false},
		{"not json", "raw-token-text", true},
		{"truncated json", `{"access_token":`, true},
		{"missing access_token", `{"phone_number_id":"1","app_secret":"s","verify_token":"v"}`, true},
		{"missing phone_number_id", `{"access_token":"a","app_secret":"s","verify_token":"v"}`, true},
		{"missing app_secret", `{"access_token":"a","phone_number_id":"1","verify_token":"v"}`, true},
		{"missing verify_token", `{"access_token":"a","phone_number_id":"1","app_secret":"s"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			creds, err := ParseCredentials(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseCredentials(%q) = %+v, want error", tc.in, creds)
				}
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("ParseCredentials(%q) error = %v, want ErrInvalid", tc.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCredentials(%q): %v", tc.in, err)
			}
			if creds.AccessToken != "eaag-token" || creds.PhoneNumberID != "15550123456" ||
				creds.AppSecret != "app-secret" || creds.VerifyToken != "verify-token" {
				t.Fatalf("ParseCredentials(%q) = %+v, fields not populated", tc.in, creds)
			}
		})
	}
}

// TestNewAdapterRequiresGatewayIDAndHandler pins the constructor validation.
func TestNewAdapterRequiresGatewayIDAndHandler(t *testing.T) {
	if _, err := NewAdapter("", testCredentialJSON, &recordingHandler{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewAdapter(empty gateway id) error = %v, want ErrInvalid", err)
	}
	if _, err := NewAdapter("gw-1", testCredentialJSON, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewAdapter(nil handler) error = %v, want ErrInvalid", err)
	}
	if _, err := NewAdapter("gw-1", `{`, &recordingHandler{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewAdapter(malformed credential) error = %v, want ErrInvalid", err)
	}
}

// TestCapabilities pins the cloud lane's feature surface (design D2/D3): no
// edit endpoint, working quick-reply buttons.
func TestCapabilities(t *testing.T) {
	a, _, _ := newTestAdapter(t)
	caps := a.Capabilities()
	if caps.CanEdit {
		t.Fatalf("Capabilities().CanEdit = true, want false (cloud api has no edit)")
	}
	if !caps.CanButton {
		t.Fatalf("Capabilities().CanButton = false, want true (quick replies work)")
	}
}

// TestSendMessageFlavorAndPayload checks the flavor contract (design D9: a
// foreign flavor is refused, never misparsed) and the text payload shape.
func TestSendMessageFlavorAndPayload(t *testing.T) {
	a, fake, _ := newTestAdapter(t)

	cases := []struct {
		name    string
		flavor  string
		wantErr bool
	}{
		{"empty flavor is the adapter's own format", "", false},
		{"whatsapp markdown", gateways.FlavorWhatsAppMD, false},
		{"telegram html refused", gateways.FlavorTelegramHTML, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake.Calls = nil
			id, err := a.SendMessage(context.Background(), "15551234567", "hello", tc.flavor, gateways.SendOptions{})
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "cannot deliver") {
					t.Fatalf("SendMessage(flavor=%q) = %q, %v; want refused", tc.flavor, id, err)
				}
				if calls := fake.CallsTo(OpSendMessage); len(calls) != 0 {
					t.Fatalf("refused flavor made %d send calls, want 0", len(calls))
				}
				return
			}
			if err != nil {
				t.Fatalf("SendMessage(flavor=%q): %v", tc.flavor, err)
			}
			calls := fake.CallsTo(OpSendMessage)
			if len(calls) != 1 {
				t.Fatalf("got %d send calls, want 1", len(calls))
			}
			var payload struct {
				MessagingProduct string `json:"messaging_product"`
				RecipientType    string `json:"recipient_type"`
				To               string `json:"to"`
				Type             string `json:"type"`
				Text             struct {
					Body string `json:"body"`
				} `json:"text"`
			}
			if err := json.Unmarshal(calls[0].Body, &payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if payload.MessagingProduct != "whatsapp" || payload.RecipientType != "individual" ||
				payload.To != "15551234567" || payload.Type != "text" || payload.Text.Body != "hello" {
				t.Fatalf("payload = %+v, want addressed whatsapp text 'hello'", payload)
			}
			if !strings.HasPrefix(id, "wamid.fake-") {
				t.Fatalf("SendMessage id = %q, want a fake wamid", id)
			}
		})
	}
}

// TestSendMessageBodyBudgetBackstop pins the 4,096-rune refusal (design D9):
// the core splitter budgets under the cap, so an oversized body must fail
// loudly here rather than on the wire.
func TestSendMessageBodyBudgetBackstop(t *testing.T) {
	a, _, _ := newTestAdapter(t)
	if _, err := a.SendMessage(context.Background(), "chat", strings.Repeat("a", bodyHardLimit), "", gateways.SendOptions{}); err != nil {
		t.Fatalf("SendMessage(exactly %d runes): %v", bodyHardLimit, err)
	}
	if _, err := a.SendMessage(context.Background(), "chat", strings.Repeat("a", bodyHardLimit+1), "", gateways.SendOptions{}); !errors.Is(err, ErrBodyTooLong) {
		t.Fatalf("SendMessage(%d runes) error = %v, want ErrBodyTooLong", bodyHardLimit+1, err)
	}
}

// TestSendMessageNoPlainTextRetry documents the parse-fallback parity with
// the telegram adapter (tasks.md 3.4): on the cloud lane the wire format
// cannot be parse-rejected (unknown WhatsApp markup renders literally,
// design D9 — the flavor's PlainFallback is the identity), so a send failure
// is terminal and exactly one send attempt is made. Retrying stripped
// content would double-send under at-least-once delivery.
func TestSendMessageNoPlainTextRetry(t *testing.T) {
	a, fake, _ := newTestAdapter(t)
	fake.SetError(OpSendMessage, errors.New("graph down"))
	if _, err := a.SendMessage(context.Background(), "chat", "*hello*", "", gateways.SendOptions{}); err == nil {
		t.Fatalf("SendMessage over a failed transport = nil error, want failure")
	}
	if calls := fake.CallsTo(OpSendMessage); len(calls) != 1 {
		t.Fatalf("got %d send calls after failure, want exactly 1 (no plain-text retry)", len(calls))
	}
}

// TestSendMessagePermanentClassification pins the error taxonomy (tasks.md
// 3.1, design D4): 131047 / its subcode family classify window-expired and
// permanent, other 4xx classify permanent, 5xx and transport failures stay
// retryable.
func TestSendMessagePermanentClassification(t *testing.T) {
	cases := []struct {
		name          string
		transportErr  error
		wantWindow    bool
		wantPermanent bool
	}{
		{
			name:          "code 131047 window expired",
			transportErr:  &APIError{Op: OpSendMessage, Status: 400, Code: 131047, Message: "re-engagement message"},
			wantWindow:    true,
			wantPermanent: true,
		},
		{
			name:          "canonical subcode 2105006",
			transportErr:  &APIError{Op: OpSendMessage, Status: 400, Subcode: 2105006, Message: "more than 24 hours have passed"},
			wantWindow:    true,
			wantPermanent: true,
		},
		{
			name:          "other 4xx permanent-ish",
			transportErr:  &APIError{Op: OpSendMessage, Status: 400, Code: 100, Message: "invalid parameter"},
			wantPermanent: true,
		},
		{
			name:          "5xx retryable",
			transportErr:  &APIError{Op: OpSendMessage, Status: 500, Code: 1, Message: "internal"},
			wantPermanent: false,
		},
		{
			name:          "transport timeout retryable",
			transportErr:  errors.New("context deadline exceeded"),
			wantPermanent: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, fake, _ := newTestAdapter(t)
			fake.SetError(OpSendMessage, tc.transportErr)
			_, err := a.SendMessage(context.Background(), "chat", "hello", "", gateways.SendOptions{})
			if err == nil {
				t.Fatalf("SendMessage = nil error, want %v", tc.transportErr)
			}
			if got := errors.Is(err, ErrWindowExpired); got != tc.wantWindow {
				t.Fatalf("errors.Is(err, ErrWindowExpired) = %v, want %v (err: %v)", got, tc.wantWindow, err)
			}
			if got := IsPermanent(err); got != tc.wantPermanent {
				t.Fatalf("IsPermanent(err) = %v, want %v (err: %v)", got, tc.wantPermanent, err)
			}
			if tc.wantPermanent && !errors.Is(err, ErrPermanentDelivery) {
				t.Fatalf("permanent error does not unwrap to ErrPermanentDelivery: %v", err)
			}
		})
	}
}

// TestEditMessageNotSupported pins the honest edit contract (design D2): the
// capability-aware core never calls this, and a stray call fails loudly with
// ErrEditingNotSupported instead of silently dropping.
func TestEditMessageNotSupported(t *testing.T) {
	a, fake, _ := newTestAdapter(t)
	err := a.EditMessage(context.Background(), "chat", "wamid.1", "new body", "")
	if !errors.Is(err, ErrEditingNotSupported) {
		t.Fatalf("EditMessage error = %v, want ErrEditingNotSupported", err)
	}
	if calls := len(fake.Calls); calls != 0 {
		t.Fatalf("EditMessage made %d transport calls, want 0", calls)
	}
}

// TestSendTypingAnchorsLastInbound checks the mark-read + typing call shape
// (tasks.md 3.4): it lands on the chat's last inbound message id, and is
// no-op-safe when nothing inbound is tracked.
func TestSendTypingAnchorsLastInbound(t *testing.T) {
	ctx := context.Background()
	a, fake, _ := newTestAdapter(t)

	// Nothing inbound yet: safe no-op, no call.
	if err := a.SendTyping(ctx, "15551234567"); err != nil {
		t.Fatalf("SendTyping before any inbound: %v", err)
	}
	if calls := fake.CallsTo(OpMarkRead); len(calls) != 0 {
		t.Fatalf("SendTyping before any inbound made %d calls, want 0", len(calls))
	}

	// Ingest one message, then type: read + typing on that id.
	payload := webhookPayloadBytes(t, []webhookMessage{
		textWebhook("wamid.in-1", "15551234567", "hi"),
	}, nil)
	if err := a.IngestWebhook(ctx, payload); err != nil {
		t.Fatalf("IngestWebhook: %v", err)
	}
	if err := a.SendTyping(ctx, "15551234567"); err != nil {
		t.Fatalf("SendTyping: %v", err)
	}
	calls := fake.CallsTo(OpMarkRead)
	if len(calls) != 1 {
		t.Fatalf("got %d mark-read calls, want 1", len(calls))
	}
	var body struct {
		MessagingProduct string `json:"messaging_product"`
		Status           string `json:"status"`
		MessageID        string `json:"message_id"`
		TypingIndicator  *struct {
			Type string `json:"type"`
		} `json:"typing_indicator"`
	}
	if err := json.Unmarshal(calls[0].Body, &body); err != nil {
		t.Fatalf("decode mark-read body: %v", err)
	}
	if body.MessagingProduct != "whatsapp" || body.Status != "read" || body.MessageID != "wamid.in-1" {
		t.Fatalf("mark-read body = %+v, want status=read on wamid.in-1", body)
	}
	if body.TypingIndicator == nil || body.TypingIndicator.Type != "text" {
		t.Fatalf("typing_indicator = %+v, want text indicator", body.TypingIndicator)
	}

	// A newer inbound moves the anchor.
	payload = webhookPayloadBytes(t, []webhookMessage{
		textWebhook("wamid.in-2", "15551234567", "again"),
	}, nil)
	if err := a.IngestWebhook(ctx, payload); err != nil {
		t.Fatalf("IngestWebhook: %v", err)
	}
	if err := a.SendTyping(ctx, "15551234567"); err != nil {
		t.Fatalf("SendTyping: %v", err)
	}
	calls = fake.CallsTo(OpMarkRead)
	if len(calls) != 2 {
		t.Fatalf("got %d mark-read calls, want 2", len(calls))
	}
	var latest struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(calls[1].Body, &latest); err != nil {
		t.Fatalf("decode mark-read body: %v", err)
	}
	if latest.MessageID != "wamid.in-2" {
		t.Fatalf("latest anchor = %q, want wamid.in-2", latest.MessageID)
	}
}

// TestSendApprovalCard checks the interactive card (design D3): two
// quick-reply buttons whose ids are EncodeApprovalCallback verbatim, titles
// inside the ~20-char cap, command echoed in the body.
func TestSendApprovalCard(t *testing.T) {
	a, fake, _ := newTestAdapter(t)
	interrupt := approvalInterrupt("int-123", "rm -rf /tmp/x")
	id, err := a.SendApprovalCard(context.Background(), "15551234567", interrupt)
	if err != nil {
		t.Fatalf("SendApprovalCard: %v", err)
	}
	if id != "wamid.fake-0001" {
		t.Fatalf("card id = %q, want fake id", id)
	}

	calls := fake.CallsTo(OpSendMessage)
	if len(calls) != 1 {
		t.Fatalf("got %d send calls, want 1", len(calls))
	}
	var payload interactivePayload
	if err := json.Unmarshal(calls[0].Body, &payload); err != nil {
		t.Fatalf("decode card payload: %v", err)
	}
	if payload.MessagingProduct != "whatsapp" || payload.To != "15551234567" || payload.Type != "interactive" {
		t.Fatalf("card envelope = %+v, want addressed interactive message", payload)
	}
	if payload.Interactive.Type != "button" {
		t.Fatalf("interactive type = %q, want button", payload.Interactive.Type)
	}
	if !strings.Contains(payload.Interactive.Body.Text, "rm -rf /tmp/x") {
		t.Fatalf("card body = %q, want the command echoed", payload.Interactive.Body.Text)
	}
	buttons := payload.Interactive.Action.Buttons
	if len(buttons) != 2 {
		t.Fatalf("got %d buttons, want 2", len(buttons))
	}
	wantTitles := []string{"Approve", "Deny"}
	wantIDs := []string{
		gateways.EncodeApprovalCallback(interrupt.InterruptID, true),
		gateways.EncodeApprovalCallback(interrupt.InterruptID, false),
	}
	for i, b := range buttons {
		if b.Type != "reply" {
			t.Fatalf("button %d type = %q, want reply", i, b.Type)
		}
		if b.Reply.Title != wantTitles[i] {
			t.Fatalf("button %d title = %q, want %q", i, b.Reply.Title, wantTitles[i])
		}
		if len([]rune(b.Reply.Title)) > buttonTitleLimit {
			t.Fatalf("button %d title exceeds %d chars: %q", i, buttonTitleLimit, b.Reply.Title)
		}
		if b.Reply.ID != wantIDs[i] {
			t.Fatalf("button %d id = %q, want EncodeApprovalCallback verbatim %q", i, b.Reply.ID, wantIDs[i])
		}
	}
}

// TestDownloadFile pins the media pipeline (tasks.md 3.1): media id →
// resolved URL → bytes.
func TestDownloadFile(t *testing.T) {
	a, fake, _ := newTestAdapter(t)
	fake.Files["https://media.fake/media-1"] = []byte("file-bytes")
	data, err := a.DownloadFile(context.Background(), "media-1")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if string(data) != "file-bytes" {
		t.Fatalf("DownloadFile = %q, want file-bytes", data)
	}
	media := fake.CallsTo(OpMedia)
	if len(media) != 1 || media[0].Path != "media-1" {
		t.Fatalf("media resolve calls = %+v, want one for media-1", media)
	}
	downloads := fake.CallsTo(OpDownload)
	if len(downloads) != 1 || downloads[0].Path != "https://media.fake/media-1" {
		t.Fatalf("download calls = %+v, want one for the resolved URL", downloads)
	}
}

// TestDownloadFileEmptyURL fails loudly when the media metadata carries no
// URL.
func TestDownloadFileEmptyURL(t *testing.T) {
	a, fake, _ := newTestAdapter(t)
	fake.SetResult(OpMedia, map[string]string{"id": "media-1", "url": ""})
	if _, err := a.DownloadFile(context.Background(), "media-1"); err == nil || !strings.Contains(err.Error(), "empty url") {
		t.Fatalf("DownloadFile error = %v, want empty-url failure", err)
	}
}

// TestStartProbesAndValidates: Start validates the credential via the
// phone-number probe, is idempotent, and refuses restarts after Stop.
func TestStartProbesAndValidates(t *testing.T) {
	ctx := context.Background()
	a, fake, _ := newTestAdapter(t)

	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if calls := fake.CallsTo(OpPhoneNumber); len(calls) != 1 {
		t.Fatalf("Start made %d probe calls, want 1", len(calls))
	}
	// Idempotent: no second probe.
	if err := a.Start(ctx); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if calls := fake.CallsTo(OpPhoneNumber); len(calls) != 1 {
		t.Fatalf("after second Start: %d probe calls, want 1", len(calls))
	}
	if err := a.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := a.Start(ctx); err == nil {
		t.Fatalf("Start after Stop = nil error, want failure")
	}
}

// TestStartProbeFailure: a bad credential fails Start (the lifecycle
// manager's Sync then skips the gateway — the getMe analogue).
func TestStartProbeFailure(t *testing.T) {
	a, fake, _ := newTestAdapter(t)
	fake.SetError(OpPhoneNumber, &APIError{Op: OpPhoneNumber, Status: 401, Code: 190, Message: "access token expired"})
	if err := a.Start(context.Background()); err == nil {
		t.Fatalf("Start over a rejected credential = nil error, want failure")
	}
}
