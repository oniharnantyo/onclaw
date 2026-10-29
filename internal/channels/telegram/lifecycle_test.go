package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/channels/telegram/botapi"
	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// errConflict409 is a getUpdates conflict (another consumer owns the token).
func errConflict409() error {
	return &botapi.Error{Kind: botapi.KindConflict, Description: "Conflict: terminated by other getUpdates request"}
}

// errAuth401 is a dead token.
func errAuth401() error {
	return &botapi.Error{Kind: botapi.KindAuthInvalid, Description: "Unauthorized"}
}

// TestAttachStartsOnRunning pins the gateway lifecycle (design D1): no
// polling before Running, the loop starts on the first Running transition or
// immediately when already Running.
func TestAttachStartsOnRunning(t *testing.T) {
	fx := newFixture(t)

	fx.svc.Attach(fx.gw) // gateway stopped: no loop
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, fx.bot.pollCount(), "no polls while the gateway is stopped")

	fx.gw.transition(gateway.StateRunning)
	eventually(t, "the poll loop to start", func() bool { return fx.bot.pollCount() > 0 })

	fx.svc.Stop()
}

// TestAttachAlreadyRunning pins the subscribe-first + state-check race guard:
// attaching to an already-Running gateway starts the loop immediately.
func TestAttachAlreadyRunning(t *testing.T) {
	fx := newFixture(t)
	fx.gw.transition(gateway.StateRunning)

	fx.svc.Attach(fx.gw)
	eventually(t, "the poll loop to start", func() bool { return fx.bot.pollCount() > 0 })
}

// TestPausedHoldsUpdatesAndResumeDrains pins the hold queue (design D9):
// updates admitted while holding queue up (bounded), and a return to Running
// dispatches them in admission order.
func TestPausedHoldsUpdatesAndResumeDrains(t *testing.T) {
	fx := newFixture(t)
	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	// Poll #1 is the cold-boot sync; wait for the regular loop to run
	// before staging held updates.
	eventually(t, "the regular poll loop to start", func() bool { return fx.bot.pollCount() >= 2 })

	fx.gw.transition(gateway.StatePaused)
	// Two held updates for one chat: the debounce merges them, and the
	// merged text proves the drain order within the chat.
	fx.bot.push(dmUpdate(1, 42, "held one"))
	fx.bot.push(dmUpdate(2, 42, "held two"))
	time.Sleep(80 * time.Millisecond)
	require.Empty(t, fx.rt.submittedReqs(), "held updates must not dispatch while paused")

	fx.gw.transition(gateway.StateRunning)
	eventually(t, "the held updates to drain", func() bool {
		return len(fx.rt.submittedReqs()) == 1
	})
	require.Equal(t, "held one\nheld two", fx.rt.submittedReqs()[0].Message, "in-order drain")
}

// TestStoppedStopsLoopAndRedeliversOnRestart pins Stopped: the loop stops
// (no dispatch while stopped) and a later Running starts a fresh loop.
func TestStoppedStopsLoopAndRedeliversOnRestart(t *testing.T) {
	fx := newFixture(t)
	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the poll loop to start", func() bool { return fx.bot.pollCount() > 0 })

	fx.gw.transition(gateway.StateStopped)
	polls := fx.bot.pollCount()
	time.Sleep(80 * time.Millisecond)
	require.LessOrEqual(t, fx.bot.pollCount(), polls+1, "the poll loop stops on Stopped")

	fx.gw.transition(gateway.StateRunning)
	eventually(t, "the poll loop to restart", func() bool { return fx.bot.pollCount() > polls })
}

// TestStopIdempotentAndBounded pins the bounded teardown (design D1): Stop
// cancels, joins, and is safe to call repeatedly — including before Attach.
func TestStopIdempotentAndBounded(t *testing.T) {
	fx := newFixture(t)
	fx.svc.Stop() // before Attach

	fx.bot.wedge = make(chan struct{}) // a hung long poll
	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the poll loop to wedge into getUpdates", func() bool { return fx.bot.pollCount() > 0 })

	done := make(chan struct{})
	go func() {
		fx.svc.Stop()
		fx.svc.Stop() // idempotent
		close(done)
	}()
	select {
	case <-done:
		// The fake's wedge honors ctx, so the join completes without the
		// bounded wait even firing.
	case <-time.After(StopJoinWait + 2*time.Second):
		t.Fatal("Stop did not return within the bounded teardown window")
	}
	close(fx.bot.wedge)
}

