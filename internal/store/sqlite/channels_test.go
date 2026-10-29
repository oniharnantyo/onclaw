package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestChannels_ChannelRoundTrip covers the full column round trip of the
// channel config row and the atomic upsert: an absent id inserts, a present
// id rewrites the configuration columns in place and keeps CreatedAt.
func TestChannels_ChannelRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	channels := s.Channels()

	_, err := channels.GetChannel(ctx, "telegram")
	require.ErrorIs(t, err, store.ErrNotFound, "an absent channel reads as the sentinel")

	in := &store.Channel{ID: "telegram", Enabled: true, BotToken: "123:ABC", ProxyURL: "socks5://localhost:1080"}
	require.NoError(t, channels.SaveChannel(ctx, in))
	require.False(t, in.CreatedAt.IsZero(), "SaveChannel must stamp CreatedAt")
	require.False(t, in.UpdatedAt.IsZero(), "SaveChannel must stamp UpdatedAt")

	got, err := channels.GetChannel(ctx, "telegram")
	require.NoError(t, err)
	require.Equal(t, "telegram", got.ID)
	require.True(t, got.Enabled)
	require.Equal(t, "123:ABC", got.BotToken)
	require.Equal(t, "socks5://localhost:1080", got.ProxyURL)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt))
	require.True(t, got.UpdatedAt.Equal(in.UpdatedAt))

	// The upsert rewrites the row: enabled flips off, token and proxy move,
	// and the original CreatedAt survives.
	in.Enabled = false
	in.BotToken = "123:DEF"
	in.ProxyURL = ""
	require.NoError(t, channels.SaveChannel(ctx, in))

	got, err = channels.GetChannel(ctx, "telegram")
	require.NoError(t, err)
	require.False(t, got.Enabled)
	require.Equal(t, "123:DEF", got.BotToken)
	require.Empty(t, got.ProxyURL)
	require.True(t, got.CreatedAt.Equal(in.CreatedAt), "the upsert must keep the original CreatedAt")
	require.False(t, got.UpdatedAt.Before(in.UpdatedAt), "the upsert must advance UpdatedAt")
}

// TestChannels_Users covers the DM allowlist: add, list, remove, and the
// unique-user constraint — a second add of the same user id is the
// ErrDuplicate sentinel, removing an absent user is ErrNotFound. Added times
// are explicit so the (added time, user id) order is deterministic.
func TestChannels_Users(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	channels := s.Channels()

	users, err := channels.ListChannelUsers(ctx)
	require.NoError(t, err)
	require.Empty(t, users, "a fresh allowlist is empty")

	ten := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	nine := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	first := store.ChannelUser{TelegramUserID: 42, DisplayName: "Ada", CreatedAt: ten}
	require.NoError(t, channels.AddChannelUser(ctx, first))
	require.NoError(t, channels.AddChannelUser(ctx, store.ChannelUser{TelegramUserID: 7, DisplayName: "Grace", CreatedAt: nine}))

	// A zero CreatedAt is stamped for storage (the caller's value copy never
	// changes; the stamp is verified through the read below).
	stamped := store.ChannelUser{TelegramUserID: 99, DisplayName: "Zero"}
	require.True(t, stamped.CreatedAt.IsZero())
	require.NoError(t, channels.AddChannelUser(ctx, stamped))

	require.ErrorIs(t, channels.AddChannelUser(ctx, store.ChannelUser{TelegramUserID: 42, DisplayName: "again"}),
		store.ErrDuplicate, "the unique user id must reject a second add")

	got, err := channels.ListChannelUsers(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, int64(7), got[0].TelegramUserID, "list must order by added time, then user id")
	require.True(t, got[0].CreatedAt.Equal(nine), "CreatedAt must round-trip")
	require.Equal(t, int64(42), got[1].TelegramUserID)
	require.True(t, got[1].CreatedAt.Equal(ten))
	require.Equal(t, int64(99), got[2].TelegramUserID, "the stamped row sorts by its stamped time")
	require.False(t, got[2].CreatedAt.IsZero(), "a zero CreatedAt is stamped on add")

	require.NoError(t, channels.RemoveChannelUser(ctx, 42))
	require.ErrorIs(t, channels.RemoveChannelUser(ctx, 42), store.ErrNotFound)

	got, err = channels.ListChannelUsers(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, int64(7), got[0].TelegramUserID)
	require.Equal(t, int64(99), got[1].TelegramUserID)
}

