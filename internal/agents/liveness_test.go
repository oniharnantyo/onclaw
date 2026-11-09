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

	"github.com/oniharnantyo/buddies/internal/agents/subagents"
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
// delivers the ONE composite in-flight snapshot (enhance-resumable-streams
// design D2): the streamed text partial, the running tools, and the turn's
// delta count ride a single live-only KindStreamSync event to the new
// subscriber — the old synthetic text-delta/tool-activity pair is gone — and
// it is never persisted to the store.
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

	// Simulate streaming text and a running tool announced in-flight: one
	// prior delta emission, then the partial it produced.
	rt.addDelta()
	rt.appendPartial("partial text so far")
	rt.addTool("call-123", "bash", `{"cmd":"ls"}`)

	// Subscribe mid-turn
	sub, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub.Close()

	// Exactly one catch-up event arrives: the composite snapshot. The turn's
	// own user_message broadcast may race the subscribe around it, so the
	// window counts snapshot-shaped events only — the old synthetic
	// text-delta/tool-activity pair would show up here as more.
	var snap sessions.Event
	snapshots := 0
	deadline := time.After(100 * time.Millisecond)
collect:
	for {
		select {
		case ev, ok := <-sub.Events():
			require.True(t, ok)
			if strings.HasPrefix(ev.ID, "snap-") {
				snapshots++
				if snapshots == 1 {
					snap = ev
				}
			}
		case <-deadline:
			break collect
		}
	}
	require.Equal(t, 1, snapshots, "the catch-up must be exactly one composite event")
	require.Equal(t, sessions.KindStreamSync, snap.Kind)
	require.Equal(t, turn.TurnID(), snap.TurnID)
	require.True(t, strings.HasPrefix(snap.ID, fmt.Sprintf("snap-%s-sync-", turn.TurnID())),
		"snapshot id %q must carry the turn-scoped sync prefix", snap.ID)
	require.NotNil(t, snap.Stream)
	require.Equal(t, "partial text so far", snap.Stream.Text)
	require.Equal(t, int64(1), snap.Stream.Deltas)
	require.Equal(t, []sessions.ToolActivity{{
		CallID:    "call-123",
		Name:      "bash",
		Arguments: `{"cmd":"ls"}`,
		Status:    sessions.ToolStatusRunning,
	}}, snap.Stream.Tools)
	require.Empty(t, snap.Stream.Tasks)

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

// waitEvent reads the subscription until one event of the kind arrives.
func waitEvent(t *testing.T, sub Subscription, kind sessions.Kind) sessions.Event {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-sub.Events():
			require.True(t, ok)
			if ev.Kind == kind {
				return ev
			}
		case <-timeout:
			t.Fatalf("timed out waiting for a %s event", kind)
		}
	}
}

