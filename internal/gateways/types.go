// Package gateways implements platform gateways that bridge external chat
// surfaces into OnClaw agent sessions. The core in this package is
// platform-neutral — routing, pairing, streaming, approvals, delivery
// reliability, and guardrails — while each platform ships an adapter under
// adapters/ that implements PlatformAdapter. v1 registers only Telegram;
// agent sessions are the only destination (never channels or work sessions).
package gateways

import (
	"context"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents"
)

// Platform identifies a gateway platform. Session bindings and user links
// are keyed on it.
const PlatformTelegram = "telegram"

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

// SendOptions carries per-send presentation flags.
type SendOptions struct {
	// DisablePreview suppresses link previews.
	DisablePreview bool
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
	// SendMessage posts a new chat message rendered as Telegram-compatible
	// HTML and returns the platform message id. Implementations apply the
	// plain-text fallback and rate-limit backoff themselves.
	SendMessage(ctx context.Context, chatID, html string, opts SendOptions) (string, error)
	// EditMessage rewrites a previously sent message in place.
	EditMessage(ctx context.Context, chatID, messageID, html string) error
	// SendTyping flashes the chat action indicator (best-effort).
	SendTyping(ctx context.Context, chatID string) error
	// SendApprovalCard posts the approve/deny inline keyboard for one
	// pending interrupt and returns the card message id.
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

// RenderedPart is one final outbound chunk: HTML body plus the send
// timestamp the outbox records.
type RenderedPart struct {
	HTML string
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
