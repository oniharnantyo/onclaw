// Package gateways implements platform gateways that bridge external chat
// surfaces into OnClaw agent sessions. The core in this package is
// platform-neutral — routing, pairing, streaming, approvals, delivery
// reliability, and guardrails — while each platform ships an adapter under
// adapters/ that implements PlatformAdapter. v1 registers only Telegram;
// agent sessions are the only destination (never channels or work sessions).
package gateways

import (
	"context"
	"errors"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
)

// Platform identifies a gateway platform. Session bindings and user links
// are keyed on it.
const (
	PlatformTelegram = "telegram"
	PlatformWhatsApp = "whatsapp"
)

// InboundKind classifies the chat surface an inbound message arrived on.
type InboundKind string

const (
	// InboundDM is a direct message between a user and the bot.
	InboundDM InboundKind = "dm"
	// InboundGroup is a group chat the bot is bound to exactly one agent in.
	InboundGroup InboundKind = "group"
)

// AttachmentKind classifies an inbound file for the attachment pipeline.
type AttachmentKind string

const (
	AttachmentPhoto    AttachmentKind = "photo"
	AttachmentDocument AttachmentKind = "document"
	AttachmentVoice    AttachmentKind = "voice"
)

// InboundMessage is one normalized platform message handed to the gateway
// service. Platform ids are opaque strings; identity resolution (platform
// user → paired member) happens downstream of the adapter.
type InboundMessage struct {
	Platform string
	// ChatID is the platform chat the message arrived in (user id for DMs).
	ChatID string
	Kind   InboundKind
	// MessageID is the platform message id, used for edits and dedup.
	MessageID string
	// FromUserID / FromUsername identify the sender on the platform.
	FromUserID   string
	FromUsername string
	// Text is the caption-inclusive text body; empty for media-only.
	Text string
	// Attachments carries the message's files, if any.
	Attachments []InboundAttachment
	// MigrateToChatID is non-empty when the platform reports the chat was
	// migrated (migrate_to_chat_id): the binding must be remapped and the
	// message itself dropped.
	MigrateToChatID string
	// FromBot marks bot-originated messages for the loop guard.
	FromBot bool
}

// InboundAttachment is one file reference on an inbound message. The file
// body is fetched through PlatformAdapter.DownloadFile on demand.
type InboundAttachment struct {
	Kind     AttachmentKind
	FileID   string
	FileName string
	MimeType string
	Size     int64
	// Duration is the media duration in seconds (voice notes).
	Duration int
}

// Callback is one inline-keyboard button press routed back to the service.
type Callback struct {
	Platform string
	ChatID   string
	// MessageID is the card message the button lives on.
	MessageID string
	// FromUserID is the pressing user (validated against pairing).
	FromUserID string
	// Data is the opaque callback payload minted with the card.
	Data string
}

// Render flavor tags: the wire format a body was rendered in. They ride
// outbox payloads and send calls so a delivery to the wrong platform's
// adapter is refused instead of misparsed (add-whatsapp-gateway design D9).
const (
	FlavorTelegramHTML = "telegram_html"
	FlavorWhatsAppMD   = "whatsapp_md"
)

// ErrPermanentDelivery marks a send failure that can never succeed on retry —
// the WhatsApp Cloud API's 24-hour window expiry and 4xx family (design D4).
// The outbox dead-letters the entry when its sender's error chain carries
// this sentinel (errors.Is) instead of rescheduling. Platform adapters own
// the classification: an adapter wraps this core sentinel so the dependency
// direction stays adapter→core (the whatsappcloud package re-exports it).
var ErrPermanentDelivery = errors.New("permanent delivery failure")

// SendOptions carries per-send presentation flags.
type SendOptions struct {
	// DisablePreview suppresses link previews (platforms without a preview
	// toggle ignore it — add-whatsapp-gateway design D9).
	DisablePreview bool
}

