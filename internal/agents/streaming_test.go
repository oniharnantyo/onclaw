package agents

import (
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/stretchr/testify/require"
)

// TestManager_TextDeltasStreamLive covers 4.3's incremental delivery: while
// the model streams, a connected subscriber receives text deltas as they are
// produced, and the terminal events ride the same live stream.
func TestManager_TextDeltasStreamLive(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "one two three four", promptTokens: 10, completionTokens: 10}
	})
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hi")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	live := collector.Wait()

	// Deltas arrived incrementally: one per streamed word, in order.
	deltas := eventsOfKind(live, sessions.KindTextDelta)
	require.Len(t, deltas, 4)
	require.Equal(t, []string{"one ", "two ", "three ", "four"},
		[]string{deltas[0].Text, deltas[1].Text, deltas[2].Text, deltas[3].Text})
	for _, delta := range deltas {
		require.Equal(t, turn.TurnID(), delta.TurnID)
		require.Empty(t, delta.ID, "deltas are live-only and never persisted")
	}

	kinds := kindsOf(live)
	require.Contains(t, kinds, string(sessions.KindAssistantMessage))
	require.Contains(t, kinds, string(sessions.KindTurnCompleted))
}

// TestManager_DisconnectDoesNotLoseTurn is 4.3's acceptance test: a
// subscriber that disconnects mid-turn does not affect the turn — it runs to
// completion, persists every event, and a fresh replay from the store
// reconstructs the complete turn.
func TestManager_DisconnectDoesNotLoseTurn(t *testing.T) {
	// The fake model pauses after its first stream chunk until released, so
	// the disconnect below lands mid-turn deterministically.
	gate := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "before after", promptTokens: 10, completionTokens: 4}
	})
	f.model.midStream = func() { <-gate }

	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)

	// The first delta proves the turn is streaming; the subscriber now goes
	// away, before the model produced the rest.
	collector.waitDelta(t, 1)
	collector.cancel() // disconnect: stop consuming immediately
	close(gate)        // let the turn finish unwatched

	require.NoError(t, turn.Wait(), "the turn must finish despite the disconnected subscriber")

	// Replay from the store: the complete turn is present, deltas and all.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(events))
	require.Equal(t, "before after", events[1].Text)
	require.NotNil(t, events[2].Usage)
	require.Equal(t, 14, events[2].Usage.TotalTokens)
}

// TestManager_SubscribeIdle covers subscribing while no turn runs: the
// subscription simply waits and then receives the next turn's events.
func TestManager_SubscribeIdle(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "x", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	collector := collect(t, f.mgr, sess.ID)
	turn, err := f.mgr.Send(t.Context(), sess.ID, "go")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	live := collector.Wait()
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindTextDelta),
		string(sessions.KindAssistantMessage),
		string(sessions.KindTurnCompleted),
	}, kindsOf(live))
}

// TestManager_TurnEventsCarryIDs covers the durable/live identity contract:
// persisted events carry their store id and sequence on the live stream too,
// so a reconnecting UI can correlate the replay with what it saw.
func TestManager_TurnEventsCarryIDs(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "hi", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "yo")
	require.NoError(t, err)
	require.NoError(t, turn.Wait())

	live := collector.Wait()
	persisted := eventsOfKind(live, sessions.KindAssistantMessage)
	require.Len(t, persisted, 1)
	require.NotEmpty(t, persisted[0].ID, "the assistant event's live form carries its store id")

	stored, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	storedAssistant := eventsOfKind(stored, sessions.KindAssistantMessage)
	require.Len(t, storedAssistant, 1)
	require.Equal(t, persisted[0].ID, storedAssistant[0].ID)
	require.Positive(t, storedAssistant[0].Seq, "the replay carries the store sequence")
}
