package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

// dmInlet is the inlet a dmUpdate(…, 42, …) produces.
func dmInlet() inlet {
	return inlet{chatID: 42, kind: store.ChannelChatKindDM, title: "Alice", senderID: 42, senderName: "Alice"}
}

func item(text string) queuedItem {
	return queuedItem{inlet: dmInlet(), text: text}
}

// TestFirstMessageCreatesAndPinsSession pins design D5: the first message
// creates the session, titles it "<chat> — Telegram", pins the binding, and
// submits with OriginTelegram.
func TestFirstMessageCreatesAndPinsSession(t *testing.T) {
	fx := newFixture(t)

	dispatchOne(t, fx, dmUpdate(1, 42, "hello"))
	eventually(t, "the turn to submit", func() bool { return len(fx.rt.submittedReqs()) == 1 })

	reqs := fx.rt.submittedReqs()
	require.Equal(t, hooks.OriginTelegram, reqs[0].Origin)
	sess := fx.rt.firstSession()
	require.Equal(t, sess, reqs[0].SessionID)
	require.Equal(t, "agent-default", fx.SessionAgent(t, sess), "the gateway default agent resolves first")

	chat, err := fx.st.Channels().GetChannelChat(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, store.ChannelChatKindDM, chat.Kind)
	require.Equal(t, sess, chat.SessionID, "the binding is pinned restart-safe")

	row, err := fx.st.Sessions().ByID(context.Background(), sess)
	require.NoError(t, err)
	require.Equal(t, "Alice (@alice) — Telegram", row.Title)
}

