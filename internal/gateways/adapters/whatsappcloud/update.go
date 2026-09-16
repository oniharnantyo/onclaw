package whatsappcloud

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/gateways"
)

// Raw Cloud API webhook shapes — only the fields the gateway normalizes
// (add-whatsapp-gateway design D8). Unknown fields are ignored by
// encoding/json, so a Meta payload addition can never break ingestion (zero
// uncaught parse errors). Statuses are deliberately not modeled beyond their
// presence: delivery receipts are ignored (design D8/D11).

type webhookPayload struct {
	Object string         `json:"object"`
	Entry  []webhookEntry `json:"entry"`
}

type webhookEntry struct {
	ID      string          `json:"id"` // WhatsApp Business Account id
	Changes []webhookChange `json:"changes"`
}

type webhookChange struct {
	Field string       `json:"field"`
	Value webhookValue `json:"value"`
}

type webhookValue struct {
	MessagingProduct string            `json:"messaging_product"`
	Metadata         webhookMetadata   `json:"metadata"`
	Contacts         []webhookContact  `json:"contacts"`
	Messages         []webhookMessage  `json:"messages"`
	Statuses         []json.RawMessage `json:"statuses"` // skipped: delivery receipts never re-enter the gateway
}

type webhookMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

type webhookContact struct {
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
	WaID string `json:"wa_id"`
}

type webhookMessage struct {
	From      string `json:"from"` // wa id — bare digits on the cloud lane (design D7)
	ID        string `json:"id"`   // wamid.* platform message id
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Text      *struct {
		Body string `json:"body"`
	} `json:"text"`
	Image       *webhookMedia       `json:"image"`
	Video       *webhookMedia       `json:"video"`
	Document    *webhookDocument    `json:"document"`
	Audio       *webhookAudio       `json:"audio"`
	Sticker     *webhookMedia       `json:"sticker"`
	Interactive *webhookInteractive `json:"interactive"`
	Context     *struct {
		From string `json:"from"`
		ID   string `json:"id"` // the quoted (card) message id
	} `json:"context"`
	// "button" (template quick replies) and every other type parse into
	// nothing: this gateway never mints templates (design D4), and the
	// unknown-type switch case drops them.
}

type webhookMedia struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	Caption  string `json:"caption"`
	FileSize int64  `json:"file_size"`
}

type webhookDocument struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	Filename string `json:"filename"`
	Caption  string `json:"caption"`
	FileSize int64  `json:"file_size"`
}

type webhookAudio struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	Voice    bool   `json:"voice"`
	FileSize int64  `json:"file_size"`
}