// TestStream_TapFeedsGrowingReasoningPartial asserts the reasoning partial
// mirrors the text one (enhance-resumable-streams design D3): a turn
// streaming reasoning feeds the in-flight builder through the drain's tap,
// the partial grows with each delta, the catch-up snapshot reconstructs it,
// and the durable reasoning record — the builder's per-call terminal —
// resets it without changing what persists.
func TestStream_TapFeedsGrowingReasoningPartial(t *testing.T) {
	gate := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{reasoning: "pondering", promptTokens: 1, completionTokens: 1}
	})
	f.model.midStream = func() { <-gate }
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "think")
	require.NoError(t, err)

	mgr := f.mgr
	snapshotReasoning := func() (string, bool) {
		// The builder's contract: the caller holds m.mu (the record and its
		// registry are map state).
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		rt := mgr.running[sess.ID]
		if rt == nil {
			return "", false
		}
		ev := mgr.streamSyncEvent(rt)
		if ev.Stream == nil {
			return "", false
		}
		return ev.Stream.Reasoning, true
	}

	// The streamed reasoning delta lands on the in-flight builder.
	require.Eventually(t, func() bool {
		r, ok := snapshotReasoning()
		return ok && r == "pondering"
	}, 2*time.Second, 5*time.Millisecond, "the drain's tap must feed the reasoning partial")

	// Growth: a further reasoning delta accumulates onto the partial.
	mgr.mu.Lock()
	rt := mgr.running[sess.ID]
	require.NotNil(t, rt)
	rt.appendReasoning(" deeper")
	r, ok := func() (string, bool) {
		ev := mgr.streamSyncEvent(rt)
		if ev.Stream == nil {
			return "", false
		}
		return ev.Stream.Reasoning, true
	}()
	mgr.mu.Unlock()
	require.True(t, ok)
	require.Equal(t, "pondering deeper", r)

	// A mid-turn subscriber reconstructs the reasoning from the catch-up.
	sub, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	snap := waitEvent(t, sub, sessions.KindStreamSync)
	require.Equal(t, "pondering deeper", snap.Stream.Reasoning)
	sub.Close()

	// The durable reasoning record is the terminal: the turn completes with
	// the model's own reasoning in the log.
	close(gate)
	require.NoError(t, turn.Wait())
	persisted, err := f.sessions.Events(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Equal(t, "pondering", textOf(persisted, sessions.KindReasoning))
}

// TestManager_Subscribe_CatchUpCarriesRunningTasks asserts the running
// delegated tasks ride the catch-up snapshot (enhance-resumable-streams
// design D2/D3): the turn's record reaches the session's task registry and a
// registered task reconstructs from the composite with its description,
// running status, and transcript path; after it finishes, nothing running
// remains.
func TestManager_Subscribe_CatchUpCarriesRunningTasks(t *testing.T) {
	blocked := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		<-blocked
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "delegate work")
	require.NoError(t, err)

	mgr := f.mgr
	// runTurn installs the session's task registry on the record before the
	// model call runs; wait for that from the turn goroutine. The field is
	// map state — read it under m.mu.
	var reg *subagents.TaskRegistry
	require.Eventually(t, func() bool {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		rt := mgr.running[sess.ID]
		if rt == nil || rt.tasks == nil {
			return false
		}
		reg = rt.tasks
		return true
	}, 2*time.Second, 5*time.Millisecond, "the turn's record must carry the task registry")
	reg.RegisterTask("task-1", "sweep the workspace\nsecond line ignored", ".tasks/call-1.output", "", turn.TurnID(), true)

	sub, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	snap := waitEvent(t, sub, sessions.KindStreamSync)
	require.Equal(t, []sessions.TaskActivity{{
		TaskID:         "task-1",
		Description:    "sweep the workspace",
		Status:         sessions.ToolStatusRunning,
		TranscriptPath: ".tasks/call-1.output",
		Background:     true,
	}}, snap.Stream.Tasks)
	sub.Close()

	// The finished task leaves the in-flight state.
	reg.Finish("task-1", sessions.ToolStatusCompleted, "all done")
	require.Empty(t, reg.RunningTasks())

	close(blocked)
	require.NoError(t, turn.Wait())
}

