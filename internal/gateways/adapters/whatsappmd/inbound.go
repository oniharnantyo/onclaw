package whatsappmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/gateways"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// stickerNoticeText is the chat notice for a refused sticker (design D11:
// stickers refused with a notice). Plain copy, no markup.
const stickerNoticeText = "Stickers aren't supported here yet — please send the image or file directly."

// approvalCardCopy closes the plain approval card (design D3: the md lane
// decides approvals by replying APPROVE or DENY — the router intercepts the
// reply; buttons are dead on this lane, design Context).
const approvalCardCopy = "\n\nReply APPROVE to approve or DENY to deny."

// onMessage normalizes one inbound whatsmeow message and hands it to the
// gateway service. Guards, in order: own echo (whatsmeow reflects our sends
// back — the FromBot loop guard), non-DM chats (groups/broadcasts/newsletters
// — DM-only v1), inbound edits, and the dedup ring.
func (a *Adapter) onMessage(e *events.Message) {
	if e == nil || e.Message == nil {
		return
	}
	if e.Info.IsFromMe {
		return
	}
	if e.Info.IsGroup || !isUserDM(e.Info.Chat) {
		return
	}
	if e.IsEdit {
		// Inbound edits are out of scope: the transcript's turn is already
		// submitted and the runner serializes turns per session.
		return
	}
	if !a.rememberMessage(string(e.Info.ID)) {
		return
	}

	inbound, sticker := a.normalizeMessage(e)
	if sticker {
		a.refuseSticker(e.Info.Chat)
		return
	}
	if inbound.Text == "" && len(inbound.Attachments) == 0 {
		return
	}

	chatID := inbound.ChatID
	a.mu.Lock()
	a.chatJIDs[chatID] = e.Info.Chat.ToNonAD()
	a.mu.Unlock()
	a.lastInboundMu.Lock()
	a.lastInbound[chatID] = inboundRef{id: string(e.Info.ID), sender: e.Info.Sender.ToNonAD()}
	a.lastInboundMu.Unlock()

	a.handler.HandleMessage(a.deviceContext(), a.gatewayID, inbound)
}

// normalizeMessage maps one inbound message to the gateway's InboundMessage
// (design D7: identity is the bare phone digits of the JID localpart). The
// second return reports a sticker — refused with a notice (design D11), not
// ingested. Media mapping: image → photo, video → document kind (design
// D11), document → document, audio/voice note → voice with duration.
func (a *Adapter) normalizeMessage(e *events.Message) (gateways.InboundMessage, bool) {
	msg := e.Message
	inbound := gateways.InboundMessage{
		Platform:     gateways.PlatformWhatsApp,
		Kind:         gateways.InboundDM,
		ChatID:       jidDigits(e.Info.Chat.User),
		MessageID:    string(e.Info.ID),
		FromUserID:   jidDigits(e.Info.Sender.User),
		FromUsername: e.Info.PushName,
	}

	if msg.GetStickerMessage() != nil {
		return inbound, true
	}

	inbound.Text = strings.TrimSpace(messageText(msg))
	if att, ok := mediaAttachment(msg); ok {
		inbound.Attachments = append(inbound.Attachments, att)
		if inbound.Text == "" {
			inbound.Text = strings.TrimSpace(mediaCaption(msg))
		}
	}
	return inbound, false
}

// isUserDM reports whether the chat is a direct user chat. User JIDs live on
// s.whatsapp.net (phone-addressed) or lid (LID-addressed); groups (@g.us),
// broadcasts, newsletters, status and everything else are excluded — the
// gateway is DM-only on this lane in v1 (design: groups deferred).
func isUserDM(chat types.JID) bool {
	if chat.IsEmpty() {
		return false
	}
	return chat.Server == types.DefaultUserServer || chat.Server == types.HiddenUserServer
}

