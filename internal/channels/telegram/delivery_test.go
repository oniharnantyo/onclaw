package telegram

import (
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/stretchr/testify/require"
)

// startTurn dispatches one DM message and waits for the submit and the
// pump's subscription; it returns the session id.
func startTurn(t *testing.T, fx *fixture, text string) string {
	t.Helper()
	dispatchOne(t, fx, dmUpdate(1, 42, text))
	eventually(t, "the turn to submit", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	sess := fx.rt.firstSession()
	eventually(t, "the pump to subscribe", func() bool { return fx.rt.subscribed(sess) })
	return sess
}

// sentTexts collects the sendMessage texts.
func sentTexts(fx *fixture) []string {
	var out []string
	for _, c := range fx.bot.callsOf("sendMessage") {
		out = append(out, c.Text)
	}
	return out
}

// TestStreamingPreviewCadenceAndFinal pins the delivery pump (design D6):
// the first push sends a preview message, later pushes edit it on the
// EditInterval cadence, and the assistant_message finalizes (an edit when
// the preview differs).
func TestStreamingPreviewCadenceAndFinal(t *testing.T) {
	fx := newFixture(t)
	sess := startTurn(t, fx, "write me a poem")

	// The pump pushes on events, gated by EditInterval: the first push
	// lands on the first delta after the interval elapses.
	fx.rt.emit(sess, fx.rt.delta(sess, "roses are red, "))
	time.Sleep(60 * time.Millisecond)
	fx.rt.emit(sess, fx.rt.delta(sess, "violets are blue"))
	eventually(t, "the preview to send", func() bool { return len(fx.bot.callsOf("sendMessage")) == 1 })

	time.Sleep(60 * time.Millisecond)
	fx.rt.emit(sess, fx.rt.delta(sess, " — and so on"))
	eventually(t, "the preview edit", func() bool { return len(fx.bot.callsOf("editMessageText")) == 1 })

	// The final differs from the streamed preview: it lands as the final
	// edit on the preview message.
	fx.rt.emit(sess, fx.rt.assistant(sess, "roses are red, violets are blue — the final version"))
	eventually(t, "the final edit", func() bool { return len(fx.bot.callsOf("editMessageText")) == 2 })

	edits := fx.bot.callsOf("editMessageText")
	require.NotZero(t, edits[0].MessageID, "interim edits target the preview message")
	require.Equal(t, edits[0].MessageID, edits[1].MessageID, "the final edit targets the same preview")
	require.Contains(t, edits[1].Text, "final version")
}

// TestFinalFreshAfterLongStream pins the staleness rule (design D6): past
// FreshFinalAfter the final goes out as a fresh message instead of editing.
func TestFinalFreshAfterLongStream(t *testing.T) {
	fx := newFixture(t)
	sess := startTurn(t, fx, "think long")

	fx.rt.emit(sess, fx.rt.delta(sess, "partial "))
	time.Sleep(60 * time.Millisecond)
	fx.rt.emit(sess, fx.rt.delta(sess, "thoughts"))
	eventually(t, "the preview to send", func() bool { return len(fx.bot.callsOf("sendMessage")) == 1 })

	// Outlive FreshFinalAfter (shortened in tests), then finalize.
	time.Sleep(200 * time.Millisecond)
	fx.rt.emit(sess, fx.rt.assistant(sess, "the complete answer after a long stream"))
	eventually(t, "the fresh final", func() bool { return len(fx.bot.callsOf("sendMessage")) == 2 })

	require.Empty(t, fx.bot.callsOf("editMessageText"), "a stale stream never edits")
	require.Contains(t, sentTexts(fx)[1], "complete answer")
}

// TestTurnErrorAndCancelNotices pins the short notices (design D6).
func TestTurnErrorAndCancelNotices(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		fx := newFixture(t)
		sess := startTurn(t, fx, "doomed")
		fx.rt.emit(sess, fx.rt.turnError(sess, "provider exploded"))

		eventually(t, "the error notice", func() bool {
			for _, c := range fx.bot.callsOf("sendMessage") {
				if strings.Contains(c.Text, "Turn failed") && strings.Contains(c.Text, "provider exploded") {
					return true
				}
			}
			return false
		})
	})
	t.Run("cancelled", func(t *testing.T) {
		fx := newFixture(t)
		sess := startTurn(t, fx, "doomed")
		fx.rt.emit(sess, fx.rt.cancelled(sess))

		eventually(t, "the cancel notice", func() bool {
			for _, c := range fx.bot.callsOf("sendMessage") {
				if strings.Contains(c.Text, "Turn cancelled") {
					return true
				}
			}
			return false
		})
	})
}

