package store

import (
	"context"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// GatewayStore manages workspace-scoped gateway configuration:
// plural rows per workspace, keyed on (workspace, platform, identity),
// carrying the encrypted bot credential (an AES-256-GCM secrets envelope — the plaintext token never
// persists and is never returned by any read), the resolved bot username,
// the enable toggle, the transport mode, and the bound agent.
// Every query is workspace-scoped.
type GatewayStore interface {
	CreateGateway(ctx context.Context, workspaceID string, g *domain.GatewayConfig) error
	GetGateway(ctx context.Context, workspaceID, id string) (*domain.GatewayConfig, error)
	ListGateways(ctx context.Context, workspaceID string) ([]domain.GatewayConfig, error)
	ListGatewaysByPlatform(ctx context.Context, workspaceID, platform string) ([]domain.GatewayConfig, error)
	UpdateGateway(ctx context.Context, workspaceID string, g *domain.GatewayConfig) error
	SetGatewayEnabled(ctx context.Context, workspaceID, id string, enabled bool) error
	DeleteGateway(ctx context.Context, workspaceID, id string) error
}

// GatewayBindings manages the chat-to-agent routing table (design D2):
// agent-scoped bindings, unique per (platform, platform_chat_id) globally —
// one agent per platform chat, across every workspace. Every query is
// workspace-scoped.
type GatewayBindings interface {
	// CreateChatBinding inserts a binding. The store validates the binding
	// and pre-checks that the target agent exists in the workspace (plain
	// FK parity); a chat already bound — in this or any other workspace —
	// returns domain.ErrGatewayBindingConflict.
	CreateChatBinding(ctx context.Context, workspaceID string, b *domain.ChatBinding) error
	// GetChatBinding resolves the agent routing for one platform chat, or
	// (nil, nil) when the chat is unbound.
	GetChatBinding(ctx context.Context, workspaceID, platform, platformChatID string) (*domain.ChatBinding, error)
	// ListChatBindings returns the workspace's bindings, oldest first.
	ListChatBindings(ctx context.Context, workspaceID string) ([]domain.ChatBinding, error)
	// RemapBindingChat rewrites a binding's platform_chat_id (design D10
	// migrate_to_chat_id): the old chat id's session keys are abandoned,
	// not rewritten. Unknown bindings return domain.ErrNotFound; a new chat
	// id already bound returns domain.ErrGatewayBindingConflict.
	RemapBindingChat(ctx context.Context, workspaceID, platform, oldChatID, newChatID string) error
	// DeleteChatBinding removes a binding by id. Absent rows return
	// domain.ErrNotFound.
	DeleteChatBinding(ctx context.Context, workspaceID, id string) error
	// ActiveSessionSuffix reads the active deterministic session-key suffix
	// for one (platform, platform chat, agent) triple (design D3): 0 means
	// the base key (tg_dm_<uid>_<agent> / tg_group_<chat>_<agent> /
	// wa_dm_<uid>_<agent>) is active; n > 0 means the key carries the "_<n>"
	// suffix. Absence is 0, never an error. Direct-message chats key on the
	// platform user id as the chat id. The row persists across restarts so a
	// restarted gateway resumes the highest suffix instead of rewinding onto
	// a live transcript.
	ActiveSessionSuffix(ctx context.Context, platform, platformChatID, agentID string) (int64, error)
	// BumpActiveSessionSuffix mints the next session for the triple (the
	// /new archive step): it upserts the cursor row with suffix = suffix+1
	// and returns the new suffix atomically, so concurrent /new commands can
	// never mint the same key.
	BumpActiveSessionSuffix(ctx context.Context, platform, platformChatID, agentID string) (int64, error)
}

// GatewayLinks manages member identity pairing (design D6): the immutable
// platform-user-id → member links and their single-use pairing tokens.
// Runs always execute under the linked member's user id; unlinked platform
// identities never reach the runner. Every query is workspace-scoped.
type GatewayLinks interface {
	// CreateUserLink inserts an identity link. A duplicate
	// (platform, platform_user_id, workspace) — the platform identity is
	// already paired in this workspace — returns domain.ErrConflict.
	CreateUserLink(ctx context.Context, workspaceID string, l *domain.UserLink) error
	// GetUserLink resolves the member a platform identity is paired to, or
	// (nil, nil) when unpaired (the default-deny refusal path).
	GetUserLink(ctx context.Context, workspaceID, platform, platformUserID string) (*domain.UserLink, error)
	// ListUserLinks returns every identity link in the workspace.
	ListUserLinks(ctx context.Context, workspaceID string) ([]domain.UserLink, error)
	// ListUserLinksForMember returns the workspace's links for one member
	// (the pairing modal's "current linked identity" view).
	ListUserLinksForMember(ctx context.Context, workspaceID, userID string) ([]domain.UserLink, error)
	// DeleteUserLink unpairs a platform identity. Absent links return
	// domain.ErrNotFound.
	DeleteUserLink(ctx context.Context, workspaceID, platform, platformUserID string) error
	// CreatePairingToken mints a single-use pairing token. The store
	// validates the token shape and pre-checks the member reference.
	CreatePairingToken(ctx context.Context, t *domain.PairingToken) error
	// ConsumePairingToken atomically consumes a token: exactly one caller
	// wins (single-use), and only while unconsumed and unexpired. The
	// consumed token is returned with ConsumedAt set. Unknown tokens return
	// domain.ErrNotFound; expired or already-consumed tokens return
	// domain.ErrPairingTokenExpired.
	ConsumePairingToken(ctx context.Context, workspaceID, token string, now time.Time) (*domain.PairingToken, error)
	// RevokePairingToken deletes an unconsumed token (revocation).
	// Absent tokens return domain.ErrNotFound.
	RevokePairingToken(ctx context.Context, workspaceID, token string) error
}

// GatewayOutbox manages the durable delivery outbox (design D9): rows are
// written before the send and redelivered after a crash — at-least-once.
// Enqueue/mark operations are workspace-scoped; the claim and prune scans
// are intentionally unscoped maintenance operations (the
// ClaimDueSchedulers precedent): they serve the gateway's delivery worker,
// and every claimed row still carries its workspace.
type GatewayOutbox interface {
	// Enqueue inserts an entry born pending (write-before-send).
	Enqueue(ctx context.Context, e *domain.OutboxEntry) error
	// ClaimDue atomically claims up to limit pending entries whose
	// deliver_after is due at now, incrementing each one's attempts —
	// concurrent claimers never take the same entry (FOR UPDATE SKIP
	// LOCKED semantics; the fake evaluates under the store mutex).
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]domain.OutboxEntry, error)
	// MarkDelivered records a successful send. Absent entries return
	// domain.ErrNotFound.
	MarkDelivered(ctx context.Context, workspaceID, id string) error
	// Reschedule returns a failed entry to pending with a new deliver_after
	// instant (backoff policy is the caller's). Absent entries return
	// domain.ErrNotFound.
	Reschedule(ctx context.Context, workspaceID, id string, deliverAfter time.Time) error
	// MarkDead exhausts an entry (attempt budget spent): dead entries are
	// never claimed again. Absent entries return domain.ErrNotFound.
	MarkDead(ctx context.Context, workspaceID, id string) error
	// CountDead returns the number of dead entries belonging to one gateway
	// within the workspace (payload gateway_id match) — the health probe's
	// dead-delivery signal (add-whatsapp-gateway design D4: window-expired
	// deliveries die observably). The gateway id rides the payload, the only
	// place the generating gateway is recorded.
	CountDead(ctx context.Context, workspaceID, gatewayID string) (int64, error)
	// PruneDelivered deletes delivered entries created before the retention
	// horizon and returns how many rows went away.
	PruneDelivered(ctx context.Context, before time.Time) (int64, error)
}
