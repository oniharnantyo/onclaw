package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/stretchr/testify/require"
)

// tickInterval is the test alignment: the loop ticks every 5 ms instead of
// waiting for minute boundaries.
const tickInterval = 5 * time.Millisecond

// mutableClock is a thread-safe clock tests advance while the ticker's loop
// goroutine reads it.
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

// syncRuns is a mutex-guarded recording Runs: the ticker fires from a
// background goroutine, so plain field access from tests would race.
type syncRuns struct {
	mu      sync.Mutex
	creates int
	submits int
}

func (r *syncRuns) Create(_ context.Context, _, _ string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates++
	return fmt.Sprintf("sess-%d", r.creates), nil
}

func (r *syncRuns) Submit(_ context.Context, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.submits++
	return nil
}

func (r *syncRuns) createCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.creates
}

// tickerFixture wires a Service over the fake store, a guarded recording
// Runs, a mutable clock, and a real gateway Manager — everything Attach
// needs.
type tickerFixture struct {
	st    *fake.Store
	runs  *syncRuns
	clock *mutableClock
	gw    gateway.Manager
	svc   *Service
}

func newTickerFixture(t *testing.T, start time.Time) *tickerFixture {
	t.Helper()
	f := &tickerFixture{
		st:    fake.New(),
		runs:  &syncRuns{},
		clock: &mutableClock{t: start},
		gw:    gateway.NewManager(),
	}
	require.NoError(t, f.st.Agents().Create(context.Background(), &store.Agent{ID: "agt-1", Name: "Agent agt-1"}))
	f.svc = New(f.st, f.runs, f.gw, WithClock(f.clock.Now))
	t.Cleanup(f.svc.Stop)
	return f
}

// seedDue stores an enabled schedule whose next fire time is slot, bypassing
// service validation (the service would compute a future slot). The name is
// unique per call — the fixture seeds several schedules across a test.
func (f *tickerFixture) seedDue(t *testing.T, slot time.Time) string {
	t.Helper()
	row := store.Schedule{
		Name:       fmt.Sprintf("tick-%d", time.Now().UnixNano()),
		AgentID:    "agt-1",
		Prompt:     "hello",
		Cron:       "* * * * *",
		Enabled:    true,
		NextFireAt: &slot,
	}
	require.NoError(t, f.st.Schedules().Create(context.Background(), &row))
	return row.ID
}

// setTickAlign re-points the attachment's loop alignment at the short test
// interval. Must be called after Attach and before the loop starts.
func setTickAlign(t *testing.T, svc *Service) {
	t.Helper()
	loop := svc.attachedLoop()
	require.NotNil(t, loop, "service has no attached ticker")
	loop.mu.Lock()
	loop.align = func(time.Time) time.Duration { return tickInterval }
	loop.mu.Unlock()
}

// rowStatus returns the stored last-run status of the schedule.
func (f *tickerFixture) rowStatus(t *testing.T, id string) string {
	t.Helper()
	return mustRow(t, f.st, id).LastRunStatus
}

func TestTicker_NoTickBeforeGatewayStarts(t *testing.T) {
	base := time.Date(2026, time.October, 21, 9, 0, 0, 0, testZone)
	f := newTickerFixture(t, base)
	id := f.seedDue(t, base.Add(-5*time.Minute)) // overdue: any tick marks it missed

	f.svc.Attach(f.gw)
	setTickAlign(t, f.svc)

	// The gateway never ran, so no loop exists and no tick ever happened.
	require.Never(t, func() bool {
		return f.rowStatus(t, id) != ""
	}, 150*time.Millisecond, tickInterval)
}

func TestTicker_ImmediateTickOnRunning_FiresDueSchedule(t *testing.T) {
	base := time.Date(2026, time.October, 21, 9, 0, 0, 0, testZone)
	f := newTickerFixture(t, base)
	id := f.seedDue(t, base.Add(-30*time.Second)) // due, within LateTolerance

	f.svc.Attach(f.gw)
	setTickAlign(t, f.svc)

	// No loop before the gateway runs.
	require.Never(t, func() bool {
		return f.runs.createCount() > 0
	}, 50*time.Millisecond, tickInterval)

	require.NoError(t, f.gw.Start(context.Background()))

	// The first tick is immediate — it must not wait for the next boundary.
	require.Eventually(t, func() bool {
		return f.runs.createCount() == 1
	}, time.Second, tickInterval)
	require.Eventually(t, func() bool {
		return f.rowStatus(t, id) == string(store.ScheduleRunStatusRan)
	}, time.Second, tickInterval)
}