// jidDigits extracts the normalized user id from a JID localpart (design
// D7): user JIDs are <digits>@s.whatsapp.net — the localpart is the id, and
// any non-digit (a stray '+', separators) is stripped so it cannot fork
// identities.
func jidDigits(localpart string) string {
	var b strings.Builder
	for i := 0; i < len(localpart); i++ {
		c := localpart[i]
		if c >= '0' && c <= '9' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// messageText extracts the plain text body of an inbound message.
func messageText(msg *waE2E.Message) string {
	if text := msg.GetConversation(); text != "" {
		return text
	}
	return msg.GetExtendedTextMessage().GetText()
}

// mediaCaption returns the caption-bearing media's caption (photo, video,
// document), empty otherwise.
func mediaCaption(msg *waE2E.Message) string {
	switch {
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetCaption()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetCaption()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetCaption()
	default:
		return ""
	}
}

// mediaAttachment maps the message's media into one InboundAttachment. The
// FileID is the self-contained serialized media message (media key + CDN
// path), base64 of the protobuf — DownloadFile decodes it back, so the
// platform reference survives the async ingest hop. Videos land as documents
// (design D11).
func mediaAttachment(msg *waE2E.Message) (gateways.InboundAttachment, bool) {
	switch {
	case msg.GetImageMessage() != nil:
		m := msg.GetImageMessage()
		return gateways.InboundAttachment{
			Kind:     gateways.AttachmentPhoto,
			FileID:   mediaRef(msg),
			MimeType: m.GetMimetype(),
			Size:     int64(m.GetFileLength()),
		}, true
	case msg.GetVideoMessage() != nil:
		m := msg.GetVideoMessage()
		mime := m.GetMimetype()
		if mime == "" {
			mime = "video/mp4"
		}
		// The video proto carries no file name; the ingest lane only needs
		// an extension-bearing name for its media-type decision.
		return gateways.InboundAttachment{
			Kind:     gateways.AttachmentDocument,
			FileID:   mediaRef(msg),
			FileName: "video.mp4",
			MimeType: mime,
			Size:     int64(m.GetFileLength()),
		}, true
	case msg.GetDocumentMessage() != nil:
		m := msg.GetDocumentMessage()
		name := m.GetFileName()
		if name == "" {
			name = m.GetTitle()
		}
		if name == "" {
			name = "document"
		}
		return gateways.InboundAttachment{
			Kind:     gateways.AttachmentDocument,
			FileID:   mediaRef(msg),
			FileName: name,
			MimeType: m.GetMimetype(),
			Size:     int64(m.GetFileLength()),
		}, true
	case msg.GetAudioMessage() != nil:
		m := msg.GetAudioMessage()
		mime := m.GetMimetype()
		if mime == "" {
			mime = "audio/ogg"
		}
		return gateways.InboundAttachment{
			Kind:     gateways.AttachmentVoice,
			FileID:   mediaRef(msg),
			MimeType: mime,
			Size:     int64(m.GetFileLength()),
			Duration: int(m.GetSeconds()),
		}, true
	default:
		return gateways.InboundAttachment{}, false
	}
}

// mediaRef serializes the media-bearing message as the opaque platform file
// reference. A marshal failure yields "" — DownloadFile refuses it with a
// clear error instead of a corrupt blob.
func mediaRef(msg *waE2E.Message) string {
	raw, err := proto.Marshal(msg)
	if err != nil {
		slog.Warn("whatsappmd adapter: media reference encode failed", "err", err)
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// refuseSticker posts the sticker-refusal notice (best-effort — a failed
// notice is logged, never fatal; the sticker is dropped either way).
func (a *Adapter) refuseSticker(chat types.JID) {
	if _, err := a.device.SendMessage(a.deviceContext(), chat.ToNonAD(),
		&waE2E.Message{Conversation: proto.String(stickerNoticeText)}); err != nil {
		slog.Warn("whatsappmd adapter: sticker refusal notice failed",
			"gateway_id", a.gatewayID, "err", err)
	}
}

// rememberMessage reports whether the message id is new (inbound dedup
// ring — reconnects can replay the delivery tail).
func (a *Adapter) rememberMessage(id string) bool {
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	if id == "" {
		return true
	}
	if _, dup := a.seen[id]; dup {
		return false
	}
	a.seen[id] = struct{}{}
	a.seenOrder = append(a.seenOrder, id)
	if len(a.seenOrder) > dedupRingSize {
		oldest := a.seenOrder[0]
		a.seenOrder = a.seenOrder[1:]
		delete(a.seen, oldest)
	}
	return true
}

// jidFor resolves a normalized chat id (bare digits) to a send target: the
// remembered chat JID when this process saw the chat (preserving the phone
// vs LID server the traffic arrived on), the phone-number JID otherwise.
func (a *Adapter) jidFor(chatID string) types.JID {
	a.mu.Lock()
	jid, ok := a.chatJIDs[chatID]
	a.mu.Unlock()
	if ok {
		return jid
	}
	return types.NewJID(chatID, types.DefaultUserServer)
}

// flavorMatches reports whether body is rendered in the format this adapter
// speaks (design D9). An empty flavor means the core's format-agnostic copy
// paths and is accepted as this adapter's own WhatsApp wire format.
func flavorMatches(flavor string) bool {
	return flavor == "" || flavor == gateways.FlavorWhatsAppMD
}

// SendMessage implements PlatformAdapter: post a WhatsApp markdown message
// (design D9 — the body arrives pre-rendered in the WhatsApp subset; the
// 4096-char budget is enforced by the core's splitter, not here). A body
// rendered in another platform's flavor is refused, never misparsed.
// DisablePreview is ignored: WhatsApp has no preview toggle (design D9).
func (a *Adapter) SendMessage(ctx context.Context, chatID, body, flavor string, opts gateways.SendOptions) (string, error) {
	if !flavorMatches(flavor) {
		return "", fmt.Errorf("whatsappmd adapter: cannot deliver %q body", flavor)
	}
	jid := a.jidFor(chatID)
	resp, err := a.device.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(body)})
	if err != nil {
		return "", fmt.Errorf("whatsappmd adapter: send: %w", err)
	}
	// The turn is delivered — end the composing indicator the typing
	// heartbeat opened (best-effort).
	a.stopTyping(context.WithoutCancel(ctx), jid)
	return string(resp.ID), nil
}

// EditMessage implements PlatformAdapter: rewrite a sent message in place
// (whatsmeow message edit — design D2 CanEdit=true). WhatsApp caps edits at
// 20 minutes after the original send; a failed edit propagates as an error —
// the approval bridge logs it best-effort rather than pretending the card
// was updated. Body/flavor follow SendMessage's contract.
func (a *Adapter) EditMessage(ctx context.Context, chatID, messageID, body, flavor string) error {
	if !flavorMatches(flavor) {
		return fmt.Errorf("whatsappmd adapter: cannot deliver %q body", flavor)
	}
	if messageID == "" {
		return fmt.Errorf("whatsappmd adapter: edit: %w: message id is empty", ErrInvalid)
	}
	jid := a.jidFor(chatID)
	edit := a.device.BuildEdit(jid, types.MessageID(messageID), &waE2E.Message{Conversation: proto.String(body)})
	if _, err := a.device.SendMessage(ctx, jid, edit); err != nil {
		return fmt.Errorf("whatsappmd adapter: edit: %w", err)
	}
	return nil
}

// SendTyping implements PlatformAdapter: mark the last inbound message read
// and start composing (chat-presence typing — design Context: typing works
// on this lane and is tied to the mark-read). Best-effort: failures are
// logged, never fatal — the streamer's heartbeat drives repeated calls.
func (a *Adapter) SendTyping(ctx context.Context, chatID string) error {
	jid := a.jidFor(chatID)
	a.lastInboundMu.Lock()
	ref, ok := a.lastInbound[chatID]
	a.lastInboundMu.Unlock()
	if ok && ref.id != "" {
		if err := a.device.MarkRead(ctx, []types.MessageID{types.MessageID(ref.id)}, time.Now(), jid, ref.sender); err != nil {
			slog.Warn("whatsappmd adapter: mark read failed",
				"gateway_id", a.gatewayID, "chat", chatID, "err", err)
		}
	}
	if err := a.device.SendChatPresence(ctx, jid, types.ChatPresenceComposing, types.ChatPresenceMediaText); err != nil {
		slog.Warn("whatsappmd adapter: typing indicator failed",
			"gateway_id", a.gatewayID, "chat", chatID, "err", err)
	}
	return nil
}

// stopTyping ends the composing indicator (chat presence paused).
func (a *Adapter) stopTyping(ctx context.Context, jid types.JID) {
	if err := a.device.SendChatPresence(ctx, jid, types.ChatPresencePaused, types.ChatPresenceMediaText); err != nil {
		slog.Warn("whatsappmd adapter: stop typing failed", "err", err)
	}
}

// SendApprovalCard implements PlatformAdapter: post the plain instructional
// card for one pending interrupt (add-whatsapp-gateway design D3). The card
// is a plain message instructing the member to reply APPROVE or DENY — no
// button attempt of any kind (native buttons are dead on this lane, design
// Context); the router intercepts that reply into the approval bridge.
func (a *Adapter) SendApprovalCard(ctx context.Context, chatID string, interrupt agents.ApprovalPayload) (string, error) {
	var b strings.Builder
	b.WriteString("Shell approval requested")
	if interrupt.Command != "" {
		b.WriteString(":\n`" + interrupt.Command + "`")
	}
	b.WriteString(approvalCardCopy)

	jid := a.jidFor(chatID)
	resp, err := a.device.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(b.String())})
	if err != nil {
		return "", fmt.Errorf("whatsappmd adapter: approval card: %w", err)
	}
	return string(resp.ID), nil
}

// DownloadFile implements PlatformAdapter: decode the serialized media
// reference and fetch the bytes through whatsmeow (media key + CDN path are
// self-contained in the proto). An undecodable reference is refused — the
// ingest stage renders it as a chat refusal, never a gateway failure.
func (a *Adapter) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	if fileID == "" {
		return nil, fmt.Errorf("whatsappmd adapter: download: %w: empty file reference", ErrInvalid)
	}
	raw, err := base64.StdEncoding.DecodeString(fileID)
	if err != nil {
		return nil, fmt.Errorf("whatsappmd adapter: download: decode media reference: %w", err)
	}
	var msg waE2E.Message
	if err := proto.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("whatsappmd adapter: download: parse media reference: %w", err)
	}
	data, err := a.device.DownloadAny(ctx, &msg)
	if err != nil {
		return nil, fmt.Errorf("whatsappmd adapter: download: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("whatsappmd adapter: download: empty media body")
	}
	return data, nil
}
