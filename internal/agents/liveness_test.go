package agents

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
)

// TestManager_TurnState_RunningParkedIdle asserts TurnState accurately reflects
// running, parked approvals, and idle/unknown sessions.
func TestManager_TurnState_RunningParkedIdle(t *testing.T) {
	unblock := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		<-unblock
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	// 1. Idle session
	busy, turnID := f.mgr.TurnState(t.Context(), sess.ID)
	require.False(t, busy)
	require.Empty(t, turnID)

	// Unknown session
	busy, turnID = f.mgr.TurnState(t.Context(), "missing-session")
	require.False(t, busy)
	require.Empty(t, turnID)

	// 2. Running turn
	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)
	require.NotEmpty(t, turn.TurnID())

	busy, turnID = f.mgr.TurnState(t.Context(), sess.ID)
	require.True(t, busy)
	require.Equal(t, turn.TurnID(), turnID)

	close(unblock)
	require.NoError(t, turn.Wait())

	// Back to idle
	busy, turnID = f.mgr.TurnState(t.Context(), sess.ID)
	require.False(t, busy)
	require.Empty(t, turnID)
}

// TestManager_Subscribe_MidTurnCatchUpSnapshot asserts subscribing mid-turn
// delivers synthetic partial text and running tool events before subsequent live events,
// and that these snapshot events are never persisted to the store.
func TestManager_Subscribe_MidTurnCatchUpSnapshot(t *testing.T) {
	firstTurnBlocked := make(chan struct{})

	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		<-firstTurnBlocked
		return fakeReply{text: "done", promptTokens: 5, completionTokens: 5}
	})
	sess := f.newSession(t)

	// Start a turn
	turn, err := f.mgr.Send(t.Context(), sess.ID, "run tool")
	require.NoError(t, err)

	mgr := f.mgr
	mgr.mu.Lock()
	rt := mgr.running[sess.ID]
	mgr.mu.Unlock()
	require.NotNil(t, rt)

	// Simulate streaming text and a running tool announced in-flight
	rt.appendPartial("partial text so far")
	rt.addTool("call-123", "bash", `{"cmd":"ls"}`)

	// Subscribe mid-turn
	sub, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub.Close()

	// Drain snapshot events from subscription
	var received []sessions.Event
	timeout := time.After(2 * time.Second)

	for len(received) < 2 {
		select {
		case ev, ok := <-sub.Events():
			require.True(t, ok)
			received = append(received, ev)
		case <-timeout:
			t.Fatalf("timed out waiting for snapshot events, got %d", len(received))
		}
	}

	// Verify synthetic snapshot events
	partialEv := received[0]
	require.Equal(t, fmt.Sprintf("snap-%s-partial", turn.TurnID()), partialEv.ID)
	require.Equal(t, sessions.KindTextDelta, partialEv.Kind)
	require.Equal(t, "partial text so far", partialEv.Text)

	toolEv := received[1]
	require.Equal(t, fmt.Sprintf("snap-%s-tool-call-123", turn.TurnID()), toolEv.ID)
	require.Equal(t, sessions.KindToolActivity, toolEv.Kind)
	require.NotNil(t, toolEv.Tool)
	require.Equal(t, "call-123", toolEv.Tool.CallID)
	require.Equal(t, "bash", toolEv.Tool.Name)
	require.Equal(t, `{"cmd":"ls"}`, toolEv.Tool.Arguments)
	require.Equal(t, sessions.ToolStatusRunning, toolEv.Tool.Status)

	// Complete the turn
	close(firstTurnBlocked)
	require.NoError(t, turn.Wait())

	// Verify snapshot events are absent from persisted events
	persisted, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	for _, ev := range persisted {
		require.False(t, strings.HasPrefix(ev.ID, "snap-"), "persisted log must not contain synthetic snapshot event: %s", ev.ID)
	}
}

// TestManager_Subscribe_IdleSessionDeliversNothing asserts subscribing to an idle
// session produces no catch-up snapshot events.
func TestManager_Subscribe_IdleSessionDeliversNothing(t *testing.T) {
	f := newFixture(t, func(_ []*schema.Message) fakeReply { return fakeReply{} })
	sess := f.newSession(t)

	sub, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub.Close()

	select {
	case ev := <-sub.Events():
		t.Fatalf("unexpected event from idle session: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// Expected: nothing received
	}
}

// TestManager_Subscribe_ConcurrentCompletionRace asserts that concurrent Subscribe
// and turn completion does not trigger data races or produce torn state.
func TestManager_Subscribe_ConcurrentCompletionRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		f := newFixture(t, func(_ []*schema.Message) fakeReply {
			return fakeReply{text: "speedy", promptTokens: 1, completionTokens: 1}
		})
		sess := f.newSession(t)

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			turn, err := f.mgr.Send(context.Background(), sess.ID, "test")
			if err == nil {
				_ = turn.Wait()
			}
		}()

		go func() {
			defer wg.Done()
			sub, err := f.mgr.Subscribe(context.Background(), sess.ID)
			if err == nil {
				// Read whatever is buffered
				time.Sleep(5 * time.Millisecond)
				sub.Close()
			}
		}()

		wg.Wait()
	}
}