// TestChannels_Chats_SingleHome covers per-chat state: the upsert by chat
// id, the nullable pinned agent round trip, and the single-home constraint —
// saving a chat with is_home clears the marker on every other row, while a
// non-home save leaves the standing home alone.
func TestChannels_Chats_SingleHome(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	channels := s.Channels()

	agent := "agt-1"
	dm := store.ChannelChat{ChatID: 100, Kind: store.ChannelChatKindDM, Title: "Ada", SessionID: "sess-1", PinnedAgentID: &agent}
	require.NoError(t, channels.SaveChannelChat(ctx, dm))
	group := store.ChannelChat{ChatID: 200, Kind: store.ChannelChatKindGroup, Title: "Ops"}
	require.NoError(t, channels.SaveChannelChat(ctx, group))

	got, err := channels.GetChannelChat(ctx, 100)
	require.NoError(t, err)
	require.NotNil(t, got.PinnedAgentID)
	require.Equal(t, "agt-1", *got.PinnedAgentID, "the pinned agent must round-trip")
	require.Equal(t, "sess-1", got.SessionID)
	require.False(t, got.IsHome)

	// One home at a time: saving the group as home clears the DM's marker.
	group.IsHome = true
	require.NoError(t, channels.SaveChannelChat(ctx, group))
	home, err := channels.GetChannelChat(ctx, 200)
	require.NoError(t, err)
	require.True(t, home.IsHome)
	dmRow, err := channels.GetChannelChat(ctx, 100)
	require.NoError(t, err)
	require.False(t, dmRow.IsHome, "saving a new home chat must clear the previous one")

	// Saving the DM as home moves the marker back; a later non-home update
	// of the DM keeps the group home-less state intact.
	dm.IsHome = true
	dm.SessionID = "sess-2"
	require.NoError(t, channels.SaveChannelChat(ctx, dm))
	home, err = channels.GetChannelChat(ctx, 100)
	require.NoError(t, err)
	require.True(t, home.IsHome)
	require.Equal(t, "sess-2", home.SessionID, "the upsert must rewrite the state columns")
	home, err = channels.GetChannelChat(ctx, 200)
	require.NoError(t, err)
	require.False(t, home.IsHome)

	dm.IsHome = false
	require.NoError(t, channels.SaveChannelChat(ctx, dm))
	home, err = channels.GetChannelChat(ctx, 200)
	require.NoError(t, err)
	require.False(t, home.IsHome, "a non-home save must not resurrect a cleared marker")

	list, err := channels.ListChannelChats(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, int64(100), list[0].ChatID, "list must order by chat id")
	require.Equal(t, int64(200), list[1].ChatID)

	// Clearing the pin stores NULL again.
	dm.PinnedAgentID = nil
	require.NoError(t, channels.SaveChannelChat(ctx, dm))
	got, err = channels.GetChannelChat(ctx, 100)
	require.NoError(t, err)
	require.Nil(t, got.PinnedAgentID, "a cleared pin reads back nil")

	_, err = channels.GetChannelChat(ctx, 999)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestChannels_Chats_KindCheck covers migration 000018's CHECK constraint:
// only the dm and group kinds are storable.
func TestChannels_Chats_KindCheck(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.Channels().SaveChannelChat(ctx, store.ChannelChat{ChatID: 1, Kind: "channel"})
	require.Error(t, err, "the kind CHECK must reject an unknown kind")
}

// TestChannels_Pairings covers the pending pairing set: add, list, remove,
// and the one-pairing-per-user constraint. Request times are explicit so the
// (request time, user id) order is deterministic.
func TestChannels_Pairings(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	channels := s.Channels()

	pairings, err := channels.ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Empty(t, pairings)

	ten := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	nine := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	first := store.ChannelPairing{TelegramUserID: 42, DisplayName: "Ada", RequestedAt: ten}
	require.NoError(t, channels.AddChannelPairing(ctx, first))
	require.NoError(t, channels.AddChannelPairing(ctx, store.ChannelPairing{TelegramUserID: 7, DisplayName: "Grace", RequestedAt: nine}))

	// A zero RequestedAt is stamped for storage (verified through the read
	// below — the caller's value copy never changes).
	stamped := store.ChannelPairing{TelegramUserID: 99, DisplayName: "Zero"}
	require.True(t, stamped.RequestedAt.IsZero())
	require.NoError(t, channels.AddChannelPairing(ctx, stamped))

	require.ErrorIs(t, channels.AddChannelPairing(ctx, store.ChannelPairing{TelegramUserID: 42}),
		store.ErrDuplicate, "one pending pairing per user")

	got, err := channels.ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, int64(7), got[0].TelegramUserID, "list must order by request time, then user id")
	require.True(t, got[0].RequestedAt.Equal(nine), "RequestedAt must round-trip")
	require.Equal(t, int64(42), got[1].TelegramUserID)
	require.Equal(t, int64(99), got[2].TelegramUserID, "the stamped row sorts by its stamped time")
	require.False(t, got[2].RequestedAt.IsZero(), "a zero RequestedAt is stamped on add")

	require.NoError(t, channels.RemoveChannelPairing(ctx, 42))
	require.ErrorIs(t, channels.RemoveChannelPairing(ctx, 42), store.ErrNotFound)

	got, err = channels.ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, int64(7), got[0].TelegramUserID)
	require.Equal(t, int64(99), got[1].TelegramUserID)
}

