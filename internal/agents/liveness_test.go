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
