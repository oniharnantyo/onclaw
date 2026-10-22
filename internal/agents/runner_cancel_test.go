package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The turn-cancellation tests (tasks.md 3.1/3.2, spec agent-runtime "Turn
// cancellation"): a bound cancel stops the in-flight run, the terminal
// turn_cancelled event persists after the turn's context died (the partial
// output retained), the run_finished observer sees the cancelled status,
// cancelling with no turn in flight is a safe no-op, and a second cancel is
// a no-op.

// shortenEscalation replaces the cancel escalation delay for the test and
// restores it on cleanup.
func shortenEscalation(t *testing.T) {
	t.Helper()
	prev := cancelEscalationDelay
	cancelEscalationDelay = 100 * time.Millisecond
	t.Cleanup(func() { cancelEscalationDelay = prev })
}

// TestCancel_StopsInFlightTurn pins the mid-stream cancel: streaming stops,
// the partial text is retained on the persisted turn_cancelled, the Turn
// finishes cancelled, and the session accepts a new send immediately.
func TestCancel_StopsInFlightTurn(t *testing.T) {
	shortenEscalation(t)
	gate := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "one two three", promptTokens: 5, completionTokens: 1}
	})
	// The fake stream pauses after its first chunk, like a provider stream
	// mid-delivery; the cancel must tear the turn down from there.
	f.model.midStream = func() { <-gate }
	sess := f.newSession(t)
	collector := collect(t, f.mgr, sess.ID)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	collector.waitDelta(t, 1)

	// The session is busy while the turn streams.
	_, err = f.mgr.Send(t.Context(), sess.ID, "queued?")
	require.ErrorIs(t, err, ErrTurnInProgress)

	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.ErrorIs(t, turn.Wait(), ErrTurnCancelled, "the cancelled turn's outcome")
	close(gate) // nothing is listening anymore; release the fake stream

	// The terminal cancelled event persisted after the turn's context died,
	// carrying the partial output of the interrupted model call.
	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{
		string(sessions.KindUserMessage),
		string(sessions.KindTurnCancelled),
	}, kindsOf(events))
	require.Equal(t, "one ", events[1].Text, "the partial output is retained")
	require.NotNil(t, events[1].Usage)
	require.Equal(t, events[0].TurnID, events[1].TurnID)

	// The session is released: the next send dispatches normally.
	second, err := f.mgr.Send(t.Context(), sess.ID, "after the cancel")
	require.NoError(t, err)
	require.NoError(t, second.Wait())

	// The live stream saw the terminal cancelled event too (the collector
	// stayed attached through the second send, so filter to this turn).
	live := collector.Wait()
	var sawCancelled bool
	for _, ev := range live {
		if ev.Kind == sessions.KindTurnCancelled && ev.TurnID == events[1].TurnID {
			sawCancelled = true
		}
	}
	require.True(t, sawCancelled, "the terminal cancelled event rides the live stream")
}

// TestCancel_NoActiveTurnIsSafe pins the safe no-op: cancelling a session
// with no turn in flight — or an unknown session — succeeds without
// touching the transcript.
func TestCancel_NoActiveTurnIsSafe(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "ok"}
	})
	sess := f.newSession(t)

	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.NoError(t, f.mgr.Cancel(t.Context(), "no-such-session"))

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Empty(t, events, "the transcript is untouched")
}

// TestCancel_SecondCancelIsNoOp pins the idempotence: a second cancel
// neither fails nor duplicates the terminal event.
func TestCancel_SecondCancelIsNoOp(t *testing.T) {
	shortenEscalation(t)
	gate := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "one two three"}
	})
	f.model.midStream = func() { <-gate }
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)

	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID), "the second cancel is a safe no-op")
	require.ErrorIs(t, turn.Wait(), ErrTurnCancelled)
	close(gate)

	events, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, 1, len(eventsOfKind(events, sessions.KindTurnCancelled)),
		"exactly one terminal cancelled event")
}

// TestCancel_HooksObserveCancelledStatus pins the observer contract
// (tasks.md 3.2): the run_finished hook delivery carries the cancelled
// status (spec agent-hooks: status completed|failed|cancelled).
func TestCancel_HooksObserveCancelledStatus(t *testing.T) {
	shortenEscalation(t)
	markers := filepath.Join(t.TempDir(), "markers")
	gate := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "one two three"}
	})
	f.model.midStream = func() { <-gate }
	f.wireRealHooks()
	seedHook(t, f.store, store.Hook{
		Name:        "saw-finish",
		Event:       hooks.EventRunFinished,
		HandlerType: hooks.HandlerCommand,
		Config:      `{"command":"grep -o cancelled >> ` + markers + `"}`,
		Enabled:     true,
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.ErrorIs(t, turn.Wait(), ErrTurnCancelled)
	close(gate)

	// The cancelled status reached the observer, detached.
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(markers)
		return err == nil && strings.Contains(string(b), "cancelled")
	}, 5*time.Second, 20*time.Millisecond, "the run_finished hook must observe the cancelled status")

	require.Eventually(t, func() bool {
		for _, exec := range auditedHooks(t, f.store) {
			if exec.Event == hooks.EventRunFinished {
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond, "the run_finished delivery is audited")
}
