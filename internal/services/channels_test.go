package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// channelFixture wires a bound ChannelService over the fake store.
type channelFixture struct {
	st      *fake.Store
	svc     *ChannelService
	emitter *recordingEmitter
}

func newChannelFixture(t *testing.T) *channelFixture {
	t.Helper()
	f := &channelFixture{
		st:      fake.New(),
		emitter: &recordingEmitter{},
	}
	f.svc = NewChannelService(f.st, f.emitter)
	return f
}

// saveChannel stores one channel through the bound service and waits for its
// (asynchronously emitted) change event to land, so later event-count
// snapshots are stable.
func (f *channelFixture) saveChannel(t *testing.T, in ChannelInput) ChannelView {
	t.Helper()
	before := len(f.emitter.recorded())
	view, err := f.svc.SaveChannel(context.Background(), "telegram", in)
	require.NoError(t, err)
	drainEvents(t, f.emitter, before, 1)
	return view
}

func TestChannelService_TokenNeverLeaks(t *testing.T) {
	// The hint is the token's last four characters — the whole token when it
	// is four runes or fewer, and empty when unset (the provider keyHint
	// contract).
	for _, tt := range []struct {
		name  string
		token string
		hint  string
	}{
		{"long token", "123456:AAHx-token-abcd", "abcd"},
		{"four chars", "abcd", "abcd"},
		{"short token", "ab", "ab"},
		{"empty token", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newChannelFixture(t)
			ctx := context.Background()

			view := f.saveChannel(t, ChannelInput{Enabled: true, BotToken: tt.token})
			require.Equal(t, tt.hint, view.TokenHint, "the hint is the token's last four")
			require.Equal(t, tt.token != "", view.TokenSet, "TokenSet reports presence")
			if len(tt.token) > 4 {
				require.NotEqual(t, tt.token, view.TokenHint, "the full token never crosses the boundary")
			}

			got, err := f.svc.GetChannel(ctx, "telegram")
			require.NoError(t, err)
			require.Equal(t, tt.hint, got.TokenHint, "reads never return the token")
			require.Equal(t, tt.token != "", got.TokenSet)
		})
	}
}

func TestChannelService_SaveChannel_BlankTokenKeepsStored(t *testing.T) {
	f := newChannelFixture(t)
	ctx := context.Background()

	view := f.saveChannel(t, ChannelInput{Enabled: true, BotToken: "123456:keep-me-9999", ProxyURL: "socks5://localhost:1080"})
	require.True(t, view.TokenSet)

	// An edit that leaves the token field untouched (blank) keeps the stored
	// token while the other fields move.
	updated, err := f.svc.SaveChannel(ctx, "telegram", ChannelInput{Enabled: false, ProxyURL: ""})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.True(t, updated.TokenSet, "the blank save must keep the stored token")
	require.Equal(t, "9999", updated.TokenHint)
	require.Empty(t, updated.ProxyURL)

	stored, err := f.st.Channels().GetChannel(ctx, "telegram")
	require.NoError(t, err)
	require.Equal(t, "123456:keep-me-9999", stored.BotToken, "the stored token survives verbatim")

	// A non-blank token replaces it.
	replaced, err := f.svc.SaveChannel(ctx, "telegram", ChannelInput{Enabled: true, BotToken: "123456:new"})
	require.NoError(t, err)
	require.Equal(t, "123456:new", mustStoredToken(t, f, "telegram"))
	require.Equal(t, ":new", replaced.TokenHint, "the hint is the new token's last four")
}

// mustStoredToken reads the raw stored token straight from the store — the
// one place a test may look at the secret to verify it survived.
func mustStoredToken(t *testing.T, f *channelFixture, id string) string {
	t.Helper()
	stored, err := f.st.Channels().GetChannel(context.Background(), id)
	require.NoError(t, err)
	return stored.BotToken
}

func TestChannelService_GetChannel_AbsentRow(t *testing.T) {
	f := newChannelFixture(t)

	view, err := f.svc.GetChannel(context.Background(), "telegram")
	require.NoError(t, err, "an absent row is a normal state (fresh install)")
	require.Equal(t, "telegram", view.ID)
	require.False(t, view.Enabled)
	require.False(t, view.TokenSet)
	require.Empty(t, view.TokenHint)
}

func TestChannelService_SetEnabled(t *testing.T) {
	f := newChannelFixture(t)
	ctx := context.Background()
	f.saveChannel(t, ChannelInput{Enabled: false, BotToken: "123456:tok"})

	before := len(f.emitter.recorded())
	view, err := f.svc.SetEnabled(ctx, "telegram", true)
	require.NoError(t, err)
	require.True(t, view.Enabled)
	require.Equal(t, ":tok", view.TokenHint, "toggling keeps the stored token")

	events := drainEvents(t, f.emitter, before, 1)
	require.Equal(t, EventChannelChanged, events[0].Name)
	require.Equal(t, ChannelChangedEvent{ChannelID: "telegram"}, events[0].Data[0])
}

