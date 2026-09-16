package whatsappcloud

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/gateways"
)

// webhookPayloadBytes marshals a batched webhook payload with the given
// messages and statuses (statuses as raw JSON, mirroring the wire). The
// payload carries one contact (wa 15551234567, profile name "Test User").
func webhookPayloadBytes(t *testing.T, messages []webhookMessage, statuses []json.RawMessage) []byte {
	t.Helper()
	contact := webhookContact{WaID: "15551234567"}
	contact.Profile.Name = "Test User"
	payload := webhookPayload{
		Object: "whatsapp_business_account",
		Entry: []webhookEntry{{
			ID: "waba-1",
			Changes: []webhookChange{{
				Field: "messages",
				Value: webhookValue{
					MessagingProduct: "whatsapp",
					Metadata:         webhookMetadata{DisplayPhoneNumber: "15550123456", PhoneNumberID: "15550123456"},
					Contacts:         []webhookContact{contact},
					Messages:         messages,
					Statuses:         statuses,
				},
			}},
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal webhook payload: %v", err)
	}
	return raw
}

// textWebhook builds a text message from a wa id.
func textWebhook(id, from, body string) webhookMessage {
	return webhookMessage{
		From: from,
		ID:   id,
		Type: "text",
		Text: &struct {
			Body string `json:"body"`
		}{Body: body},
	}
}

// mediaWebhook builds a media message of the given type.
func mediaWebhook(id, from, typ, mediaID, mimeType, caption string) webhookMessage {
	msg := webhookMessage{From: from, ID: id, Type: typ}
	switch typ {
	case "image":
		msg.Image = &webhookMedia{ID: mediaID, MimeType: mimeType, Caption: caption, FileSize: 128}
	case "video":
		msg.Video = &webhookMedia{ID: mediaID, MimeType: mimeType, Caption: caption, FileSize: 4096}
	case "sticker":
		msg.Sticker = &webhookMedia{ID: mediaID, MimeType: mimeType}
	case "audio":
		msg.Audio = &webhookAudio{ID: mediaID, MimeType: mimeType, Voice: caption == "voice", FileSize: 96}
	}
	return msg
}

func documentWebhook(id, from, mediaID, filename, caption string) webhookMessage {
	return webhookMessage{
		From: from,
		ID:   id,
		Type: "document",
		Document: &webhookDocument{
			ID:       mediaID,
			MimeType: "application/pdf",
			Filename: filename,
			Caption:  caption,
			FileSize: 2048,
		},
	}
}

// TestWebhookBatchParse covers the batched entry/changes/value walk
// (tasks.md 3.2): every consumable message kind normalizes, statuses are
// skipped, and the contact profile name becomes the sender's username.
func TestWebhookBatchParse(t *testing.T) {
	a, _, handler := newTestAdapter(t)

	raw := webhookPayloadBytes(t, []webhookMessage{
		textWebhook("wamid.t1", "15551234567", "hello agent"),
		mediaWebhook("wamid.i1", "15551234567", "image", "media-img", "image/jpeg", "what is this?"),
		mediaWebhook("wamid.v1", "15551234567", "video", "media-vid", "video/mp4", "watch this"),
		documentWebhook("wamid.d1", "15551234567", "media-doc", "report.pdf", "the report"),
		mediaWebhook("wamid.a1", "15551234567", "audio", "media-voice", "audio/ogg; codecs=opus", "voice"),
		mediaWebhook("wamid.a2", "15551234567", "audio", "media-audio", "audio/mp4", "file"),
	}, []json.RawMessage{
		json.RawMessage(`{"id":"wamid.t1","status":"delivered"}`),
		json.RawMessage(`{"id":"wamid.t1","status":"read"}`),
	})
	if err := a.IngestWebhook(context.Background(), raw); err != nil {
		t.Fatalf("IngestWebhook: %v", err)
	}

	if got := handler.messageCount(); got != 6 {
		t.Fatalf("ingested %d messages, want 6", got)
	}
	if got := handler.callbackCount(); got != 0 {
		t.Fatalf("ingested %d callbacks, want 0", got)
	}

	msgs := handler.messages
	assert := func(i int, check func(gateways.InboundMessage) []string) {
		t.Helper()
		for _, problem := range check(msgs[i]) {
			t.Errorf("message %d: %s", i, problem)
		}
	}

	assert(0, func(m gateways.InboundMessage) []string {
		var out []string
		if m.Platform != gateways.PlatformWhatsApp {
			out = append(out, "platform = "+m.Platform)
		}
		if m.Kind != gateways.InboundDM {
			out = append(out, "kind = "+string(m.Kind))
		}
		if m.ChatID != "15551234567" || m.FromUserID != "15551234567" {
			out = append(out, "chat/from = "+m.ChatID+"/"+m.FromUserID+" (wa ids are bare digits, design D7)")
		}
		if m.FromUsername != "Test User" {
			out = append(out, "username = "+m.FromUsername)
		}
		if m.MessageID != "wamid.t1" || m.Text != "hello agent" {
			out = append(out, "id/text = "+m.MessageID+"/"+m.Text)
		}
		return out
	})
	assert(1, func(m gateways.InboundMessage) []string {
		var out []string
		if m.Text != "what is this?" {
			out = append(out, "caption text = "+m.Text)
		}
		if len(m.Attachments) != 1 {
			return []string{"missing photo attachment"}
		}
		att := m.Attachments[0]
		if att.Kind != gateways.AttachmentPhoto || att.FileID != "media-img" || att.MimeType != "image/jpeg" {
			out = append(out, "photo attachment = "+string(att.Kind)+"/"+att.FileID+"/"+att.MimeType)
		}
		return out
	})
	assert(2, func(m gateways.InboundMessage) []string {
		// Video rides the document lane (design D11).
		if len(m.Attachments) != 1 || m.Attachments[0].Kind != gateways.AttachmentDocument {
			return []string{"video must map to AttachmentDocument"}
		}
		return nil
	})
	assert(3, func(m gateways.InboundMessage) []string {
		if len(m.Attachments) != 1 {
			return []string{"missing document attachment"}
		}
		att := m.Attachments[0]
		if att.Kind != gateways.AttachmentDocument || att.FileName != "report.pdf" || att.MimeType != "application/pdf" {
			return []string{"document attachment = " + string(att.Kind) + "/" + att.FileName + "/" + att.MimeType}
		}
		if m.Text != "the report" {
			return []string{"document caption = " + m.Text}
		}
		return nil
	})
	assert(4, func(m gateways.InboundMessage) []string {
		// Voice-capable audio maps to the voice kind (transcriber lane).
		if len(m.Attachments) != 1 || m.Attachments[0].Kind != gateways.AttachmentVoice {
			return []string{"voice audio must map to AttachmentVoice"}
		}
		return nil
	})
	assert(5, func(m gateways.InboundMessage) []string {
		// Non-voice audio is a plain file.
		if len(m.Attachments) != 1 || m.Attachments[0].Kind != gateways.AttachmentDocument {
			return []string{"non-voice audio must map to AttachmentDocument"}
		}
		return nil
	})
}

// TestWebhookStatusesOnlyNoop: a status-only payload (delivery receipts)
// never enters the gateway (design D8/D11).
func TestWebhookStatusesOnlyNoop(t *testing.T) {
	a, _, handler := newTestAdapter(t)
	raw := webhookPayloadBytes(t, nil, []json.RawMessage{
		json.RawMessage(`{"id":"wamid.s1","status":"delivered"}`),
	})
	if err := a.IngestWebhook(context.Background(), raw); err != nil {
		t.Fatalf("IngestWebhook: %v", err)
	}
	if handler.messageCount() != 0 || handler.callbackCount() != 0 {
		t.Fatalf("statuses-only payload ingested traffic: %d messages, %d callbacks",
			handler.messageCount(), handler.callbackCount())
	}
}

// TestWebhookUnknownAndMalformedDropped: zero uncaught parse errors —
// unknown types and malformed JSON drop silently.
func TestWebhookUnknownAndMalformedDropped(t *testing.T) {
	a, _, handler := newTestAdapter(t)

	unknown := webhookPayloadBytes(t, []webhookMessage{
		{From: "15551234567", ID: "wamid.u1", Type: "location"},
	}, nil)
	if err := a.IngestWebhook(context.Background(), unknown); err != nil {
		t.Fatalf("IngestWebhook(unknown type): %v", err)
	}
	if err := a.IngestWebhook(context.Background(), []byte("not json at all")); err != nil {
		t.Fatalf("IngestWebhook(malformed) = %v, want dropped nil", err)
	}
	if handler.messageCount() != 0 || handler.callbackCount() != 0 {
		t.Fatalf("unknown/malformed payloads ingested traffic")
	}
}

// TestWebhookDuplicateMessageIDDropped: Meta redelivers on retry — the dedup
// ring drops repeats, and distinct ids within one batch all land
// (tasks.md 3.4).
func TestWebhookDuplicateMessageIDDropped(t *testing.T) {
	a, _, handler := newTestAdapter(t)
	ctx := context.Background()

	payload := webhookPayloadBytes(t, []webhookMessage{
		textWebhook("wamid.dup", "15551234567", "once"),
	}, nil)
	if err := a.IngestWebhook(ctx, payload); err != nil {
		t.Fatalf("IngestWebhook: %v", err)
	}
	if err := a.IngestWebhook(ctx, payload); err != nil {
		t.Fatalf("IngestWebhook(redelivery): %v", err)
	}
	if got := handler.messageCount(); got != 1 {
		t.Fatalf("ingested %d messages across a redelivery, want 1", got)
	}

	batch := webhookPayloadBytes(t, []webhookMessage{
		textWebhook("wamid.b1", "15551234567", "one"),
		textWebhook("wamid.b2", "15551234567", "two"),
	}, nil)
	if err := a.IngestWebhook(ctx, batch); err != nil {
		t.Fatalf("IngestWebhook(batch): %v", err)
	}
	if got := handler.messageCount(); got != 3 {
		t.Fatalf("ingested %d messages total, want 3 (dup + two distinct)", got)
	}
}

// TestWebhookSelfEchoDropped: messages the business number itself sent come
// back through the webhook and must never re-ingest.
func TestWebhookSelfEchoDropped(t *testing.T) {
	a, _, handler := newTestAdapter(t)
	raw := webhookPayloadBytes(t, []webhookMessage{
		textWebhook("wamid.echo", "15550123456", "outbound echo"),
	}, nil)
	if err := a.IngestWebhook(context.Background(), raw); err != nil {
		t.Fatalf("IngestWebhook: %v", err)
	}
	if handler.messageCount() != 0 {
		t.Fatalf("self-echo ingested as inbound message")
	}
}

// TestWebhookForeignPhoneNumberDropped: a payload addressed to another phone
// number never ingests here.
func TestWebhookForeignPhoneNumberDropped(t *testing.T) {
	a, _, handler := newTestAdapter(t)
	raw := webhookPayloadBytes(t, []webhookMessage{
		textWebhook("wamid.x1", "15551234567", "someone else's"),
	}, nil)
	// Rewrite the metadata to a foreign number.
	var payload webhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	payload.Entry[0].Changes[0].Value.Metadata.PhoneNumberID = "19998887777"
	foreign, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := a.IngestWebhook(context.Background(), foreign); err != nil {
		t.Fatalf("IngestWebhook: %v", err)
	}
	if handler.messageCount() != 0 {
		t.Fatalf("foreign phone number payload ingested")
	}
}

// TestButtonReplyRoundTrip pins the approval bridge contract (tasks.md 3.4,
// design D3): the callback data minted with EncodeApprovalCallback comes
// back through a button_reply webhook VERBATIM and decodes to the same
// interrupt/decision.
func TestButtonReplyRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		approved bool
		title    string
	}{
		{"approve button", true, "Approve"},
		{"deny button", false, "Deny"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, handler := newTestAdapter(t)
			data := gateways.EncodeApprovalCallback("int-abc-123", tc.approved)

			msg := webhookMessage{
				From: "15551234567",
				ID:   "wamid.reply-1",
				Type: "interactive",
				Interactive: &webhookInteractive{
					Type: "button_reply",
					ButtonReply: &struct {
						ID    string `json:"id"`
						Title string `json:"title"`
					}{ID: data, Title: tc.title},
				},
				Context: &struct {
					From string `json:"from"`
					ID   string `json:"id"`
				}{ID: "wamid.card-1"},
			}
			raw := webhookPayloadBytes(t, []webhookMessage{msg}, nil)
			if err := a.IngestWebhook(context.Background(), raw); err != nil {
				t.Fatalf("IngestWebhook: %v", err)
			}
			if got := handler.callbackCount(); got != 1 {
				t.Fatalf("ingested %d callbacks, want 1", got)
			}
			if got := handler.messageCount(); got != 0 {
				t.Fatalf("button reply ingested %d messages, want 0", got)
			}
			cb := handler.callbacks[0]
			if cb.Platform != gateways.PlatformWhatsApp || cb.ChatID != "15551234567" || cb.FromUserID != "15551234567" {
				t.Fatalf("callback coordinates = %+v", cb)
			}
			if cb.MessageID != "wamid.card-1" {
				t.Fatalf("callback MessageID = %q, want the card id from context", cb.MessageID)
			}
			if cb.Data != data {
				t.Fatalf("callback Data = %q, want verbatim %q (the adapter must never parse or re-encode)", cb.Data, data)
			}
			interruptID, approved, ok := gateways.DecodeApprovalCallback(cb.Data)
			if !ok || interruptID != "int-abc-123" || approved != tc.approved {
				t.Fatalf("DecodeApprovalCallback(%q) = %q, %v, %v", cb.Data, interruptID, approved, ok)
			}
		})
	}
}

