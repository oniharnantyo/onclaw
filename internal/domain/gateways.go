package domain

import (
	"fmt"
	"strings"
	"time"
)

// Gateway platforms. v1 ships Telegram only; the field exists on every
// gateway row so the next adapter (Slack) is a data change, not a schema one
// (integrate-telegram-gateway design D1).
const GatewayPlatformTelegram = "telegram"

// Gateway transport modes: a public HTTPS webhook URL, or in-process long
// polling that works behind NAT (integrate-telegram-gateway design D12).
const (
	GatewayTransportWebhook     = "webhook"
	GatewayTransportLongPolling = "long_polling"
)

// Gateway outbox entry statuses (integrate-telegram-gateway design D9):
// pending rows are claimable for delivery, delivered rows await retention
// pruning, dead rows exhausted their attempt budget and are never retried.
const (
	GatewayOutboxStatusPending   = "pending"
	GatewayOutboxStatusDelivered = "delivered"
	GatewayOutboxStatusDead      = "dead"
)

// Gateway sentinels. The pairing-token sentinels wrap ErrInvalid and the
// binding-conflict sentinel wraps ErrConflict so generic envelope mapping
// (errors.Is against the base sentinels) keeps working while callers that
// care can distinguish the gateway-specific cause.
var (
	// ErrInvalidPairingToken reports a pairing token whose shape is not a
	// valid gateway pairing token (length or charset).
	ErrInvalidPairingToken = fmt.Errorf("%w: pairing token shape is invalid", ErrInvalid)

	// ErrPairingTokenExpired reports a pairing token that cannot be consumed
	// because it expired or was already consumed (single-use). Callers treat
	// both identically: the link is refused either way.
	ErrPairingTokenExpired = fmt.Errorf("%w: pairing token expired or already used", ErrInvalid)

	// ErrGatewayBindingConflict reports that the platform chat is already
	// bound (each platform chat has at most one binding, globally).
	ErrGatewayBindingConflict = fmt.Errorf("%w: platform chat is already bound to an agent", ErrConflict)
)

// gatewayPairingTokenMinLength / gatewayPairingTokenMaxLength bound the
// pairing-token shape: crypto-random, URL-safe-base64 characters only.
// 32 bytes of entropy base64url-encoded is 43 chars; the window leaves room
// for alternative encodings without admitting human-guessable tokens.
const (
	gatewayPairingTokenMinLength = 32
	gatewayPairingTokenMaxLength = 128
)

// GatewayConfig is one workspace's gateway configuration for a platform:
// the encrypted bot credential (an AES-256-GCM secrets envelope, AAD-bound
// to the workspace — the web-search provider credential pattern), the
// resolved bot username (display-only), the enable toggle, the transport
// mode, and the default agent for direct messages that have no per-user
// choice. Exactly one row per (workspace, platform). The plaintext token
// never persists and never leaves the write path.
type GatewayConfig struct {
	ID                 string    `json:"id"`
	WorkspaceID        string    `json:"workspace_id"`
	Platform           string    `json:"platform"`
	BotTokenCiphertext string    `json:"-"` // secrets v1 envelope; never serialized
	BotUsername        string    `json:"bot_username"`
	Enabled            bool      `json:"enabled"`
	Transport          string    `json:"transport"` // webhook | long_polling
	WebhookURL         string    `json:"webhook_url,omitempty"`
	DefaultAgentID     *string   `json:"default_agent_id,omitempty"` // nullable; ON DELETE SET NULL
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// UserLink binds one platform identity (keyed on the immutable numeric id,
// never the mutable username) to a workspace member. Pairing is
// default-deny: only linked identities reach the runner, and every run
// executes under the linked member's user id and RBAC (design D6).
type UserLink struct {
	Platform         string    `json:"platform"`
	PlatformUserID   string    `json:"platform_user_id"`
	WorkspaceID      string    `json:"workspace_id"`
	UserID           string    `json:"user_id"`
	PlatformUsername string    `json:"platform_username"` // display-only; never used for identity
	// DefaultAgentID is the member's per-user default agent choice for
	// gateway direct messages (design D3), set with /agent <name>. nil falls
	// back to the gateway's configured default agent. Nullable; ON DELETE
	// SET NULL.
	DefaultAgentID *string   `json:"default_agent_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// PairingToken is a single-use, crypto-random, expiring credential a member
// mints to pair their platform identity (design D6): sent to the bot as
// `/start <token>`, consumed on use, revocable, and dead after expiry.
type PairingToken struct {
	Token       string     `json:"token"`
	WorkspaceID string     `json:"workspace_id"`
	UserID      string     `json:"user_id"`
	ExpiresAt   time.Time  `json:"expires_at"`
	ConsumedAt  *time.Time `json:"consumed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ChatBinding routes one platform chat to exactly one workspace agent
// (design D2): agent-scoped, unique per (platform, platform_chat_id)
// globally — a group chat can only ever belong to one workspace's agent.
// No channel coordinates, no forum-topic column.
type ChatBinding struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Platform       string    `json:"platform"`
	PlatformChatID string    `json:"platform_chat_id"`
	AgentID        string    `json:"agent_id"`
	CreatedBy      *string   `json:"created_by,omitempty"` // nullable; ON DELETE SET NULL
	CreatedAt      time.Time `json:"created_at"`
}

// OutboxEntry is one durable outbound delivery record written before the
// send (design D9): a crash between generating a reply and delivering it is
// recovered by the startup redelivery sweep. Payload is opaque JSON owned
// by the gateway service (message coordinates + rendered content).
type OutboxEntry struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	SessionID    string    `json:"session_id"`
	Payload      []byte    `json:"payload"`
	Status       string    `json:"status"` // pending | delivered | dead
	Attempts     int       `json:"attempts"`
	DeliverAfter time.Time `json:"deliver_after"`
	CreatedAt    time.Time `json:"created_at"`
}