// TestChannels_Receipts covers the processed-update markers: idempotent
// add, the has check, and the retention prune.
func TestChannels_Receipts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	channels := s.Channels()

	ok, err := channels.HasUpdateReceipt(ctx, "buddybot", 10)
	require.NoError(t, err)
	require.False(t, ok, "an unrecorded update is unmarked")

	receipt := store.ChannelUpdateReceipt{BotUsername: "buddybot", UpdateID: 10, CreatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}
	require.NoError(t, channels.AddUpdateReceipt(ctx, receipt))
	// A redelivered marker re-adds as a no-op (design D9).
	require.NoError(t, channels.AddUpdateReceipt(ctx, receipt))

	ok, err = channels.HasUpdateReceipt(ctx, "buddybot", 10)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = channels.HasUpdateReceipt(ctx, "buddybot", 11)
	require.NoError(t, err)
	require.False(t, ok, "the marker is scoped to the (bot, update id) pair")

	older := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	require.NoError(t, channels.AddUpdateReceipt(ctx, store.ChannelUpdateReceipt{BotUsername: "buddybot", UpdateID: 9, CreatedAt: older}))
	require.NoError(t, channels.PruneUpdateReceipts(ctx, time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)))

	ok, err = channels.HasUpdateReceipt(ctx, "buddybot", 10)
	require.NoError(t, err)
	require.True(t, ok, "prune must keep receipts at or after the cutoff")
	ok, err = channels.HasUpdateReceipt(ctx, "buddybot", 9)
	require.NoError(t, err)
	require.False(t, ok, "prune must drop receipts older than the cutoff")
}

// TestChannels_MigrateForwardFromV17 covers task 1.2's upgrade path: a
// database at the previous schema version (v17) migrates to v18 on open,
// creating the five channel tables without touching existing rows.
func TestChannels_MigrateForwardFromV17(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/v17.db"

	// Build a database at schema v17: every embedded migration but 000018.
	var upToV17 []migration
	for _, m := range mustEmbeddedMigrations(t) {
		if m.Version <= 17 {
			upToV17 = append(upToV17, m)
		}
	}
	require.Len(t, upToV17, 17)

	st17, err := openWith(ctx, path, upToV17)
	require.NoError(t, err)
	agent := &store.Agent{Name: "survivor", Slug: "survivor"}
	require.NoError(t, st17.Agents().Create(ctx, agent))
	require.NoError(t, st17.Close())

	db := openRaw(t, path)
	for _, table := range []string{"channels", "channel_users", "channel_chats", "channel_pairings", "channel_update_receipts"} {
		require.False(t, tableExists(t, db, table), "a v17 database must not hold %s yet", table)
	}

	// Opening with the full embedded list applies 000018 and keeps the data.
	st, err := Open(ctx, path)
	require.NoError(t, err, "a v17 database must migrate on open")
	t.Cleanup(func() { _ = st.Close() })

	var version int
	require.NoError(t, db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 18, version, "migration 000018 must be recorded as applied")

	for _, table := range []string{"channels", "channel_users", "channel_chats", "channel_pairings", "channel_update_receipts"} {
		require.True(t, tableExists(t, db, table), "table %s must exist after the upgrade", table)
	}
	got, err := st.Agents().ByID(ctx, agent.ID)
	require.NoError(t, err, "existing rows must survive the v18 migration")
	require.Equal(t, "survivor", got.Name)

	// The new aggregates are usable right after the upgrade.
	require.NoError(t, st.Channels().SaveChannel(ctx, &store.Channel{ID: "telegram", Enabled: true, BotToken: "123:ABC"}))
	ch, err := st.Channels().GetChannel(ctx, "telegram")
	require.NoError(t, err)
	require.Equal(t, "123:ABC", ch.BotToken)
}

// TestChannels_VisibleThroughTx covers the tx-scoped channel store: writes
// inside WithTx commit with the transaction.
func TestChannels_VisibleThroughTx(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.WithTx(ctx, func(tx store.Store) error {
		return tx.Channels().SaveChannel(ctx, &store.Channel{ID: "telegram", Enabled: true, BotToken: "123:TX"})
	})
	require.NoError(t, err)

	got, err := s.Channels().GetChannel(ctx, "telegram")
	require.NoError(t, err)
	require.True(t, got.Enabled)
}