// TestManager_SnapshotIDs_UniquePerEmission asserts every composite snapshot
// emission mints a fresh synthetic id (enhance-resumable-streams design D2):
// two catch-ups and two resyncs for one turn arrive with distinct ids, so a
// client's seenEventIDs dedupe cannot swallow a re-fire.
func TestManager_SnapshotIDs_UniquePerEmission(t *testing.T) {
	blocked := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		<-blocked
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	turn, err := f.mgr.Send(t.Context(), sess.ID, "stream")
	require.NoError(t, err)
	mgr := f.mgr
	mgr.mu.Lock()
	rt := mgr.running[sess.ID]
	mgr.mu.Unlock()
	require.NotNil(t, rt)
	rt.appendPartial("in flight")

	sub1, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub1.Close()
	sub2, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub2.Close()

	catchUp1 := waitEvent(t, sub1, sessions.KindStreamSync)
	catchUp2 := waitEvent(t, sub2, sessions.KindStreamSync)
	require.NoError(t, f.mgr.Resync(t.Context(), sess.ID))
	resync1 := waitEvent(t, sub1, sessions.KindStreamSync)
	resync2 := waitEvent(t, sub2, sessions.KindStreamSync)

	// One emission, one id: the two catch-ups differ, the single resync
	// broadcast delivers the SAME id to both subscribers, and all three
	// emissions are distinct (a shared id would let a client's seenEventIDs
	// dedupe swallow the re-fire).
	ids := []string{catchUp1.ID, catchUp2.ID, resync1.ID}
	for i, id := range ids {
		require.True(t, strings.HasPrefix(id, fmt.Sprintf("snap-%s-sync-", turn.TurnID())),
			"id %q must carry the turn-scoped sync prefix", id)
		for j, other := range ids {
			if i != j {
				require.NotEqual(t, other, id, "snapshot ids must be unique per emission")
			}
		}
	}
	require.Equal(t, resync1.ID, resync2.ID, "one broadcast emission carries one id to every subscriber")

	close(blocked)
	require.NoError(t, turn.Wait())
}

