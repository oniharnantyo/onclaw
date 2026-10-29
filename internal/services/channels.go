package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// ChannelView is the read shape of one channel configuration, as bound to the
// frontend. It deliberately carries no bot token: the raw secret never
// crosses the service boundary (add-telegram-channel task 1.4, the provider
// key precedent) — callers see only whether a token is stored and its
// non-secret last-four hint.
type ChannelView struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	// TokenSet and TokenHint are the secret-free token views: only whether a
	// bot token is stored and its last four characters, empty when unset.
	TokenSet  bool      `json:"tokenSet"`
	TokenHint string    `json:"tokenHint"`
	ProxyURL  string    `json:"proxyUrl"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ChannelInput is the write shape of one channel configuration (the
// ProviderInput pattern): the id is addressed separately and a blank BotToken
// preserves the stored one — the edit form resubmits only what changed, and
// the token field is write-only.
type ChannelInput struct {
	Enabled  bool
	BotToken string
	ProxyURL string
}

// ChannelChatView is the read shape of one channel chat's persisted state.
// PinnedAgentID flattens the store's nullable pointer: the empty string reads
// as "no override".
type ChannelChatView struct {
	ChatID        int64  `json:"chatId"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	SessionID     string `json:"sessionId"`
	PinnedAgentID string `json:"pinnedAgentId"`
	IsHome        bool   `json:"isHome"`
	FreeResponse  bool   `json:"freeResponse"`
}

// ChannelService is the bound service for the Channels pane (add-telegram-
// channel task 1.4): channel configuration (token write-only), the per-chat
// state rows, and the pending-pairing resolution. Every mutation is announced
// through the emitter as EventChannelChanged so the pane refetches.
type ChannelService struct {
	store   store.Store
	emitter Emitter
}

// NewChannelService returns the channel service over st, announcing every
// mutation through emitter.
func NewChannelService(st store.Store, emitter Emitter) *ChannelService {
	return &ChannelService{store: st, emitter: emitter}
}

// GetChannel returns one channel's view. An absent row is a normal state —
// fresh installs boot before any save (the GatewaySettings precedent) — and
// reads as the not-configured default: disabled, no token.
func (s *ChannelService) GetChannel(ctx context.Context, id string) (ChannelView, error) {
	ch, err := s.storedOrDefault(ctx, id)
	if err != nil {
		return ChannelView{}, err
	}
	return newChannelView(*ch), nil
}

// SaveChannel upserts one channel's configuration; a blank BotToken keeps the
// stored token (task 1.4: the token field is write-only).
func (s *ChannelService) SaveChannel(ctx context.Context, id string, in ChannelInput) (ChannelView, error) {
	stored, err := s.storedOrDefault(ctx, id)
	if err != nil {
		return ChannelView{}, err
	}
	token := in.BotToken
	if token == "" {
		token = stored.BotToken
	}
	ch := &store.Channel{ID: id, Enabled: in.Enabled, BotToken: token, ProxyURL: in.ProxyURL}
	if err := s.store.Channels().SaveChannel(ctx, ch); err != nil {
		return ChannelView{}, fmt.Errorf("save channel %s: %w", id, err)
	}
	s.changed(id)
	return newChannelView(*ch), nil
}

// SetEnabled toggles the channel's enabled flag without touching the stored
// token or proxy.
func (s *ChannelService) SetEnabled(ctx context.Context, id string, enabled bool) (ChannelView, error) {
	stored, err := s.storedOrDefault(ctx, id)
	if err != nil {
		return ChannelView{}, err
	}
	ch := &store.Channel{ID: id, Enabled: enabled, BotToken: stored.BotToken, ProxyURL: stored.ProxyURL}
	if err := s.store.Channels().SaveChannel(ctx, ch); err != nil {
		return ChannelView{}, fmt.Errorf("set channel %s enabled: %w", id, err)
	}
	s.changed(id)
	return newChannelView(*ch), nil
}

// ListChats returns every chat's state ordered by chat id. The result is
// empty (not nil) when no chat is known.
func (s *ChannelService) ListChats(ctx context.Context) ([]ChannelChatView, error) {
	chats, err := s.store.Channels().ListChannelChats(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channel chats: %w", err)
	}
	views := make([]ChannelChatView, 0, len(chats))
	for _, c := range chats {
		views = append(views, newChannelChatView(c))
	}
	return views, nil
}

// SetChatPinnedAgent pins the chat's agent override; an empty agentID clears
// the pin (the store normalizes both nil and empty to "no override").
// store.ErrNotFound when the chat is absent.
func (s *ChannelService) SetChatPinnedAgent(ctx context.Context, chatID int64, agentID string) (ChannelChatView, error) {
	return s.mutateChat(ctx, chatID, func(c *store.ChannelChat) { c.PinnedAgentID = pinPtr(agentID) })
}

// SetHomeChat marks the chat the announcement target (design D13); the store
// clears the marker on every other row, so at most one chat is home.
// store.ErrNotFound when the chat is absent.
func (s *ChannelService) SetHomeChat(ctx context.Context, chatID int64) (ChannelChatView, error) {
	return s.mutateChat(ctx, chatID, func(c *store.ChannelChat) { c.IsHome = true })
}

