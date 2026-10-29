package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// TestAnnounceNoHomeIsNoop pins design D13: no home chat, no send, no error.
func TestAnnounceNoHomeIsNoop(t *testing.T) {
	fx := newFixture(t)

	require.NoError(t, fx.svc.AnnounceRunCompletion(context.Background(), "nightly", "ran", "all good"))
	require.Empty(t, fx.bot.callsOf("sendMessage"))
}

// TestAnnounceDisabledIsNoop pins the disabled rule: a disabled channel
// announces nothing.
func TestAnnounceDisabledIsNoop(t *testing.T) {
	fx := newFixture(t)
	require.NoError(t, fx.st.Channels().SaveChannel(context.Background(), &store.Channel{ID: DefaultChannelID, Enabled: false, BotToken: "tok"}))
	seedHome(t, fx)

	require.NoError(t, fx.svc.AnnounceRunCompletion(context.Background(), "nightly", "ran", "all good"))
	require.Empty(t, fx.bot.callsOf("sendMessage"))
}

// TestAnnounceHomeDelivery pins the home-chat delivery and the payload shape:
// "<schedule name> — <outcome> — <excerpt>" through the normal outbound path.
func TestAnnounceHomeDelivery(t *testing.T) {
	fx := newFixture(t)
	seedHome(t, fx)

	require.NoError(t, fx.svc.AnnounceRunCompletion(context.Background(), "nightly-backup", "ran", "42 files synced"))
	sends := fx.bot.callsOf("sendMessage")
	require.Len(t, sends, 1)
	require.Equal(t, int64(42), sends[0].ChatID, "the home chat is the target")
	require.Equal(t, "nightly\\-backup — ran — 42 files synced", sends[0].Text)
	require.Equal(t, "MarkdownV2", sends[0].ParseMode, "the announcement rides the markdown pipeline")
}

// TestAnnounceExcerptClipped pins the defensive excerpt clip.
func TestAnnounceExcerptClipped(t *testing.T) {
	fx := newFixture(t)
	seedHome(t, fx)

	long := strings.Repeat("x", 1000)
	require.NoError(t, fx.svc.AnnounceRunCompletion(context.Background(), "s", "ran", long))
	sends := fx.bot.callsOf("sendMessage")
	require.Len(t, sends, 1)
	require.LessOrEqual(t, len([]rune(sends[0].Text)), 420, "the excerpt clips to the cap")
}

// TestAnnounceSendErrorReturns pins the error contract: send failures return
// (the scheduler logs them and never touches RecordFire outcomes).
func TestAnnounceSendErrorReturns(t *testing.T) {
	fx := newFixture(t)
	seedHome(t, fx)
	fx.bot.errFor["sendMessage"] = &botapi.Error{Kind: botapi.KindForbidden, Description: "bot blocked"}

	err := fx.svc.AnnounceRunCompletion(context.Background(), "nightly", "ran", "x")
	require.Error(t, err)
}

// seedHome marks chat 42 as the single home chat.
func seedHome(t *testing.T, fx *fixture) {
	t.Helper()
	require.NoError(t, fx.st.Channels().SaveChannelChat(context.Background(), store.ChannelChat{
		ChatID: 42, Kind: store.ChannelChatKindDM, Title: "Alice", IsHome: true,
	}))
}
