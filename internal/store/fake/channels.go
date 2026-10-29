package fake

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
)

// channelReceiptKey is the composite identity of one processed-update marker
// (bot username, update id) — the dedupe pair of design D9.
type channelReceiptKey struct {
	BotUsername string
	UpdateID    int64
}

type channelStore struct{ s *Store }

// GetChannel returns one channel configuration row; ErrNotFound when absent.
func (f channelStore) GetChannel(_ context.Context, id string) (*store.Channel, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	ch, ok := f.s.channels[id]
	if !ok {
		return nil, fmt.Errorf("channel %s: %w", id, store.ErrNotFound)
	}
	out := ch
	return &out, nil
}

// ListChannels returns every channel row ordered by id. The result is never
// nil.
func (f channelStore) ListChannels(_ context.Context) ([]store.Channel, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.Channel, 0, len(f.s.channels))
	for _, ch := range f.s.channels {
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SaveChannel upserts the channel atomically, mirroring the sqlite adapter's
// INSERT ... ON CONFLICT: an absent id inserts, a present id rewrites the
// configuration columns in place, keeping the original CreatedAt.
func (f channelStore) SaveChannel(_ context.Context, ch *store.Channel) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	stored, ok := f.s.channels[ch.ID]
	if ok {
		ch.CreatedAt = stored.CreatedAt
	} else if ch.CreatedAt.IsZero() {
		ch.CreatedAt = nowUTC()
	}
	if ch.UpdatedAt.IsZero() {
		ch.UpdatedAt = nowUTC()
	}
	f.s.channels[ch.ID] = *ch
	return nil
}

// ListChannelUsers returns the DM allowlist ordered by added time, then user
// id. The result is never nil.
func (f channelStore) ListChannelUsers(_ context.Context) ([]store.ChannelUser, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.ChannelUser, 0, len(f.s.channelUsers))
	for _, u := range f.s.channelUsers {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].TelegramUserID < out[j].TelegramUserID
	})
	return out, nil
}

// AddChannelUser allowlists one user; a user already on the allowlist is the
// ErrDuplicate sentinel, mirroring the primary-key collision in sqlite.
func (f channelStore) AddChannelUser(_ context.Context, u store.ChannelUser) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.channelUsers[u.TelegramUserID]; ok {
		return fmt.Errorf("add channel user %d: %w", u.TelegramUserID, store.ErrDuplicate)
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = nowUTC()
	}
	f.s.channelUsers[u.TelegramUserID] = u
	return nil
}

// RemoveChannelUser drops one allowlisted user; ErrNotFound when absent.
func (f channelStore) RemoveChannelUser(_ context.Context, telegramUserID int64) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.channelUsers[telegramUserID]; !ok {
		return fmt.Errorf("remove channel user %d: %w", telegramUserID, store.ErrNotFound)
	}
	delete(f.s.channelUsers, telegramUserID)
	return nil
}

// GetChannelChat returns one chat's state; ErrNotFound when absent.
func (f channelStore) GetChannelChat(_ context.Context, chatID int64) (*store.ChannelChat, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	c, ok := f.s.channelChats[chatID]
	if !ok {
		return nil, fmt.Errorf("channel chat %d: %w", chatID, store.ErrNotFound)
	}
	out := cloneChannelChat(c)
	return &out, nil
}

// ListChannelChats returns every chat's state ordered by chat id. The result
// is never nil.
func (f channelStore) ListChannelChats(_ context.Context) ([]store.ChannelChat, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.ChannelChat, 0, len(f.s.channelChats))
	for _, c := range f.s.channelChats {
		out = append(out, cloneChannelChat(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChatID < out[j].ChatID })
	return out, nil
}

// SaveChannelChat upserts one chat by its chat id. Saving the home chat first
// clears every other row's home marker, so at most one chat is home (design
// D3: single home).
func (f channelStore) SaveChannelChat(_ context.Context, c store.ChannelChat) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if c.IsHome {
		for id := range f.s.channelChats {
			if id != c.ChatID {
				other := f.s.channelChats[id]
				other.IsHome = false
				f.s.channelChats[id] = other
			}
		}
	}
	f.s.channelChats[c.ChatID] = cloneChannelChat(c)
	return nil
}

// ListPendingPairings returns the pending pairing requests ordered by request
// time, then user id. The result is never nil.
func (f channelStore) ListPendingPairings(_ context.Context) ([]store.ChannelPairing, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	out := make([]store.ChannelPairing, 0, len(f.s.channelPairings))
	for _, p := range f.s.channelPairings {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].RequestedAt.Equal(out[j].RequestedAt) {
			return out[i].RequestedAt.Before(out[j].RequestedAt)
		}
		return out[i].TelegramUserID < out[j].TelegramUserID
	})
	return out, nil
}

// AddChannelPairing records one pending pairing; a pairing already pending
// for the user is the ErrDuplicate sentinel.
func (f channelStore) AddChannelPairing(_ context.Context, p store.ChannelPairing) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.channelPairings[p.TelegramUserID]; ok {
		return fmt.Errorf("add channel pairing %d: %w", p.TelegramUserID, store.ErrDuplicate)
	}
	if p.RequestedAt.IsZero() {
		p.RequestedAt = nowUTC()
	}
	f.s.channelPairings[p.TelegramUserID] = p
	return nil
}

// RemoveChannelPairing drops one pending pairing; ErrNotFound when absent.
func (f channelStore) RemoveChannelPairing(_ context.Context, telegramUserID int64) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if _, ok := f.s.channelPairings[telegramUserID]; !ok {
		return fmt.Errorf("remove channel pairing %d: %w", telegramUserID, store.ErrNotFound)
	}
	delete(f.s.channelPairings, telegramUserID)
	return nil
}

// AddUpdateReceipt records one processed update idempotently: re-adding the
// same (bot username, update id) pair is a no-op that keeps the original
// marker, mirroring sqlite's INSERT OR IGNORE (design D9).
func (f channelStore) AddUpdateReceipt(_ context.Context, r store.ChannelUpdateReceipt) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	key := channelReceiptKey{BotUsername: r.BotUsername, UpdateID: r.UpdateID}
	if _, ok := f.s.channelReceipts[key]; ok {
		return nil
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = nowUTC()
	}
	f.s.channelReceipts[key] = r
	return nil
}

// HasUpdateReceipt reports whether the update was already processed.
func (f channelStore) HasUpdateReceipt(_ context.Context, botUsername string, updateID int64) (bool, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	_, ok := f.s.channelReceipts[channelReceiptKey{BotUsername: botUsername, UpdateID: updateID}]
	return ok, nil
}

// PruneUpdateReceipts deletes the receipts created before olderThan.
func (f channelStore) PruneUpdateReceipts(_ context.Context, olderThan time.Time) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	for key, r := range f.s.channelReceipts {
		if r.CreatedAt.Before(olderThan) {
			delete(f.s.channelReceipts, key)
		}
	}
	return nil
}

// cloneChannelChat copies a chat value, re-pointing the nullable agent
// override so the stored value never aliases a caller's string.
func cloneChannelChat(in store.ChannelChat) store.ChannelChat {
	if in.PinnedAgentID != nil {
		agent := *in.PinnedAgentID
		in.PinnedAgentID = &agent
	}
	return in
}
