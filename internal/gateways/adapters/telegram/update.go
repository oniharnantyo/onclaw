package telegram

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/gateways"
)

// Raw Bot API update shapes — only the fields the gateway normalizes.
// Unknown fields are ignored by encoding/json, so a Bot API addition can
// never break ingestion (zero uncaught parse errors).

type apiUpdate struct {
	UpdateID      int64             `json:"update_id"`
	Message       *apiMessage       `json:"message"`
	CallbackQuery *apiCallbackQuery `json:"callback_query"`
	// edited_message, channel_post, my_chat_member and friends are
	// deliberately not consumed: edits re-render through the stream
	// controller's own message ids, and channel posts are out of scope
	// (the gateway never touches channels).
}

type apiUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type apiChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"` // "private" | "group" | "supergroup"
}

type apiPhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int64  `json:"file_size"`
}

type apiDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type apiVoice struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
	Duration int    `json:"duration"`
}

type apiMessage struct {
	MessageID int64          `json:"message_id"`
	From      *apiUser       `json:"from"`
	Chat      apiChat        `json:"chat"`
	Text      string         `json:"text"`
	Caption   string         `json:"caption"`
	Photo     []apiPhotoSize `json:"photo"`
	Document  *apiDocument   `json:"document"`
	Voice     *apiVoice      `json:"voice"`
	// migrate_to_chat_id rides a service message when a group upgrades to a
	// supergroup (design D10): the binding must be remapped, the message
	// itself dropped.
	MigrateToChatID int64       `json:"migrate_to_chat_id"`
	ReplyToMessage  *apiMessage `json:"reply_to_message"`
}

type apiCallbackQuery struct {
	ID      string      `json:"id"`
	From    apiUser     `json:"from"`
	Message *apiMessage `json:"message"`
	Data    string      `json:"data"`
}

// gatewayCommands is the set of gateway-owned commands accepted in groups
// when sent bare (no @botname suffix): everything else unaddressed in a
// group is another bot's audience, not ours.
var gatewayCommands = map[string]struct{}{
	"start":   {},
	"new":     {},
	"compact": {},
	"usage":   {},
	"agent":   {},
}

// ParseUpdate decodes one raw update payload. Malformed JSON returns a nil
// update — callers drop it silently (zero uncaught parse errors).
func ParseUpdate(raw []byte) *apiUpdate {
	var upd apiUpdate
	if err := json.Unmarshal(raw, &upd); err != nil {
		slog.Warn("telegram webhook: undecodable update dropped", "err", err)
		return nil
	}
	return &upd
}

// normalize converts one raw update into gateway traffic. The second return
// reports whether the update carries anything the gateway consumes
// (messages and callback queries only).
func (a *Adapter) normalize(upd *apiUpdate) ([]gateways.InboundMessage, *gateways.Callback) {
	if upd == nil {
		return nil, nil
	}

	if msg := upd.Message; msg != nil && msg.Chat.Type != "" {
		botID, botUsername := a.identity()
		// Our own messages echoed back: never re-ingest (self-loop).
		if msg.From != nil && msg.From.ID == botID {
			return nil, nil
		}
		if !a.targeted(msg, botUsername) {
			// Privacy-mode defense-in-depth: untargeted group chatter never
			// reaches ingestion (Telegram already filters with privacy ON).
			return nil, nil
		}
		inbound := a.normalizeMessage(msg)
		return []gateways.InboundMessage{inbound}, nil
	}

	if cq := upd.CallbackQuery; cq != nil {
		cb := gateways.Callback{
			Platform:   gateways.PlatformTelegram,
			FromUserID: strconv.FormatInt(cq.From.ID, 10),
			Data:       cq.Data,
		}
		if cq.Message != nil {
			cb.ChatID = strconv.FormatInt(cq.Message.Chat.ID, 10)
			cb.MessageID = strconv.FormatInt(cq.Message.MessageID, 10)
		}
		return nil, &cb
	}

	return nil, nil
}