func TestTicker_KeepsTickingWhilePaused(t *testing.T) {
	base := time.Date(2026, time.October, 21, 9, 0, 0, 0, testZone)
	f := newTickerFixture(t, base)
	id := f.seedDue(t, base.Add(time.Minute)) // future: the boot tick does nothing

	f.svc.Attach(f.gw)
	setTickAlign(t, f.svc)

	require.NoError(t, f.gw.Start(context.Background()))
	require.NoError(t, f.gw.Pause())
	// A pause held past the slot means the slot is missed — and only a
	// still-running loop can record that.
	f.clock.Advance(70 * time.Second)

	require.Eventually(t, func() bool {
		return f.rowStatus(t, id) == string(store.ScheduleRunStatusMissed)
	}, time.Second, tickInterval)
	require.Equal(t, 0, f.runs.createCount()) // paused, never fired
}

func TestTicker_LoopStopsAfterGatewayStop(t *testing.T) {
	base := time.Date(2026, time.October, 21, 9, 0, 0, 0, testZone)
	f := newTickerFixture(t, base)

	f.svc.Attach(f.gw)
	setTickAlign(t, f.svc)
	require.NoError(t, f.gw.Start(context.Background()))
	// Stop notifies listeners synchronously and the listener waits for the
	// loop goroutine, so the loop is gone when Stop returns.
	require.NoError(t, f.gw.Stop())

	// An overdue schedule seeded after the stop is never marked: no loop
	// exists to tick it.
	id := f.seedDue(t, f.clock.Now().Add(-5*time.Minute))
	require.Never(t, func() bool {
		return f.rowStatus(t, id) != ""
	}, 150*time.Millisecond, tickInterval)
}

func TestTicker_FreshLoopOnNextRunning(t *testing.T) {
	base := time.Date(2026, time.October, 21, 9, 0, 0, 0, testZone)
	f := newTickerFixture(t, base)
	first := f.seedDue(t, base.Add(-30*time.Second))

	f.svc.Attach(f.gw)
	setTickAlign(t, f.svc)
	require.NoError(t, f.gw.Start(context.Background()))
	require.Eventually(t, func() bool {
		return f.runs.createCount() == 1
	}, time.Second, tickInterval)
	require.NoError(t, f.gw.Stop())

	// A schedule becoming due while the gateway is down stays untouched…
	// 3m30s puts the first row's advanced slot (base+1m) 2m30s in the past —
	// beyond LateTolerance.
	f.clock.Advance(3*time.Minute + 30*time.Second)
	second := f.seedDue(t, f.clock.Now().Add(-30*time.Second))
	require.Never(t, func() bool {
		return f.rowStatus(t, second) != ""
	}, 50*time.Millisecond, tickInterval)

	// …until the gateway runs again: a fresh loop fires it, while the first
	// row's slot — now overdue beyond LateTolerance — is marked missed
	// instead of bursting late (design D3).
	require.NoError(t, f.gw.Start(context.Background()))
	require.Eventually(t, func() bool {
		return f.runs.createCount() == 2
	}, time.Second, tickInterval)
	require.Equal(t, string(store.ScheduleRunStatusMissed), f.rowStatus(t, first))
	require.Equal(t, string(store.ScheduleRunStatusRan), f.rowStatus(t, second))
}

func TestTicker_StopIdempotentAndSafeBeforeRunning(t *testing.T) {
	base := time.Date(2026, time.October, 21, 9, 0, 0, 0, testZone)
	f := newTickerFixture(t, base)
	f.svc.Attach(f.gw)
	setTickAlign(t, f.svc)

	f.svc.Stop() // never ran
	f.svc.Stop() // idempotent
	require.Nil(t, f.svc.attachedLoop(), "Stop must detach the ticker")

	// A service that was never attached stops safely too.
	New(fake.New(), &syncRuns{}, gateway.NewManager()).Stop()
}