// ValidateGatewayConfig validates a gateway configuration for persistence.
// The struct is normalized in place: platform and transport are trimmed,
// long-polling clears the webhook URL. The bot token ciphertext must carry
// the AES-256-GCM envelope shape ("v1:<nonce>:<ciphertext>" — the secrets
// package's Version1Prefix, matched literally here because domain cannot
// import secrets without an import cycle). Uniqueness (one gateway per
// workspace+platform) is a store concern.
func ValidateGatewayConfig(g *GatewayConfig) error {
	if g == nil {
		return fmt.Errorf("%w: gateway config is required", ErrInvalid)
	}

	g.Platform = strings.TrimSpace(g.Platform)
	if g.Platform == "" {
		return fmt.Errorf("%w: gateway platform is required", ErrInvalid)
	}

	g.BotTokenCiphertext = strings.TrimSpace(g.BotTokenCiphertext)
	if g.BotTokenCiphertext == "" {
		return fmt.Errorf("%w: bot token ciphertext is required", ErrInvalid)
	}
	parts := strings.Split(g.BotTokenCiphertext, ":")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] == "" || parts[2] == "" {
		return fmt.Errorf("%w: bot token ciphertext must be a v1 secrets envelope", ErrInvalid)
	}

	g.BotUsername = strings.TrimSpace(g.BotUsername)

	switch g.Transport {
	case GatewayTransportWebhook:
		g.WebhookURL = strings.TrimSpace(g.WebhookURL)
		if !strings.HasPrefix(g.WebhookURL, "https://") {
			return fmt.Errorf("%w: webhook transport requires an https:// webhook_url", ErrInvalid)
		}
	case GatewayTransportLongPolling:
		g.WebhookURL = ""
	default:
		return fmt.Errorf("%w: gateway transport %q must be %q or %q", ErrInvalid, g.Transport, GatewayTransportWebhook, GatewayTransportLongPolling)
	}

	if g.DefaultAgentID != nil {
		*g.DefaultAgentID = strings.TrimSpace(*g.DefaultAgentID)
		if *g.DefaultAgentID == "" {
			g.DefaultAgentID = nil
		}
	}

	return nil
}

