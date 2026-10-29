package store

import (
	"context"
	"time"
)

// ChannelStore persists the Telegram channel aggregates (add-telegram-channel
// design D3/D4): the channel configuration row, the DM allowlist, per-chat
// state, pending pairings, and the processed-update receipts. Absent rows
// read as ErrNotFound; a violated natural-key uniqueness (user id, pairing
// user id) reads as ErrDuplicate.
type ChannelStore interface {
	// GetChannel returns the channel with the given slug id; ErrNotFound
	// when absent.
	GetChannel(ctx context.Context, id string) (*Channel, error)
	// ListChannels returns every channel row ordered by id. The result is
	// never nil.
	ListChannels(ctx context.Context) ([]Channel, error)
	// SaveChannel upserts the channel atomically (add-telegram-channel task
	// 1.3): an absent id inserts, a present id rewrites the configuration
	// columns in place, keeping the original CreatedAt. Zero timestamps are
	// stamped (UTC).
	SaveChannel(ctx context.Context, c *Channel) error

	// ListChannelUsers returns the DM allowlist ordered by added time, then
	// user id. The result is never nil.
	ListChannelUsers(ctx context.Context) ([]ChannelUser, error)
	// AddChannelUser allowlists one user; a user already on the allowlist
	// returns ErrDuplicate. Zero CreatedAt is stamped (UTC).
	AddChannelUser(ctx context.Context, u ChannelUser) error
	// RemoveChannelUser removes one user from the allowlist; ErrNotFound
	// when absent.
	RemoveChannelUser(ctx context.Context, telegramUserID int64) error

	// GetChannelChat returns one chat's state; ErrNotFound when absent.
	GetChannelChat(ctx context.Context, chatID int64) (*ChannelChat, error)
	// ListChannelChats returns every chat's state ordered by chat id. The
	// result is never nil.
	ListChannelChats(ctx context.Context) ([]ChannelChat, error)
	// SaveChannelChat upserts one chat by its chat id. Saving a row with
	// IsHome clears IsHome on every other row (design D3: one home).
	SaveChannelChat(ctx context.Context, c ChannelChat) error

	// ListPendingPairings returns the pending pairing requests ordered by
	// request time, then user id. The result is never nil.
	ListPendingPairings(ctx context.Context) ([]ChannelPairing, error)
	// AddChannelPairing records one pending pairing; a pairing already
	// pending for the user returns ErrDuplicate. Zero RequestedAt is
	// stamped (UTC).
	AddChannelPairing(ctx context.Context, p ChannelPairing) error
	// RemoveChannelPairing drops one pending pairing (approved or
	// dismissed); ErrNotFound when absent.
	RemoveChannelPairing(ctx context.Context, telegramUserID int64) error

	// AddUpdateReceipt records one processed update. Re-adding the same
	// (bot username, update id) pair is a no-op — the marker is idempotent
	// (design D9).
	AddUpdateReceipt(ctx context.Context, r ChannelUpdateReceipt) error
	// HasUpdateReceipt reports whether the update was already processed.
	HasUpdateReceipt(ctx context.Context, botUsername string, updateID int64) (bool, error)
	// PruneUpdateReceipts deletes the receipts created before olderThan
	// (the service's lazy retention sweep).
	PruneUpdateReceipts(ctx context.Context, olderThan time.Time) error
}