// TestSecondMessageReusesSession pins the persistent mapping: later messages
// submit to the same session without recreating it.
func TestSecondMessageReusesSession(t *testing.T) {
	fx := newFixture(t)

	dispatchOne(t, fx, dmUpdate(1, 42, "first"))
	eventually(t, "the first turn", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	fx.rt.endTurn(fx.rt.firstSession()) // let the turn finish
	eventually(t, "the pump to release", func() bool { return fx.svc.chats[42].idle() })

	dispatchOne(t, fx, dmUpdate(2, 42, "second"))
	eventually(t, "the second turn", func() bool { return len(fx.rt.submittedReqs()) == 2 })

	reqs := fx.rt.submittedReqs()
	require.Equal(t, reqs[0].SessionID, reqs[1].SessionID, "the session persists across messages")
	require.Equal(t, 1, fx.rt.sessionCount(), "no second session is created")
}

// TestNewClearsBinding pins /new: the binding clears and the next message
// opens a fresh session; the old session stays in the rail.
func TestNewClearsBinding(t *testing.T) {
	fx := newFixture(t)

	dispatchOne(t, fx, dmUpdate(1, 42, "hello"))
	eventually(t, "the first turn", func() bool { return len(fx.rt.submittedReqs()) == 1 })

	fx.svc.handleMessage(context.Background(), dmUpdate(2, 42, "/new").Message)
	time.Sleep(60 * time.Millisecond)
	chat, err := fx.st.Channels().GetChannelChat(context.Background(), 42)
	require.NoError(t, err)
	require.Empty(t, chat.SessionID, "/new clears the binding")
	require.Empty(t, fx.rt.cancels, "/new does not cancel")

	dispatchOne(t, fx, dmUpdate(3, 42, "fresh start"))
	eventually(t, "a second session to be created", func() bool { return fx.rt.sessionCount() == 2 })
}

// TestAgentResolutionBranches pins the per-chat resolution order (design D7):
// pin → gateway default → chat-visible error, no turn.
func TestAgentResolutionBranches(t *testing.T) {
	t.Run("pinned agent wins", func(t *testing.T) {
		fx := newFixture(t)
		pin := "agent-x"
		require.NoError(t, fx.st.Channels().SaveChannelChat(context.Background(), store.ChannelChat{
			ChatID: 42, Kind: store.ChannelChatKindDM, PinnedAgentID: &pin,
		}))

		dispatchOne(t, fx, dmUpdate(1, 42, "hello"))
		eventually(t, "the turn", func() bool { return len(fx.rt.submittedReqs()) == 1 })
		require.Equal(t, "agent-x", fx.SessionAgent(t, fx.rt.firstSession()))
	})

	t.Run("no pin and no default errors to the chat", func(t *testing.T) {
		fx := newFixture(t)
		require.NoError(t, fx.st.GatewaySettings().Save(context.Background(), &store.GatewaySettings{Enabled: true}))

		dispatchOne(t, fx, dmUpdate(1, 42, "hello"))
		eventually(t, "the chat notice", func() bool {
			return len(fx.bot.callsOf("sendMessage")) > 0
		})
		require.Contains(t, fx.bot.callsOf("sendMessage")[0].Text, "no agent")
		require.Empty(t, fx.rt.submittedReqs(), "no turn starts without an agent")
	})
}

// TestQueueDrainsInOrderOnTurnEnd pins the per-chat FIFO (design D5): a
// message admitted while the channel's turn runs queues up, and the pump's
// terminal event dequeues it.
func TestQueueDrainsInOrderOnTurnEnd(t *testing.T) {
	fx := newFixture(t)

	dispatchOne(t, fx, dmUpdate(1, 42, "m1"))
	eventually(t, "the first turn", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	sess := fx.rt.firstSession()

	// The channel's turn is still running: m2 queues.
	dispatchOne(t, fx, dmUpdate(2, 42, "m2"))
	time.Sleep(60 * time.Millisecond)
	require.Len(t, fx.rt.submittedReqs(), 1, "m2 must queue behind the running turn")

	// The pump's terminal event: final answer, completion, turn end.
	fx.rt.emit(sess, fx.rt.assistant(sess, "answer"))
	fx.rt.emit(sess, fx.rt.completed(sess))
	fx.rt.endTurn(sess)

	eventually(t, "the queue to drain", func() bool { return len(fx.rt.submittedReqs()) == 2 })
	reqs := fx.rt.submittedReqs()
	require.Equal(t, "m1", reqs[0].Message)
	require.Equal(t, "m2", reqs[1].Message, "FIFO order")
}

// TestForeignBusyQueuesThenDrains pins the foreign-turn path: a session busy
// with a turn this channel did not start queues the message and drains when
// the session frees.
func TestForeignBusyQueuesThenDrains(t *testing.T) {
	fx := newFixture(t)
	pin := "sess-foreign"
	require.NoError(t, fx.st.Channels().SaveChannelChat(context.Background(), store.ChannelChat{
		ChatID: 42, Kind: store.ChannelChatKindDM, Title: "Alice", SessionID: pin,
	}))
	// The pinned session exists in the store (a UI turn is running on it).
	require.NoError(t, fx.st.Sessions().Create(context.Background(), &store.Session{ID: pin, AgentID: "agent-default"}))
	fx.rt.mu.Lock()
	fx.rt.busy[pin] = true // a UI-origin turn owns the session
	fx.rt.mu.Unlock()

	dispatchOne(t, fx, dmUpdate(1, 42, "while busy"))
	time.Sleep(60 * time.Millisecond)
	require.Empty(t, fx.rt.submittedReqs(), "a foreign-busy session defers the message")

	fx.rt.endTurn(pin)
	eventually(t, "the queued message to submit after the foreign turn", func() bool {
		return len(fx.rt.submittedReqs()) == 1
	})
	require.Equal(t, "while busy", fx.rt.submittedReqs()[0].Message)
}

// TestDebounceMergesParts pins the ~600ms inbound debounce (design D5):
// multi-part pastes merge into one dispatch.
func TestDebounceMergesParts(t *testing.T) {
	fx := newFixture(t)

	dispatchOne(t, fx, dmUpdate(1, 42, "part one"))
	fx.svc.handleMessage(context.Background(), dmUpdate(2, 42, "part two").Message)

	eventually(t, "the merged dispatch", func() bool { return len(fx.rt.submittedReqs()) == 1 })
	require.Equal(t, "part one\npart two", fx.rt.submittedReqs()[0].Message)
}

// TestQueueCapDropsOldestWithNotice pins the bounded queue: over the cap the
// oldest queued message drops with a chat notice. The first item starts the
// chat's turn; the remaining 65 must fit 64 queue slots, dropping the oldest.
func TestQueueCapDropsOldestWithNotice(t *testing.T) {
	fx := newFixture(t)

	for i := 0; i < ChatQueueCap+2; i++ {
		fx.svc.dispatchItem(context.Background(), item(strings.Repeat("x", i+1)))
	}

	fx.svc.chatsMu.Lock()
	cs := fx.svc.chats[42]
	fx.svc.chatsMu.Unlock()
	cs.mu.Lock()
	qlen := len(cs.queue)
	head := ""
	if qlen > 0 {
		head = cs.queue[0].text
	}
	cs.mu.Unlock()
	require.Equal(t, ChatQueueCap, qlen, "the queue stays bounded")
	require.Len(t, head, 3, "the first queued message dropped: the head is the second item")

	eventually(t, "the drop notice", func() bool {
		for _, c := range fx.bot.callsOf("sendMessage") {
			if strings.Contains(c.Text, "oldest queued message") {
				return true
			}
		}
		return false
	})
}

// idle reports a chat with no initiator turn.
func (cs *chatState) idle() bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.turnID == ""
}