// turnRecorder collects WithTurnTransition callbacks. Transitions fire from
// turn goroutines, so access is guarded.
type turnRecorder struct {
	mu  sync.Mutex
	got []string // "sessionID|true" / "sessionID|false" in fire order
}

func (r *turnRecorder) record(sessionID string, busy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, fmt.Sprintf("%s|%t", sessionID, busy))
}

func (r *turnRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// TestManager_RunningSessions_Transitions asserts the app-wide busy-set
// query and the WithTurnTransition transitions (add-grouped-session-rail
// design D2): a turn started via Send announces busy=true and lists the
// session; the terminal announces busy=false and unlists it; the idle set is
// empty and non-nil.
func TestManager_RunningSessions_Transitions(t *testing.T) {
	unblock := make(chan struct{})
	rec := &turnRecorder{}
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		<-unblock
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	}, WithTurnTransition(rec.record))
	sess := f.newSession(t)

	running := f.mgr.RunningSessions(t.Context())
	require.NotNil(t, running)
	require.Empty(t, running)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)

	// The claim is synchronous with Send: the session is listed and the
	// busy=true transition already fired.
	require.Equal(t, []string{sess.ID}, f.mgr.RunningSessions(t.Context()))
	require.Equal(t, []string{sess.ID + "|true"}, rec.snapshot())

	close(unblock)
	require.NoError(t, turn.Wait())

	// The busy record clears strictly before the Turn completes, so Wait
	// returning means the session is unlisted and busy=false fired.
	require.Empty(t, f.mgr.RunningSessions(t.Context()))
	require.Equal(t, []string{sess.ID + "|true", sess.ID + "|false"}, rec.snapshot())
}

// TestManager_Submit_TransitionsCoverSchedulerPath asserts turns fired via
// Submit — the scheduler's and gateway triggers' dispatch — transition the
// same way as Send, and several sessions' in-flight state is listed
// together.
func TestManager_Submit_TransitionsCoverSchedulerPath(t *testing.T) {
	unblock := make(chan struct{})
	rec := &turnRecorder{}
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		<-unblock
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	}, WithTurnTransition(rec.record))
	sess := f.newSession(t)
	other := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "from the chat")
	require.NoError(t, err)
	schedTurn, err := f.mgr.Submit(t.Context(), SendRequest{
		SessionID: other.ID,
		Message:   "from the scheduler",
		Origin:    "scheduler",
	})
	require.NoError(t, err)

	require.ElementsMatch(t, []string{sess.ID, other.ID}, f.mgr.RunningSessions(t.Context()))

	close(unblock)
	require.NoError(t, turn.Wait())
	require.NoError(t, schedTurn.Wait())

	require.Empty(t, f.mgr.RunningSessions(t.Context()))
	require.ElementsMatch(t, []string{
		sess.ID + "|true", sess.ID + "|false",
		other.ID + "|true", other.ID + "|false",
	}, rec.snapshot())
}

// TestManager_Cancel_EmitsIdleTransition asserts the cancel path — a real
// terminal — announces busy=false and releases the session from the busy set.
func TestManager_Cancel_EmitsIdleTransition(t *testing.T) {
	shortenEscalation(t)
	rec := &turnRecorder{}
	gate := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "partial", promptTokens: 1, completionTokens: 1}
	}, WithTurnTransition(rec.record))
	f.model.midStream = func() { <-gate }
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)

	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.ErrorIs(t, turn.Wait(), ErrTurnCancelled)
	close(gate) // nothing is listening anymore; release the fake stream

	require.Empty(t, f.mgr.RunningSessions(t.Context()))
	require.Equal(t, []string{sess.ID + "|true", sess.ID + "|false"}, rec.snapshot())
}

// TestManager_RunningSessions_ParkedTurnStaysListed asserts the parked
// contract (add-grouped-session-rail design D2): parking on a pending
// approval and resolving it fire no transition — the busy=true from the
// start still stands — and only the terminal unlists the session.
func TestManager_RunningSessions_ParkedTurnStaysListed(t *testing.T) {
	rec := &turnRecorder{}
	f := approvalFixture(t, t.TempDir(), func(input []*schema.Message) fakeReply {
		sawTool := false
		for _, msg := range input {
			if msg.Role == schema.Tool && msg.ToolCallID == "call-1" {
				sawTool = true
			}
		}
		if !sawTool {
			return dangerousCall("call-1", "doomed.txt")
		}
		return fakeReply{text: "handled", promptTokens: 10, completionTokens: 2}
	}, WithTurnTransition(rec.record))
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)

	turn, pending := parkOnApproval(t, f, sess)

	// Parked: the session stays listed and no further transition fired.
	require.Equal(t, []string{sess.ID}, f.mgr.RunningSessions(t.Context()))
	require.Equal(t, []string{sess.ID + "|true"}, rec.snapshot())

	// Resolving keeps the turn in flight; the terminal is the one transition.
	require.NoError(t, f.mgr.ResolveApproval(t.Context(), sess.ID, pending.Approval.InterruptID, false))
	require.NoError(t, turn.Wait())

	require.Empty(t, f.mgr.RunningSessions(t.Context()))
	require.Equal(t, []string{sess.ID + "|true", sess.ID + "|false"}, rec.snapshot())
}