// SetChatFreeResponse toggles the group chat's unaddressed-message gate
// (design D4). store.ErrNotFound when the chat is absent.
func (s *ChannelService) SetChatFreeResponse(ctx context.Context, chatID int64, free bool) (ChannelChatView, error) {
	return s.mutateChat(ctx, chatID, func(c *store.ChannelChat) { c.FreeResponse = free })
}

// ListPendingPairings returns the pending authorization requests ordered by
// request time. The result is empty (not nil) when nothing pends.
func (s *ChannelService) ListPendingPairings(ctx context.Context) ([]store.ChannelPairing, error) {
	pairings, err := s.store.Channels().ListPendingPairings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list channel pairings: %w", err)
	}
	return pairings, nil
}

// ApprovePairing resolves one pending request by moving the user onto the DM
// allowlist and dropping the pairing (design D4). A pairing whose user is
// already allowlisted still resolves — the stale request is dropped; an
// unknown request wraps store.ErrNotFound.
func (s *ChannelService) ApprovePairing(ctx context.Context, telegramUserID int64) error {
	pairings, err := s.store.Channels().ListPendingPairings(ctx)
	if err != nil {
		return fmt.Errorf("approve pairing %d: %w", telegramUserID, err)
	}
	var pairing store.ChannelPairing
	found := false
	for _, p := range pairings {
		if p.TelegramUserID == telegramUserID {
			pairing = p
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("approve pairing %d: %w", telegramUserID, store.ErrNotFound)
	}

	err = s.store.Channels().AddChannelUser(ctx, store.ChannelUser{
		TelegramUserID: pairing.TelegramUserID,
		DisplayName:    pairing.DisplayName,
	})
	// A user already on the allowlist means the pending request went stale;
	// resolution still drops it.
	if err != nil && !errors.Is(err, store.ErrDuplicate) {
		return fmt.Errorf("approve pairing %d: %w", telegramUserID, err)
	}
	if err := s.store.Channels().RemoveChannelPairing(ctx, telegramUserID); err != nil {
		return fmt.Errorf("approve pairing %d: %w", telegramUserID, err)
	}
	s.changed("")
	return nil
}

// DismissPairing drops one pending request without allowlisting its user;
// an unknown request wraps store.ErrNotFound.
func (s *ChannelService) DismissPairing(ctx context.Context, telegramUserID int64) error {
	if err := s.store.Channels().RemoveChannelPairing(ctx, telegramUserID); err != nil {
		return fmt.Errorf("dismiss pairing %d: %w", telegramUserID, err)
	}
	s.changed("")
	return nil
}

// storedOrDefault returns the stored channel row, or a not-configured default
// when absent (fresh installs boot before any save). Other failures surface.
func (s *ChannelService) storedOrDefault(ctx context.Context, id string) (*store.Channel, error) {
	ch, err := s.store.Channels().GetChannel(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return &store.Channel{ID: id}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get channel %s: %w", id, err)
	}
	return ch, nil
}

// mutateChat loads one chat row, applies fn, saves, and announces the change;
// store.ErrNotFound when the chat is absent.
func (s *ChannelService) mutateChat(ctx context.Context, chatID int64, fn func(*store.ChannelChat)) (ChannelChatView, error) {
	c, err := s.store.Channels().GetChannelChat(ctx, chatID)
	if err != nil {
		return ChannelChatView{}, fmt.Errorf("get channel chat %d: %w", chatID, err)
	}
	fn(c)
	if err := s.store.Channels().SaveChannelChat(ctx, *c); err != nil {
		return ChannelChatView{}, fmt.Errorf("save channel chat %d: %w", chatID, err)
	}
	s.changed("")
	return newChannelChatView(*c), nil
}

// changed announces a channel mutation to the pane. The emitter contract
// forbids blocking, so the emit runs on its own goroutine (the knowledge
// service pattern). Chat and pairing rows are not channel-keyed, so their
// events carry an empty id — fields not applicable to the change are empty
// strings (the SchedulesChangedEvent precedent).
func (s *ChannelService) changed(channelID string) {
	go s.emitter.Emit(EventChannelChanged, ChannelChangedEvent{ChannelID: channelID})
}

// newChannelView projects a stored channel onto the read shape. The raw
// token is consumed here and must not leak into the result.
func newChannelView(ch store.Channel) ChannelView {
	return ChannelView{
		ID:        ch.ID,
		Enabled:   ch.Enabled,
		TokenSet:  ch.BotToken != "",
		TokenHint: keyHint(ch.BotToken),
		ProxyURL:  ch.ProxyURL,
		CreatedAt: ch.CreatedAt,
		UpdatedAt: ch.UpdatedAt,
	}
}

// newChannelChatView projects a stored chat row onto the read shape,
// flattening the nullable agent override to the empty string.
func newChannelChatView(c store.ChannelChat) ChannelChatView {
	var pinned string
	if c.PinnedAgentID != nil {
		pinned = *c.PinnedAgentID
	}
	return ChannelChatView{
		ChatID:        c.ChatID,
		Kind:          c.Kind,
		Title:         c.Title,
		SessionID:     c.SessionID,
		PinnedAgentID: pinned,
		IsHome:        c.IsHome,
		FreeResponse:  c.FreeResponse,
	}
}

// pinPtr encodes the pinned-agent override: the empty string is "no
// override" and stores as nil.
func pinPtr(agentID string) *string {
	if agentID == "" {
		return nil
	}
	return &agentID
}