// TestStickerRefusedWithNotice pins the sticker refusal (design D11): a
// notice goes out to the chat and the sticker never ingests.
func TestStickerRefusedWithNotice(t *testing.T) {
	a, fake, handler := newTestAdapter(t)
	raw := webhookPayloadBytes(t, []webhookMessage{
		mediaWebhook("wamid.st1", "15551234567", "sticker", "media-sticker", "image/webp", ""),
	}, nil)
	if err := a.IngestWebhook(context.Background(), raw); err != nil {
		t.Fatalf("IngestWebhook: %v", err)
	}
	if handler.messageCount() != 0 {
		t.Fatalf("sticker ingested as inbound message, want refusal")
	}
	sends := fake.CallsTo(OpSendMessage)
	if len(sends) != 1 {
		t.Fatalf("got %d refusal sends, want 1", len(sends))
	}
	var payload struct {
		To   string `json:"to"`
		Type string `json:"type"`
		Text struct {
			Body string `json:"body"`
		} `json:"text"`
	}
	if err := json.Unmarshal(sends[0].Body, &payload); err != nil {
		t.Fatalf("decode refusal: %v", err)
	}
	if payload.To != "15551234567" || payload.Type != "text" ||
		!strings.Contains(payload.Text.Body, "Stickers aren't supported") {
		t.Fatalf("refusal notice = %+v, want unsupported-sticker copy to the sender", payload)
	}
}