// TestDisabledChannel pins the disabled state (design D3): a disabled row
// emits "disabled" and never polls.
func TestDisabledChannel(t *testing.T) {
	fx := newFixture(t)
	require.NoError(t, fx.st.Channels().SaveChannel(context.Background(), &store.Channel{ID: DefaultChannelID, Enabled: false, BotToken: "tok"}))

	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)

	eventually(t, "the disabled state", func() bool { return fx.svc.currentState() == StateDisabled })
	require.Zero(t, fx.bot.pollCount())
}

// TestConflictIsTerminal pins design D10's conflict rule: 409 stops polling
// with an error state and no retry fight — a later Running does not restart.
func TestConflictIsTerminal(t *testing.T) {
	fx := newFixture(t)
	fx.bot.errFor["getUpdates"] = errConflict409() // permanent: sync and loop both see it

	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the error state", func() bool { return fx.svc.currentState() == StateError })

	polls := fx.bot.pollCount()
	fx.gw.transition(gateway.StateStopped)
	fx.gw.transition(gateway.StateRunning)
	time.Sleep(80 * time.Millisecond)
	require.LessOrEqual(t, fx.bot.pollCount(), polls+1, "a terminal conflict must not restart")
}

// TestAuthInvalidIsTerminal pins the 401 rule: a dead token surfaces the
// error state from the connect probe.
func TestAuthInvalidIsTerminal(t *testing.T) {
	fx := newFixture(t)
	fx.bot.errFor["getMe"] = errAuth401()

	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the error state", func() bool { return fx.svc.currentState() == StateError })
}

// TestReconnectBackoffAndDegraded pins the rebuild behavior (design D10):
// retryable getUpdates failures rebuild with backoff; repeated rebuilds read
// degraded; sustained health returns to connected.
func TestReconnectBackoffAndDegraded(t *testing.T) {
	fx := newFixture(t)
	// The first error hits the cold-boot sync (survivable); the next two
	// are loop rebuilds, which trip the degraded threshold.
	fx.bot.errSeq["getUpdates"] = []error{
		&botapi.Error{Kind: botapi.KindTransient, Description: "boom 1"},
		&botapi.Error{Kind: botapi.KindTransient, Description: "boom 2"},
		&botapi.Error{Kind: botapi.KindTransient, Description: "boom 3"},
	}

	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)

	eventually(t, "the degraded state after repeated rebuilds", func() bool {
		return fx.svc.currentState() == StateDegraded
	})
	// Cold boot + two failing polls.
	require.GreaterOrEqual(t, fx.bot.pollCount(), 3, "the loop rebuilt and kept polling")

	// Two clean heartbeat cycles prove recovery: the rebuild counter
	// resets and the state reads connected again.
	eventually(t, "the state to recover to connected", func() bool {
		return fx.svc.currentState() == StateConnected
	})
}

// TestConfigChangeStartsIdleLoop pins the configure-after-boot recovery: a
// channel row saved after the runtime booted idle (absent, disabled, or
// tokenless at connect) must begin polling on ConfigChanged without an app
// restart.
func TestConfigChangeStartsIdleLoop(t *testing.T) {
	fx := newFixtureWith(t, func(t *testing.T, fx *fixture) {
		require.NoError(t, fx.st.Channels().SaveChannel(context.Background(), &store.Channel{ID: DefaultChannelID, Enabled: false, BotToken: "tok"}))
	})

	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the idle state at boot", func() bool { return fx.svc.currentState() == StateDisabled })
	require.Zero(t, fx.bot.pollCount(), "an idle channel never polls")

	// The operator saves the channel enabled (SaveChannel's write path).
	require.NoError(t, fx.st.Channels().SaveChannel(context.Background(), &store.Channel{ID: DefaultChannelID, Enabled: true, BotToken: "tok"}))
	fx.svc.ConfigChanged()

	eventually(t, "the loop to start after the config kick", func() bool { return fx.bot.pollCount() > 0 })
	eventually(t, "the connected state", func() bool { return fx.svc.currentState() == StateConnected })
}