// TestManager_Resync_BroadcastsCompositeToAllSubscribers asserts the resync
// verb (enhance-resumable-streams design D5): every live subscriber of the
// session receives the freshly built composite snapshot, an idle or unknown
// session is a safe no-op, and a terminal turn delivers nothing.
func TestManager_Resync_BroadcastsCompositeToAllSubscribers(t *testing.T) {
	blocked := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		<-blocked
		return fakeReply{text: "done", promptTokens: 1, completionTokens: 1}
	})
	sess := f.newSession(t)

	// Idle and unknown sessions: safe no-ops.
	require.NoError(t, f.mgr.Resync(t.Context(), sess.ID))
	require.NoError(t, f.mgr.Resync(t.Context(), "missing-session"))

	turn, err := f.mgr.Send(t.Context(), sess.ID, "stream")
	require.NoError(t, err)
	mgr := f.mgr
	mgr.mu.Lock()
	rt := mgr.running[sess.ID]
	mgr.mu.Unlock()
	require.NotNil(t, rt)
	rt.appendPartial("mid-stream")

	sub1, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub1.Close()
	sub2, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub2.Close()

	// Both subscribers get their own catch-up first; consume it.
	catchUp1 := waitEvent(t, sub1, sessions.KindStreamSync)
	catchUp2 := waitEvent(t, sub2, sessions.KindStreamSync)

	require.NoError(t, f.mgr.Resync(t.Context(), sess.ID))
	resync1 := waitEvent(t, sub1, sessions.KindStreamSync)
	resync2 := waitEvent(t, sub2, sessions.KindStreamSync)

	// The resync carries the same composite state to every subscriber; one
	// emission is one id, and the re-fire mints a fresh one (design D2).
	require.Equal(t, catchUp1.Stream, resync1.Stream)
	require.Equal(t, catchUp2.Stream, resync2.Stream)
	require.Equal(t, resync1.Stream, resync2.Stream)
	require.Equal(t, resync1.ID, resync2.ID)
	require.NotEqual(t, catchUp1.ID, resync1.ID)
	require.NotEqual(t, catchUp2.ID, resync2.ID)

	close(blocked)
	require.NoError(t, turn.Wait())

	// Everything buffered predates the terminal (including a user_message
	// broadcast that raced the subscribe); drain it, then the resync is a
	// no-op delivering nothing.
	drainBuffered(sub1)
	drainBuffered(sub2)
	require.NoError(t, f.mgr.Resync(t.Context(), sess.ID))
	select {
	case ev, ok := <-sub1.Events():
		if !ok {
			return
		}
		t.Fatalf("unexpected event after terminal: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

// drainBuffered consumes everything already queued on the subscription. Every
// event in the buffer was sent before the call point — the senders this test
// cares about are quiescent by then.
func drainBuffered(sub Subscription) {
	for {
		select {
		case _, ok := <-sub.Events():
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// TestManager_StreamBeacon_PingsMonotonicStopsAtTerminal asserts the liveness
// beacon (enhance-resumable-streams design D4): while the turn streams, a
// per-turn ticker broadcasts live-only pings carrying the turn's monotonic
// text-delta count; the beacon stops at the turn's terminal — no ping
// follows it.
func TestManager_StreamBeacon_PingsMonotonicStopsAtTerminal(t *testing.T) {
	gate := make(chan struct{})
	f := newFixture(t, func(_ []*schema.Message) fakeReply {
		return fakeReply{text: "done soon", promptTokens: 1, completionTokens: 1}
	})
	f.model.midStream = func() { <-gate }
	f.mgr.beaconInterval = 10 * time.Millisecond
	sess := f.newSession(t)

	sub, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub.Close()

	turn, err := f.mgr.Send(t.Context(), sess.ID, "hello")
	require.NoError(t, err)

	// Pings flow while the turn holds; their counts never regress and they
	// carry the streamed text deltas.
	var lastDeltas int64 = -1
	for pings := 0; pings < 3; pings++ {
		ev := waitEvent(t, sub, sessions.KindStreamPing)
		require.Equal(t, sessions.KindStreamPing, ev.Kind)
		require.Equal(t, turn.TurnID(), ev.TurnID)
		require.GreaterOrEqual(t, ev.Deltas, lastDeltas, "ping counts must be monotonic")
		lastDeltas = ev.Deltas
	}
	require.Greater(t, lastDeltas, int64(0), "streamed deltas must show up in the counts")

	// The terminal stops the beacon: stopBeacon is waited out before the Turn
	// completes, so Wait returning means no further ping can be sent.
	close(gate)
	require.NoError(t, turn.Wait())

	// Everything buffered predates the terminal; drain it, then verify the
	// channel stays quiet for a few beacon intervals.
	drainBuffered(sub)
	select {
	case ev, ok := <-sub.Events():
		if !ok {
			return
		}
		t.Fatalf("event after the turn's terminal: %+v", ev)
	case <-time.After(3 * f.mgr.beaconInterval):
	}
}

// TestManager_StreamBeacon_PingsThroughPark asserts the parked contract
// (enhance-resumable-streams design D4): a turn parked on a pending approval
// stops emitting deltas but not pings — the beacon keeps proving liveness
// with the static count through the park and stops only at the terminal.
func TestManager_StreamBeacon_PingsThroughPark(t *testing.T) {
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
	f.mgr.beaconInterval = 10 * time.Millisecond
	sess, err := f.mgr.CreateSession(t.Context(), f.agent.ID)
	require.NoError(t, err)

	sub, err := f.mgr.Subscribe(t.Context(), sess.ID)
	require.NoError(t, err)
	defer sub.Close()

	turn, pending := parkOnApproval(t, f, sess)
	require.NotEmpty(t, pending.Approval.InterruptID)

	// During the park the pings keep flowing; no delta was emitted, so the
	// counts stay at zero (silence-proof liveness).
	for pings := 0; pings < 3; pings++ {
		ev := waitEvent(t, sub, sessions.KindStreamPing)
		require.Equal(t, turn.TurnID(), ev.TurnID)
		require.Equal(t, int64(0), ev.Deltas)
	}

	// Cancelling the parked turn is a terminal: the beacon stops with it.
	require.NoError(t, f.mgr.Cancel(t.Context(), sess.ID))
	require.ErrorIs(t, turn.Wait(), ErrTurnCancelled)

	drainBuffered(sub)
	select {
	case ev, ok := <-sub.Events():
		if !ok {
			return
		}
		t.Fatalf("event after the parked turn's terminal: %+v", ev)
	case <-time.After(3 * f.mgr.beaconInterval):
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