// TestVerifySignature pins the X-Hub-Signature-256 check (tasks.md 3.4,
// design D8): HMAC-SHA256 of the raw body with the app secret, hex after
// "sha256=", constant-time compared, refusals on any malformed input.
func TestVerifySignature(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	mac := hmac.New(sha256.New, []byte("app-secret"))
	mac.Write(body)
	valid := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	cases := []struct {
		name   string
		secret string
		body   []byte
		header string
		want   bool
	}{
		{"valid signature", "app-secret", body, valid, true},
		{"wrong secret", "other-secret", body, valid, false},
		{"tampered body", "app-secret", []byte(`{"object":"tampered"}`), valid, false},
		{"missing sha256= prefix", "app-secret", body, hex.EncodeToString(mac.Sum(nil)), false},
		{"empty header", "app-secret", body, "", false},
		{"empty secret", "", body, valid, false},
		{"non-hex digest", "app-secret", body, "sha256=zzzz", false},
		{"wrong digest length", "app-secret", body, "sha256=abcd", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifySignature(tc.secret, tc.body, tc.header); got != tc.want {
				t.Fatalf("VerifySignature(%q, body, %q) = %v, want %v", tc.secret, tc.header, got, tc.want)
			}
		})
	}
}

// TestTypingHeartbeatPinned documents the pinned typing cadence (openspec
// add-whatsapp-gateway task 8.1: live spike pending — the constant exists so
// composition can wire the streamer's typing interval per lane).
func TestTypingHeartbeatPinned(t *testing.T) {
	if TypingHeartbeat != 25*time.Second {
		t.Fatalf("TypingHeartbeat = %v, want the pinned 25s", TypingHeartbeat)
	}
}

// TestWindowSentinelsChain: ErrWindowExpired wraps ErrPermanentDelivery so a
// single errors.Is check dead-letters (design D4).
func TestWindowSentinelsChain(t *testing.T) {
	if !errors.Is(ErrWindowExpired, ErrPermanentDelivery) {
		t.Fatalf("ErrWindowExpired must wrap ErrPermanentDelivery")
	}
}