// AdapterCapabilities reports the platform features an adapter honestly
// implements (add-whatsapp-gateway design D2). Callers adapt instead of
// assuming Telegram's abilities: the streamer skips the placeholder/edit
// stream when CanEdit is false, and the approval bridge resolves cards with
// a receipt follow-up instead of an edit. Typing is universal — every
// adapter implements SendTyping or no-ops — so it has no capability flag.
type AdapterCapabilities struct {
	// CanEdit reports whether sent messages can be rewritten in place.
	CanEdit bool
	// CanButton reports whether approval cards can carry interactive buttons
	// (button ids are EncodeApprovalCallback verbatim). When false, the
	// platform decides approvals by text reply — the card must instruct
	// replying APPROVE or DENY, and the router intercepts that reply.
	CanButton bool
}

// PlatformAdapter is the narrow seam each platform implements: lifecycle,
// outbound primitives, and file download. Implementations translate these
// to platform APIs and deliver inbound messages/callbacks to the service
// through the InboundHandler registered at construction.
type PlatformAdapter interface {
	// Start begins ingestion (long polling or webhook receiver) and returns
	// after the transport is live; Stop tears it down. Both must be safe to
	// call once per adapter instance.
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	// Capabilities reports the platform's feature surface (design D2).
	Capabilities() AdapterCapabilities
	// SendMessage posts a new chat message and returns the platform message
	// id. body is rendered in flavor (one of the Flavor* tags); a flavor the
	// adapter does not speak must be refused with an error, never misparsed —
	// an empty flavor means the adapter's own format (the core's
	// format-agnostic copy paths). Implementations apply the plain-text
	// fallback and rate-limit backoff themselves.
	SendMessage(ctx context.Context, chatID, body, flavor string, opts SendOptions) (string, error)
	// EditMessage rewrites a previously sent message in place. Only honest
	// when Capabilities().CanEdit; body/flavor follow SendMessage's contract.
	EditMessage(ctx context.Context, chatID, messageID, body, flavor string) error
	// SendTyping flashes the chat action indicator (best-effort).
	SendTyping(ctx context.Context, chatID string) error
	// SendApprovalCard posts the approve/deny card for one pending interrupt
	// and returns the card message id. The card carries interactive buttons
	// (callback_data = EncodeApprovalCallback verbatim) only when
	// Capabilities().CanButton; otherwise its body instructs replying
	// APPROVE or DENY in the chat (add-whatsapp-gateway design D3).
	SendApprovalCard(ctx context.Context, chatID string, interrupt agents.ApprovalPayload) (string, error)
	// DownloadFile fetches a file by its platform reference.
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
}

// InboundHandler is the service-side receiver adapters call with normalized
// platform traffic. Implementations must not block platform ingestion
// loops: handlers that need to wait (busy queue, run drain) dispatch to
// their own goroutines.
type InboundHandler interface {
	HandleMessage(ctx context.Context, gatewayID string, msg InboundMessage)
	HandleCallback(ctx context.Context, gatewayID string, cb Callback)
}

// RunSubmitter is the runner surface the gateway needs, narrowed so the
// gateway is testable without a full Runner. Busy sessions reject with
// domain.ErrConflict — the queue's trigger.
type RunSubmitter interface {
	Run(ctx context.Context, req agents.ExecRequest) (*agents.EventStream, error)
	Resume(ctx context.Context, req agents.ExecRequest, approval agents.ApprovalPayload, approved bool) (*agents.EventStream, error)
}

// RenderedPart is one final outbound chunk: a body rendered in its platform
// flavor plus the send timestamp the outbox records.
type RenderedPart struct {
	Body   string
	Flavor string
}

// OutboxRecord is the delivery unit the outbox persists before sending.
// Payload is the adapter-specific send request, JSON-encoded.
type OutboxRecord struct {
	ID           string
	GatewayID    string
	WorkspaceID  string
	SessionID    string
	ChatID       string
	Payload      []byte
	Status       string
	Attempts     int
	DeliverAfter time.Time
	CreatedAt    time.Time
}