// TestInitiatorSilence pins the initiator rule (design D6): events of turns
// this channel did not submit deliver nothing.
func TestInitiatorSilence(t *testing.T) {
	fx := newFixture(t)
	sess := startTurn(t, fx, "ours")

	// A foreign turn's events flow through the same subscription.
	fx.rt.emit(sess, sessions.Event{TurnID: "turn-foreign", Kind: sessions.KindAssistantMessage, Text: "a user-origin answer"})
	time.Sleep(80 * time.Millisecond)
	require.Empty(t, fx.bot.callsOf("sendMessage"), "foreign-turn events are silent")
	require.Empty(t, fx.bot.callsOf("editMessageText"))

	// Our own turn still delivers.
	fx.rt.emit(sess, fx.rt.assistant(sess, "our answer"))
	eventually(t, "our final to deliver", func() bool { return len(fx.bot.callsOf("sendMessage")) == 1 })
}

// TestUnsubscribeAtTurnEnd pins the release rule: the pump closes its
// subscription when the turn ends.
func TestUnsubscribeAtTurnEnd(t *testing.T) {
	fx := newFixture(t)
	sess := startTurn(t, fx, "hello")

	fx.rt.emit(sess, fx.rt.assistant(sess, "done"))
	fx.rt.emit(sess, fx.rt.completed(sess))
	fx.rt.endTurn(sess)

	eventually(t, "the subscription to close", func() bool {
		sub := fx.rt.openSub(sess)
		return sub != nil && sub.isClosed()
	})
}

// TestParseFallbackOnFinalSend pins the MarkdownV2 → plain fallback (design
// D7): a parse-kind rejection resends stripped plain text.
func TestParseFallbackOnFinalSend(t *testing.T) {
	fx := newFixture(t)
	fx.bot.errSeq["sendMessage"] = []error{
		&botapi.Error{Kind: botapi.KindParse, Description: "can't parse entities"},
	}
	sess := startTurn(t, fx, "format this")

	fx.rt.emit(sess, fx.rt.assistant(sess, "hello *world* and _more_"))
	eventually(t, "the plain resend", func() bool { return len(fx.bot.callsOf("sendMessage")) == 2 })

	sends := fx.bot.callsOf("sendMessage")
	require.Equal(t, "MarkdownV2", sends[0].ParseMode)
	require.Empty(t, sends[1].ParseMode, "the fallback drops the parse mode")
	require.NotEqual(t, sends[0].Text, sends[1].Text, "the fallback sends stripped text")
}

// TestEditParseFallback pins the same fallback on the edit path.
func TestEditParseFallback(t *testing.T) {
	fx := newFixture(t)
	fx.bot.errSeq["editMessageText"] = []error{
		&botapi.Error{Kind: botapi.KindParse, Description: "can't parse entities"},
	}
	sess := startTurn(t, fx, "stream this")

	fx.rt.emit(sess, fx.rt.delta(sess, "growing "))
	time.Sleep(60 * time.Millisecond)
	fx.rt.emit(sess, fx.rt.delta(sess, "text — the first push is a fresh send"))
	eventually(t, "the preview", func() bool { return len(fx.bot.callsOf("sendMessage")) == 1 })
	time.Sleep(60 * time.Millisecond)
	fx.rt.emit(sess, fx.rt.delta(sess, " with *bold* inside"))
	eventually(t, "the plain edit fallback", func() bool { return len(fx.bot.callsOf("editMessageText")) == 2 })

	edits := fx.bot.callsOf("editMessageText")
	require.Equal(t, "MarkdownV2", edits[0].ParseMode)
	require.Empty(t, edits[1].ParseMode, "the fallback drops the parse mode on edits")
}

// TestTypingWhileTurnRuns pins task 4.5: the typing indicator refreshes while
// the turn runs and stops at finalize.
func TestTypingWhileTurnRuns(t *testing.T) {
	fx := newFixture(t)
	sess := startTurn(t, fx, "hello")

	eventually(t, "typing actions to flow", func() bool { return len(fx.bot.callsOf("sendChatAction")) >= 2 })

	before := len(fx.bot.callsOf("sendChatAction"))
	fx.rt.emit(sess, fx.rt.assistant(sess, "done"))
	fx.rt.emit(sess, fx.rt.completed(sess))
	fx.rt.endTurn(sess)
	eventually(t, "the pump to exit", func() bool {
		sub := fx.rt.openSub(sess)
		return sub != nil && sub.isClosed()
	})
	time.Sleep(100 * time.Millisecond)
	after := len(fx.bot.callsOf("sendChatAction"))
	require.LessOrEqual(t, after, before+1, "typing stops at finalize")
}