// TestConfigChangeStopsDisabledLoop pins the toggle-off path: disabling a
// live channel row stops polling at the next config kick.
func TestConfigChangeStopsDisabledLoop(t *testing.T) {
	fx := newFixture(t)
	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the poll loop to start", func() bool { return fx.bot.pollCount() >= 2 })

	require.NoError(t, fx.st.Channels().SaveChannel(context.Background(), &store.Channel{ID: DefaultChannelID, Enabled: false, BotToken: "tok"}))
	fx.svc.ConfigChanged()
	eventually(t, "the disabled state", func() bool { return fx.svc.currentState() == StateDisabled })

	polls := fx.bot.pollCount()
	time.Sleep(80 * time.Millisecond)
	require.LessOrEqual(t, fx.bot.pollCount(), polls+1, "a disabled channel stops polling")
}

// TestConfigChangeRecoversTerminalAuth pins the corrected-token path: an
// explicit config change clears even a terminal failure (design D10's
// no-retry-fight rule governs automatic restarts, not operator action).
func TestConfigChangeRecoversTerminalAuth(t *testing.T) {
	fx := newFixture(t)
	fx.bot.errFor["getMe"] = errAuth401()

	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the terminal error state", func() bool { return fx.svc.currentState() == StateError })

	// The operator pastes a working token.
	fx.bot.mu.Lock()
	delete(fx.bot.errFor, "getMe")
	fx.bot.mu.Unlock()
	require.NoError(t, fx.st.Channels().SaveChannel(context.Background(), &store.Channel{ID: DefaultChannelID, Enabled: true, BotToken: "fixed"}))
	fx.svc.ConfigChanged()

	eventually(t, "the loop to recover after the corrected token", func() bool { return fx.svc.currentState() == StateConnected })
	require.Greater(t, fx.bot.pollCount(), 0, "the recovered runtime polls")
}

// TestConnectAbsentRowIsIdle pins the fresh-install connect: an absent
// channel row is idle, never terminal (a store miss must not dead the
// attachment before the first save ever lands).
func TestConnectAbsentRowIsIdle(t *testing.T) {
	st := fake.New() // no channel row
	sessSvc := sessions.NewService(st)
	bot := newFakeBot()
	svc := New(newFakeRuntime(sessSvc), sessSvc, st.Channels(), st.GatewaySettings(), &fakeAttachments{},
		WithBotFactory(func(string, string) (BotAPI, error) { return bot, nil }))
	t.Cleanup(svc.Stop)

	_, err := svc.connect(context.Background())
	require.ErrorIs(t, err, errChannelDisabled, "an absent row is idle, not terminal")
}

// TestConnectBlankTokenIsIdle pins the tokenless connect: an enabled row
// without a token is idle — building a client anyway would 401 terminally
// on the GetMe probe.
func TestConnectBlankTokenIsIdle(t *testing.T) {
	st := fake.New()
	require.NoError(t, st.Channels().SaveChannel(context.Background(), &store.Channel{ID: DefaultChannelID, Enabled: true, BotToken: ""}))
	sessSvc := sessions.NewService(st)
	bot := newFakeBot()
	svc := New(newFakeRuntime(sessSvc), sessSvc, st.Channels(), st.GatewaySettings(), &fakeAttachments{},
		WithBotFactory(func(string, string) (BotAPI, error) { return bot, nil }))
	t.Cleanup(svc.Stop)

	_, err := svc.connect(context.Background())
	require.ErrorIs(t, err, errChannelDisabled, "a tokenless row is idle, not terminal")
}

// TestHeartbeatRestartsWedgedLoop pins the stall probe (design D10): a wedged
// long poll (no round-trip past the stall window) is force-restarted by the
// heartbeat, and the fresh runtime keeps polling.
func TestHeartbeatRestartsWedgedLoop(t *testing.T) {
	fx := newFixture(t)
	fx.gw.transition(gateway.StateRunning)
	fx.svc.Attach(fx.gw)
	eventually(t, "the poll loop to start", func() bool { return fx.bot.pollCount() >= 2 })

	before := fx.bot.pollCount()
	fx.bot.wedgeOnce()
	// The wedge only releases when its context is cancelled — i.e. by the
	// heartbeat's restart (Stop runs after the assertion).
	eventually(t, "the wedged poll to be restarted", func() bool {
		fx.bot.mu.Lock()
		defer fx.bot.mu.Unlock()
		return fx.bot.wedged == 0
	})
	eventually(t, "the fresh runtime to keep polling", func() bool {
		return fx.bot.pollCount() >= before+2
	})
}