func TestChannelService_Chats(t *testing.T) {
	f := newChannelFixture(t)
	ctx := context.Background()

	chats, err := f.svc.ListChats(ctx)
	require.NoError(t, err)
	require.Empty(t, chats, "a fresh chat list is empty")

	// Seed two chats directly in the store; the service only mutates them.
	require.NoError(t, f.st.Channels().SaveChannelChat(ctx, store.ChannelChat{ChatID: 100, Kind: store.ChannelChatKindDM, Title: "Ada"}))
	require.NoError(t, f.st.Channels().SaveChannelChat(ctx, store.ChannelChat{ChatID: 200, Kind: store.ChannelChatKindGroup, Title: "Ops"}))

	before := len(f.emitter.recorded())
	pinned, err := f.svc.SetChatPinnedAgent(ctx, 100, "agt-1")
	require.NoError(t, err)
	require.Equal(t, "agt-1", pinned.PinnedAgentID)
	drainEvents(t, f.emitter, before, 1)

	home, err := f.svc.SetHomeChat(ctx, 200)
	require.NoError(t, err)
	require.True(t, home.IsHome)

	free, err := f.svc.SetChatFreeResponse(ctx, 200, true)
	require.NoError(t, err)
	require.True(t, free.FreeResponse)
	require.True(t, free.IsHome, "mutating another flag keeps the home marker")

	// Single home: marking the DM home clears the group's marker (the store
	// constraint, visible through the service read).
	dm, err := f.svc.SetHomeChat(ctx, 100)
	require.NoError(t, err)
	require.True(t, dm.IsHome)

	chats, err = f.svc.ListChats(ctx)
	require.NoError(t, err)
	require.Len(t, chats, 2)
	require.Equal(t, int64(100), chats[0].ChatID, "list must order by chat id")
	require.Equal(t, "agt-1", chats[0].PinnedAgentID)
	require.True(t, chats[0].IsHome)
	require.Equal(t, int64(200), chats[1].ChatID)
	require.False(t, chats[1].IsHome, "saving a new home chat must clear the previous one")
	require.True(t, chats[1].FreeResponse)

	// Clearing the pin flattens to the empty string.
	cleared, err := f.svc.SetChatPinnedAgent(ctx, 100, "")
	require.NoError(t, err)
	require.Empty(t, cleared.PinnedAgentID)

	// Unknown chats read as the sentinel, never silently created.
	_, err = f.svc.SetChatPinnedAgent(ctx, 999, "agt-1")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = f.svc.SetHomeChat(ctx, 999)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = f.svc.SetChatFreeResponse(ctx, 999, true)
	require.ErrorIs(t, err, store.ErrNotFound)

	// Every chat mutation announced the change.
	drainEvents(t, f.emitter, before, 5)
}

func TestChannelService_PairingApproval(t *testing.T) {
	f := newChannelFixture(t)
	ctx := context.Background()

	pairings, err := f.svc.ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Empty(t, pairings, "nothing pends on a fresh store")

	require.NoError(t, f.st.Channels().AddChannelPairing(ctx, store.ChannelPairing{TelegramUserID: 42, DisplayName: "Ada"}))
	require.NoError(t, f.st.Channels().AddChannelPairing(ctx, store.ChannelPairing{TelegramUserID: 7, DisplayName: "Grace"}))

	// Approve moves the user onto the allowlist and drops the pairing.
	before := len(f.emitter.recorded())
	require.NoError(t, f.svc.ApprovePairing(ctx, 42))
	drainEvents(t, f.emitter, before, 1)

	users, err := f.st.Channels().ListChannelUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, int64(42), users[0].TelegramUserID)
	require.Equal(t, "Ada", users[0].DisplayName, "the display name rides the approval")

	pairings, err = f.svc.ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Len(t, pairings, 1)
	require.Equal(t, int64(7), pairings[0].TelegramUserID)

	// The resolved request is gone: approving it again is the sentinel.
	require.ErrorIs(t, f.svc.ApprovePairing(ctx, 42), store.ErrNotFound)

	// Dismiss drops the request without allowlisting its user.
	before = len(f.emitter.recorded())
	require.NoError(t, f.svc.DismissPairing(ctx, 7))
	drainEvents(t, f.emitter, before, 1)

	pairings, err = f.svc.ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Empty(t, pairings)
	users, err = f.st.Channels().ListChannelUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1, "a dismissed user is never allowlisted")

	require.ErrorIs(t, f.svc.DismissPairing(ctx, 7), store.ErrNotFound)
}

func TestChannelService_ApprovePairing_StaleRequest(t *testing.T) {
	f := newChannelFixture(t)
	ctx := context.Background()

	require.NoError(t, f.st.Channels().AddChannelPairing(ctx, store.ChannelPairing{TelegramUserID: 42, DisplayName: "Ada"}))
	require.NoError(t, f.st.Channels().AddChannelUser(ctx, store.ChannelUser{TelegramUserID: 42, DisplayName: "Ada"}))

	// The user is already allowlisted: the stale request still resolves —
	// dropped, allowlist unchanged, no error.
	require.NoError(t, f.svc.ApprovePairing(ctx, 42))

	users, err := f.st.Channels().ListChannelUsers(ctx)
	require.NoError(t, err)
	require.Len(t, users, 1)
	pairings, err := f.svc.ListPendingPairings(ctx)
	require.NoError(t, err)
	require.Empty(t, pairings)
}
