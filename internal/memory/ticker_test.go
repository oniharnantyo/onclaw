package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
	"github.com/stretchr/testify/require"
)

// The sweep ticker tests (add-agent-memory tasks 6.7, spec agent-memory
// "Consolidation sweep" / "Sweep heartbeat and visibility"), modeled on the
// scheduler's ticker tests: no sweep while Paused, the stale heartbeat after
// two missed intervals while Running, and no staleness after a successful
// sweep.

// tickInterval is the test alignment: the loop ticks every 5 ms instead of
// waiting for hour boundaries.
const tickInterval = 5 * time.Millisecond

// mutableClock is a thread-safe clock tests advance while the ticker's loop
// goroutine reads it (the scheduler ticker test's pattern).
type mutableClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *mutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *mutableClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// tickerFixture wires a Ticker over the fake store and a real gateway
// Manager, with the sweeper's clock pinned to the mutable clock so every
// sweep's heartbeat is timestamped deterministically.
type tickerFixture struct {
	st    *fake.Store
	clock *mutableClock
	gw    gateway.Manager
	tk    *Ticker
}

func newTickerFixture(t *testing.T, start time.Time) *tickerFixture {
	t.Helper()
	f := &tickerFixture{
		st:    fake.New(),
		clock: &mutableClock{t: start},
		gw:    gateway.NewManager(),
	}
	require.NoError(t, f.st.Agents().Create(context.Background(), &store.Agent{
		ID:   "agent-1",
		Name: "Agent agent-1",
		Slug: "agent-1",
	}))
	f.tk = NewTicker(f.st, workspaces.NewManager(t.TempDir()))
	f.tk.sweeper.now = f.clock.Now
	t.Cleanup(f.tk.Stop)
	return f
}

// heartbeat returns the agent's recorded sweep heartbeat (found, finish).
func (f *tickerFixture) heartbeat(t *testing.T) (bool, *time.Time) {
	t.Helper()
	sw, found, err := f.st.Memory().GetSweep(context.Background(), "agent-1")
	require.NoError(t, err)
	return found, sw.LastFinish
}

// setTickAlign re-points the attachment's loop alignment at the short test
// interval. Must be called after Attach and before the loop starts.
func setTickAlign(t *testing.T, tk *Ticker) {
	t.Helper()
	loop := tk.loop
	require.NotNil(t, loop, "ticker has no attached loop")
	loop.mu.Lock()
	loop.align = func(time.Time) time.Duration { return tickInterval }
	loop.mu.Unlock()
}

// TestTicker_NoSweepWhilePaused covers the paused skip (spec agent-memory
// "Sweep does not run while paused"): the loop keeps ticking through a Pause
// but sweeps nothing, and a Resume picks the sweep back up.
func TestTicker_NoSweepWhilePaused(t *testing.T) {
	base := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	f := newTickerFixture(t, base)

	f.tk.Attach(f.gw)
	setTickAlign(t, f.tk)

	require.NoError(t, f.gw.Start(context.Background()))
	// The immediate first sweep records the heartbeat at the pinned clock.
	require.Eventually(t, func() bool {
		found, finish := f.heartbeat(t)
		return found && finish != nil && finish.Equal(base)
	}, time.Second, tickInterval)

	require.NoError(t, f.gw.Pause())
	f.clock.Advance(200 * time.Millisecond) // dozens of ticks, all skipped
	require.Never(t, func() bool {
		_, finish := f.heartbeat(t)
		return finish != nil && !finish.Equal(base)
	}, 300*time.Millisecond, tickInterval, "no sweep may run while Paused")

	// Resuming finds the loop still alive: the next tick sweeps again.
	require.NoError(t, f.gw.Resume())
	require.Eventually(t, func() bool {
		_, finish := f.heartbeat(t)
		return finish != nil && finish.After(base)
	}, time.Second, tickInterval)
}

// TestTicker_StaleAfterTwoMissedIntervalsWhileRunning covers the stale
// notice (spec agent-memory "Stale maintenance is surfaced"): the gateway
// Running with no successful sweep within twice the interval reads stale —
// never swept and never finished both do — while the same old heartbeat
// with the gateway not Running never does.
func TestTicker_StaleAfterTwoMissedIntervalsWhileRunning(t *testing.T) {
	base := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	f := newTickerFixture(t, base)

	f.tk.Attach(f.gw)
	setTickAlign(t, f.tk)
	require.NoError(t, f.gw.Start(context.Background()))
	require.Eventually(t, func() bool {
		found, _ := f.heartbeat(t)
		return found
	}, time.Second, tickInterval)

	// The sweep then goes quiet while the gateway stays Running — the
	// ticker stops succeeding (a crashed sweep, a slept process) — and two
	// intervals pass with no further successful sweep.
	f.tk.Stop()
	f.clock.Advance(2*SweepInterval + time.Second)
	sw, found, err := f.st.Memory().GetSweep(context.Background(), "agent-1")
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, SweepStale(sw, found, true, f.clock.Now()),
		"past two missed intervals while Running, maintenance is stale")
	require.False(t, SweepStale(sw, found, false, f.clock.Now()),
		"the same heartbeat with the gateway not Running is never stale")

	// A heartbeatless agent reads stale too: never swept is not healthy.
	require.True(t, SweepStale(store.MemorySweep{}, false, true, f.clock.Now()))
}

// TestTicker_NoStaleNoticeAfterSuccessfulSweep covers the healthy case (spec
// agent-memory "Healthy maintenance is silent"): a sweep that succeeded
// within the interval clears the staleness the next block would read.
func TestTicker_NoStaleNoticeAfterSuccessfulSweep(t *testing.T) {
	base := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	f := newTickerFixture(t, base)

	f.tk.Attach(f.gw)
	setTickAlign(t, f.tk)
	require.NoError(t, f.gw.Start(context.Background()))
	require.Eventually(t, func() bool {
		found, _ := f.heartbeat(t)
		return found
	}, time.Second, tickInterval)

	// Well within two intervals of the successful sweep: silent.
	f.clock.Advance(SweepInterval)
	sw, found, err := f.st.Memory().GetSweep(context.Background(), "agent-1")
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, SweepStale(sw, found, true, f.clock.Now()),
		"a fresh successful sweep is not stale")

	// And the next sweep, after the tick, moves the heartbeat forward —
	// still silent at the advanced clock.
	f.clock.Advance(SweepInterval)
	require.Eventually(t, func() bool {
		_, finish := f.heartbeat(t)
		return finish != nil && finish.Equal(f.clock.Now())
	}, time.Second, tickInterval)
	sw, found, err = f.st.Memory().GetSweep(context.Background(), "agent-1")
	require.NoError(t, err)
	require.False(t, SweepStale(sw, found, true, f.clock.Now()))
}