// ValidateUserLink validates an identity link for persistence: the immutable
// platform id, workspace, and member are required; the username is
// display-only and normalized to a trim. Uniqueness (PK platform +
// platform_user_id + workspace) is a store concern.
func ValidateUserLink(l *UserLink) error {
	if l == nil {
		return fmt.Errorf("%w: user link is required", ErrInvalid)
	}

	l.Platform = strings.TrimSpace(l.Platform)
	l.PlatformUserID = strings.TrimSpace(l.PlatformUserID)
	if l.Platform == "" || l.PlatformUserID == "" {
		return fmt.Errorf("%w: platform and platform_user_id are required", ErrInvalid)
	}
	if strings.TrimSpace(l.WorkspaceID) == "" {
		return fmt.Errorf("%w: workspace is required", ErrInvalid)
	}
	if strings.TrimSpace(l.UserID) == "" {
		return fmt.Errorf("%w: member user_id is required", ErrInvalid)
	}
	l.PlatformUsername = strings.TrimSpace(l.PlatformUsername)
	if l.DefaultAgentID != nil {
		*l.DefaultAgentID = strings.TrimSpace(*l.DefaultAgentID)
		if *l.DefaultAgentID == "" {
			l.DefaultAgentID = nil
		}
	}

	return nil
}

// ValidatePairingToken validates a pairing token for persistence: the token
// shape (crypto-random, URL-safe charset, bounded length), workspace, and
// member are required, and a minted token must expire strictly in the
// future relative to now. Expiry and single-use enforcement at consumption
// time are store concerns (ConsumePairingToken).
func ValidatePairingToken(t *PairingToken, now time.Time, creating bool) error {
	if t == nil {
		return fmt.Errorf("%w: pairing token is required", ErrInvalid)
	}

	if err := ValidatePairingTokenShape(t.Token); err != nil {
		return err
	}
	if strings.TrimSpace(t.WorkspaceID) == "" {
		return fmt.Errorf("%w: workspace is required", ErrInvalid)
	}
	if strings.TrimSpace(t.UserID) == "" {
		return fmt.Errorf("%w: member user_id is required", ErrInvalid)
	}
	if creating && !t.ExpiresAt.After(now) {
		return fmt.Errorf("%w: expires_at must be in the future", ErrInvalid)
	}

	return nil
}

// ValidatePairingTokenShape enforces the token's shape: 32–128 URL-safe
// base64 characters (43 chars for 32 bytes of entropy). Anything else —
// shorter, longer, or carrying whitespace, punctuation, or separator
// characters — is refused with ErrInvalidPairingToken.
func ValidatePairingTokenShape(token string) error {
	if len(token) < gatewayPairingTokenMinLength || len(token) > gatewayPairingTokenMaxLength {
		return fmt.Errorf("%w: token length must be %d–%d characters", ErrInvalidPairingToken, gatewayPairingTokenMinLength, gatewayPairingTokenMaxLength)
	}
	for _, r := range token {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("%w: token must contain only URL-safe base64 characters", ErrInvalidPairingToken)
		}
	}
	return nil
}

// ValidateChatBinding validates a chat binding for persistence: platform,
// immutable platform chat id, and target agent are required. Uniqueness
// (one binding per platform chat, globally) is a store concern and surfaces
// as ErrGatewayBindingConflict.
func ValidateChatBinding(b *ChatBinding) error {
	if b == nil {
		return fmt.Errorf("%w: chat binding is required", ErrInvalid)
	}

	b.Platform = strings.TrimSpace(b.Platform)
	b.PlatformChatID = strings.TrimSpace(b.PlatformChatID)
	b.AgentID = strings.TrimSpace(b.AgentID)
	if b.Platform == "" || b.PlatformChatID == "" {
		return fmt.Errorf("%w: platform and platform_chat_id are required", ErrInvalid)
	}
	if b.AgentID == "" {
		return fmt.Errorf("%w: agent_id is required", ErrInvalid)
	}

	return nil
}

// ValidateOutboxEntry validates an outbox entry before its write-before-send
// insert: workspace, session, and payload are required, and the status is
// forced to pending — entries are born claimable, never pre-delivered.
func ValidateOutboxEntry(e *OutboxEntry) error {
	if e == nil {
		return fmt.Errorf("%w: outbox entry is required", ErrInvalid)
	}

	if strings.TrimSpace(e.WorkspaceID) == "" {
		return fmt.Errorf("%w: workspace is required", ErrInvalid)
	}
	if strings.TrimSpace(e.SessionID) == "" {
		return fmt.Errorf("%w: session_id is required", ErrInvalid)
	}
	if len(e.Payload) == 0 {
		return fmt.Errorf("%w: payload is required", ErrInvalid)
	}
	e.Status = GatewayOutboxStatusPending

	return nil
}