// normalizeMessage maps one raw message to the gateway's InboundMessage.
func (a *Adapter) normalizeMessage(msg *apiMessage) gateways.InboundMessage {
	kind := gateways.InboundDM
	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	if msg.Chat.Type != "private" {
		kind = gateways.InboundGroup
	}

	inbound := gateways.InboundMessage{
		Platform: gateways.PlatformTelegram,
		ChatID:   chatID,
		Kind:     kind,
		// Caption-inclusive text (spec: photos arrive with caption questions).
		Text:            strings.TrimSpace(msg.Text),
		FromBot:         msg.From != nil && msg.From.IsBot,
		MessageID:       strconv.FormatInt(msg.MessageID, 10),
		MigrateToChatID: strconv.FormatInt(msg.MigrateToChatID, 10),
	}
	if msg.From != nil {
		inbound.FromUserID = strconv.FormatInt(msg.From.ID, 10)
		inbound.FromUsername = msg.From.Username
	}

	// Media attachments (normalized; the ingress stage classifies lanes —
	// design D8).
	if len(msg.Photo) > 0 {
		// Telegram sends an array of sizes; the last is the largest.
		largest := msg.Photo[len(msg.Photo)-1]
		inbound.Attachments = append(inbound.Attachments, gateways.InboundAttachment{
			Kind:     gateways.AttachmentPhoto,
			FileID:   largest.FileID,
			MimeType: "image/jpeg",
			Size:     largest.FileSize,
		})
	}
	if msg.Document != nil {
		inbound.Attachments = append(inbound.Attachments, gateways.InboundAttachment{
			Kind:     gateways.AttachmentDocument,
			FileID:   msg.Document.FileID,
			FileName: msg.Document.FileName,
			MimeType: msg.Document.MimeType,
			Size:     msg.Document.FileSize,
		})
	}
	if msg.Voice != nil {
		inbound.Attachments = append(inbound.Attachments, gateways.InboundAttachment{
			Kind:     gateways.AttachmentVoice,
			FileID:   msg.Voice.FileID,
			MimeType: msg.Voice.MimeType,
			Size:     msg.Voice.FileSize,
			Duration: msg.Voice.Duration,
		})
	}

	return inbound
}

// targeted reports whether a group message addresses the bot: a command for
// it, an @mention, or a reply to one of its messages. Private chats are
// always targeted. Design D2/spec: only commands, bot replies, and bot
// mentions reach ingestion.
func (a *Adapter) targeted(msg *apiMessage, botUsername string) bool {
	if msg.Chat.Type == "private" {
		return true
	}

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}

	// Reply to one of the bot's messages.
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.ID == a.botID {
		return true
	}

	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "/") {
		fields := strings.Fields(trimmed)
		head := strings.TrimPrefix(fields[0], "/")
		if at := strings.IndexByte(head, '@'); at >= 0 {
			// Addressed command: only ours.
			return strings.EqualFold(head[at+1:], botUsername)
		}
		// Bare command: accepted only when the gateway itself owns it —
		// anything else belongs to another bot (or an agent slash command,
		// which groups invoke addressed).
		_, ours := gatewayCommands[strings.ToLower(head)]
		return ours
	}

	if botUsername != "" && strings.Contains(text, "@"+botUsername) {
		return true
	}
	return false
}

// IngestWebhook implements the gateway.WebhookReceiver capability: one raw
// update payload from the public ingress handler (after secret-token
// validation). Malformed payloads are dropped inside ParseUpdate, so this
// never fails on body content.
func (a *Adapter) IngestWebhook(ctx context.Context, raw []byte) error {
	upd := ParseUpdate(raw)
	if upd == nil {
		return nil
	}
	// Webhook retries can redeliver: dedup on update_id.
	if !a.rememberUpdate(upd.UpdateID) {
		return nil
	}
	a.processUpdate(ctx, upd)
	return nil
}

// processUpdate fans one raw update out to the gateway service.
func (a *Adapter) processUpdate(ctx context.Context, upd *apiUpdate) {
	msgs, cb := a.normalize(upd)
	for _, msg := range msgs {
		// migrate_to_chat_id (design D10): the service remaps the binding and
		// drops the message itself.
		a.handler.HandleMessage(ctx, a.gatewayID, msg)
	}
	if cb != nil {
		a.handler.HandleCallback(ctx, a.gatewayID, *cb)
	}
}
