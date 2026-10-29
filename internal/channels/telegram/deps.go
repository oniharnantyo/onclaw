package telegram

import (
	"context"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// TurnView is the turn identity the delivery pump needs (the initiator rule,
// design D6): the turn id every event of the turn carries, and the done
// signal marking its terminal state. *agents.Turn satisfies it structurally;
// tests fake it.
type TurnView interface {
	// TurnID returns the turn's identifier.
	TurnID() string
	// Done returns a channel closed when the turn reaches a terminal state.
	Done() <-chan struct{}
}

// Runtime is the slice of the agent runtime the channel drives
// (add-telegram-channel design D5/D6): session creation, turn submission with
// an explicit origin, live subscriptions, the busy probe, cancellation, and
// approval resolution. The agents manager satisfies it structurally.
type Runtime interface {
	// CreateSession registers a new, untitled chat session for the agent;
	// store.ErrNotFound when the agent does not exist.
	CreateSession(ctx context.Context, agentID string) (sessions.Session, error)
	// Submit dispatches one turn and returns promptly while it runs in the
	// background; ErrTurnInProgress when the session already has a turn in
	// flight.
	Submit(ctx context.Context, req agents.SendRequest) (TurnView, error)
	// Subscribe registers a live subscription for the session's events.
	Subscribe(ctx context.Context, sessionID string) (agents.Subscription, error)
	// Busy reports whether the session has a turn in flight.
	Busy(ctx context.Context, sessionID string) bool
	// Cancel stops the session's in-flight turn; a no-op when none runs.
	Cancel(ctx context.Context, sessionID string) error
	// ResolveApproval submits the user's decision for a pending approval;
	// agents.ErrApprovalNotPending when the interrupt is no longer pending.
	ResolveApproval(ctx context.Context, sessionID, interruptID string, approved bool) error
}

// BotAPI is the slice of the Bot API client the channel consumes. The
// *botapi.Client satisfies it structurally; tests fake it with scripted
// behavior.
type BotAPI interface {
	// GetUpdates long-polls for updates newer than p.Offset.
	GetUpdates(ctx context.Context, p botapi.GetUpdatesParams) ([]botapi.Update, error)
	// SendMessage sends text to a chat and returns the sent message.
	SendMessage(ctx context.Context, p botapi.SendMessageParams) (*botapi.Message, error)
	// EditMessageText rewrites the text of a previously sent message.
	EditMessageText(ctx context.Context, p botapi.EditMessageTextParams) (*botapi.Message, error)
	// SendChatAction shows a chat status indicator (typing).
	SendChatAction(ctx context.Context, chatID int64, action string) error
	// SendDocument uploads a document and returns the sent message.
	SendDocument(ctx context.Context, p botapi.SendDocumentParams) (*botapi.Message, error)
	// GetFile resolves a file_id into a File naming the download path.
	GetFile(ctx context.Context, fileID string) (*botapi.File, error)
	// DownloadFile fetches one file's bytes by its server-relative path.
	DownloadFile(ctx context.Context, filePath string) ([]byte, error)
	// AnswerCallbackQuery acknowledges an inline keyboard tap.
	AnswerCallbackQuery(ctx context.Context, callbackQueryID string) error
	// GetMe returns the bot's own identity.
	GetMe(ctx context.Context) (*botapi.User, error)
	// GetWebhookInfo returns the webhook state (the poll-path probe).
	GetWebhookInfo(ctx context.Context) (*botapi.WebhookInfo, error)
	// SetMyCommands replaces the bot's command menu.
	SetMyCommands(ctx context.Context, commands []botapi.BotCommand) error
}

// AttachmentRegistry is the upload seam the media-inbound path registers
// downloaded files through (design D11). *attachments.Store satisfies it
// structurally.
type AttachmentRegistry interface {
	// Save stores the bytes and registers the metadata row, returning the
	// capability key the turn's AttachmentKeys must name.
	Save(ctx context.Context, name, mime string, data []byte) (sessions.Attachment, error)
}

// Artifacts is the seam that resolves one turn artifact (a workspace-relative
// path riding the turn_completed event) to readable bytes for sendDocument
// delivery (design D12). Unset — the default — artifact delivery is skipped.
type Artifacts interface {
	// Read returns one artifact's bytes. agentID locates the workspace; the
	// adapter resolves it to the agent's workspace directory.
	Read(ctx context.Context, agentID, relPath string) ([]byte, error)
}

// DefaultAgents reads the gateway's default agent (design D7's resolution
// order: chat pin → gateway default). The store's GatewaySettingsStore
// satisfies it structurally.
type DefaultAgents interface {
	// Get returns the saved settings; (nil, nil) when nothing was saved.
	Get(ctx context.Context) (*store.GatewaySettings, error)
}

// BotFactory builds a Bot API client for a channel row's connection settings.
// The channel re-reads its row on every (re)start, so the factory — not a
// fixed client — is the dependency: a token or proxy fix takes effect on the
// next start without recreating the service. The default factory builds a
// *botapi.Client, honoring the BUDDIES_TELEGRAM_API_BASE override.
type BotFactory func(token, proxyURL string) (BotAPI, error)

// StateSink receives every channel connection-state transition (design D10):
// the channel row id ("telegram"), one of the State* vocabulary values, and a
// human-readable detail for errors. Composition maps these onto the
// channel:state app event.
type StateSink func(channelID, state, detail string)

// Channel connection states — the StateSink vocabulary.
const (
	// StateConnected marks a healthy poll loop (send and poll path proven).
	StateConnected = "connected"
	// StateDegraded marks a loop that rebuilt after repeated failures and
	// has not yet proven sustained recovery.
	StateDegraded = "degraded"
	// StateError marks a terminal failure (auth, conflict): no polling.
	StateError = "error"
	// StateDisabled marks a channel row whose enabled flag is off.
	StateDisabled = "disabled"
)

var _ BotAPI = (*botapi.Client)(nil)