type webhookInteractive struct {
	Type        string `json:"type"` // "button_reply"
	ButtonReply *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"button_reply"`
}

// stickerRefusalCopy is the notice the adapter posts when a sticker arrives
// (add-whatsapp-gateway design D11: stickers refused with a notice — the
// attachment pipeline has no sticker lane). Format-agnostic plain text.
const stickerRefusalCopy = "Stickers aren't supported here yet — please send the content as a photo, document, or voice note."

// VerifySignature validates the X-Hub-Signature-256 header Meta sends with
// every webhook delivery: HMAC-SHA256 over the raw body keyed with the app
// secret, hex-encoded after a "sha256=" prefix (add-whatsapp-gateway design
// D8). Constant-time compare; any missing or malformed input refuses.
func VerifySignature(appSecret string, body []byte, signatureHeader string) bool {
	if appSecret == "" || signatureHeader == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signatureHeader, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

// IngestWebhook implements the gateway.WebhookReceiver capability: one raw
// batched payload from the public ingress handler (after X-Hub-Signature-256
// validation — the route owns the check, the adapter owns the parse).
// Malformed payloads are dropped, so this never fails on body content.
func (a *Adapter) IngestWebhook(ctx context.Context, raw []byte) error {
	var payload webhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		slog.Warn("whatsapp cloud webhook: undecodable payload dropped", "gateway_id", a.gatewayID, "err", err)
		return nil
	}
	a.processWebhook(ctx, &payload)
	return nil
}

// processWebhook fans one batched payload out to the gateway service:
// messages become InboundMessages (button replies become Callbacks),
// statuses and unknown shapes are skipped (design D8).
func (a *Adapter) processWebhook(ctx context.Context, payload *webhookPayload) {
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			value := change.Value

			// Defense against misrouted deliveries: this webhook route only
			// carries our phone number's events, but a payload addressed to
			// another number must not ingest here.
			if value.Metadata.PhoneNumberID != "" && value.Metadata.PhoneNumberID != a.creds.PhoneNumberID {
				slog.Warn("whatsapp cloud webhook: payload for another phone number dropped",
					"gateway_id", a.gatewayID, "payload_phone_number_id", value.Metadata.PhoneNumberID)
				continue
			}

			contacts := make(map[string]string, len(value.Contacts))
			for _, c := range value.Contacts {
				contacts[c.WaID] = c.Profile.Name
			}

			for i := range value.Messages {
				a.processMessage(ctx, &value.Messages[i], contacts)
			}
			// value.Statuses: delivery receipts — deliberately ignored
			// (design D8/D11). Nothing to consume, nothing to log per id.
		}
	}
}

// processMessage normalizes one webhook message and delivers it (or its
// callback) to the service. Self-echo, duplicates, and unconsumable types
// stop here.
func (a *Adapter) processMessage(ctx context.Context, msg *webhookMessage, contacts map[string]string) {
	// Self-echo guard: messages sent by this business phone number come
	// back through the webhook — never re-ingest (loop guard, the
	// telegram FromBot analogue; the Cloud API marks them from our number).
	if msg.From == a.creds.PhoneNumberID {
		return
	}
	if msg.ID == "" {
		// Without a platform message id there is nothing to dedup against
		// and nothing to mark read — drop loudly rather than process twice.
		slog.Warn("whatsapp cloud webhook: message without id dropped", "gateway_id", a.gatewayID, "from", msg.From)
		return
	}
	// Webhook retries redeliver whole payloads: dedup on the platform
	// message id (the telegram update_id ring's analogue).
	if !a.rememberMessage(msg.ID) {
		return
	}

	// Track the chat's most recent inbound id for the mark-read + typing
	// call (SendTyping's anchor) — messages and button replies both anchor
	// it: a button press resumes a turn whose stream will heartbeat typing
	// on this chat.
	a.mu.Lock()
	a.lastInbound[msg.From] = msg.ID
	a.mu.Unlock()

	// Interactive button replies ride the Callback path with the data
	// VERBATIM: the id is the bridge's EncodeApprovalCallback output — the
	// bridge decodes and validates, the adapter never parses or re-encodes
	// it (add-whatsapp-gateway design D3).
	if msg.Type == "interactive" && msg.Interactive != nil && msg.Interactive.Type == "button_reply" && msg.Interactive.ButtonReply != nil {
		cb := gateways.Callback{
			Platform:   gateways.PlatformWhatsApp,
			ChatID:     msg.From,
			FromUserID: msg.From,
			Data:       msg.Interactive.ButtonReply.ID,
		}
		if msg.Context != nil {
			cb.MessageID = msg.Context.ID // the card the button lives on
		}
		a.handler.HandleCallback(ctx, a.gatewayID, cb)
		return
	}

	inbound, ok := a.normalizeMessage(ctx, msg, contacts)
	if !ok {
		return // refused or unconsumable — normalizeMessage already handled it
	}
	a.handler.HandleMessage(ctx, a.gatewayID, inbound)
}

// normalizeMessage maps one webhook message to the gateway's InboundMessage
// (wa ids are bare digits already, so ChatID = FromUserID = the wa id, and
// DM is the only kind — groups do not exist on this lane, design D7). The
// ok return is false for refused messages (sticker: a notice went out) and
// unconsumable ones.
func (a *Adapter) normalizeMessage(ctx context.Context, msg *webhookMessage, contacts map[string]string) (gateways.InboundMessage, bool) {
	inbound := gateways.InboundMessage{
		Platform:     gateways.PlatformWhatsApp,
		ChatID:       msg.From,
		Kind:         gateways.InboundDM,
		MessageID:    msg.ID,
		FromUserID:   msg.From,
		FromUsername: contacts[msg.From],
	}

	// Media-type dispatch (add-whatsapp-gateway design D11: video rides the
	// document lane, audio keeps its voice capability, stickers are refused
	// with a notice).
	switch msg.Type {
	case "text":
		if msg.Text == nil {
			return gateways.InboundMessage{}, false
		}
		inbound.Text = strings.TrimSpace(msg.Text.Body)
	case "image":
		if msg.Image == nil || msg.Image.ID == "" {
			return gateways.InboundMessage{}, false
		}
		inbound.Text = strings.TrimSpace(msg.Image.Caption)
		inbound.Attachments = append(inbound.Attachments, gateways.InboundAttachment{
			Kind:     gateways.AttachmentPhoto,
			FileID:   msg.Image.ID,
			MimeType: msg.Image.MimeType,
			Size:     msg.Image.FileSize,
		})
	case "video":
		// Video → document kind (design D11): the attachment pipeline has no
		// video lane; the bytes still reach the turn as a file.
		if msg.Video == nil || msg.Video.ID == "" {
			return gateways.InboundMessage{}, false
		}
		inbound.Text = strings.TrimSpace(msg.Video.Caption)
		inbound.Attachments = append(inbound.Attachments, gateways.InboundAttachment{
			Kind:     gateways.AttachmentDocument,
			FileID:   msg.Video.ID,
			MimeType: msg.Video.MimeType,
			Size:     msg.Video.FileSize,
		})
	case "document":
		if msg.Document == nil || msg.Document.ID == "" {
			return gateways.InboundMessage{}, false
		}
		inbound.Text = strings.TrimSpace(msg.Document.Caption)
		inbound.Attachments = append(inbound.Attachments, gateways.InboundAttachment{
			Kind:     gateways.AttachmentDocument,
			FileID:   msg.Document.ID,
			FileName: msg.Document.Filename,
			MimeType: msg.Document.MimeType,
			Size:     msg.Document.FileSize,
		})
	case "audio":
		if msg.Audio == nil || msg.Audio.ID == "" {
			return gateways.InboundMessage{}, false
		}
		kind := gateways.AttachmentDocument
		if msg.Audio.Voice {
			kind = gateways.AttachmentVoice // voice-capable: the transcriber lane
		}
		inbound.Attachments = append(inbound.Attachments, gateways.InboundAttachment{
			Kind:     kind,
			FileID:   msg.Audio.ID,
			MimeType: msg.Audio.MimeType,
			Size:     msg.Audio.FileSize,
		})
	case "sticker":
		// Refused with a notice (design D11) — the chat must know why
		// nothing happened. Best-effort: a failed notice is logged, the
		// sticker still never ingests.
		a.refuseSticker(ctx, msg.From)
		return gateways.InboundMessage{}, false
	default:
		// Unknown or unconsumed type (location, contacts, order, template
		// quick replies this gateway never minted, ...): dropped silently —
		// zero uncaught parse errors.
		return gateways.InboundMessage{}, false
	}

	return inbound, true
}

// refuseSticker posts the sticker refusal notice (best-effort).
func (a *Adapter) refuseSticker(ctx context.Context, chatID string) {
	if _, err := a.SendMessage(ctx, chatID, stickerRefusalCopy, "", gateways.SendOptions{}); err != nil {
		slog.Warn("whatsapp cloud: sticker refusal notice failed", "chat_id", chatID, "err", err)
	}
}

// rememberMessage reports whether the platform message id is new (inbound
// dedup ring, mirroring the telegram update-id ring).
func (a *Adapter) rememberMessage(id string) bool {
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	if _, dup := a.seen[id]; dup {
		return false
	}
	a.seen[id] = struct{}{}
	a.seenOrder = append(a.seenOrder, id)
	if len(a.seenOrder) > a.seenMax {
		oldest := a.seenOrder[0]
		a.seenOrder = a.seenOrder[1:]
		delete(a.seen, oldest)
	}
	return true
}
