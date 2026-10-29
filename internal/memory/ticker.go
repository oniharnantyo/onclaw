package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// SweepInterval is the consolidation sweep cadence (add-agent-memory design
// D7): one sweep per agent per hour while the gateway is Running. The
// staleness check measures against twice the interval.
const SweepInterval = time.Hour

// SweepStale reports a stale sweep heartbeat (add-agent-memory design D13):
// with the gateway Running, no completed sweep within twice the interval is
// maintenance gone quiet — never swept and never finished both read stale, a
// fresh finish does not. A gateway that is not Running is never stale:
// sweeps are skipped while Paused by design, and cold is not dead. The
// memory block consumes this rule against the heartbeat the sweep records.
func SweepStale(sw store.MemorySweep, found bool, gatewayRunning bool, at time.Time) bool {
	if !gatewayRunning {
		return false
	}
	if !found || sw.LastFinish == nil {
		return true
	}
	return at.Sub(*sw.LastFinish) > 2*SweepInterval
}

// TargetSource resolves one tick's sweep targets: every agent's store
// identity paired with the workspace directory whose memory/ subtree is
// swept.
type TargetSource interface {
	SweepTargets(ctx context.Context) ([]SweepTarget, error)
}

// NewTargets returns the production target source: every stored agent paired
// with its workspace directory under ws. Agents without a valid slug (rows
// not yet backfilled) are skipped with a logged reason — there is no
// workspace to sweep.
func NewTargets(st store.Store, ws *workspaces.Manager) TargetSource {
	return storeTargets{st: st, ws: ws}
}

// storeTargets is NewTargets' implementation.
type storeTargets struct {
	st store.Store
	ws *workspaces.Manager
}

// SweepTargets implements TargetSource.
func (s storeTargets) SweepTargets(ctx context.Context) ([]SweepTarget, error) {
	agents, err := s.st.Agents().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("memory: list agents for sweep: %w", err)
	}
	targets := make([]SweepTarget, 0, len(agents))
	for _, agent := range agents {
		if agent.Slug == "" {
			slog.WarnContext(ctx, "memory: skipping agent without a workspace slug", "agent", agent.ID)
			continue
		}
		dir, err := s.ws.Dir(agent.Slug)
		if err != nil {
			slog.WarnContext(ctx, "memory: skipping agent with an unusable workspace", "agent", agent.ID, "slug", agent.Slug, "error", err)
			continue
		}
		targets = append(targets, SweepTarget{AgentID: agent.ID, WorkspaceDir: dir})
	}
	return targets, nil
}

// Ticker drives the background consolidation sweep (add-agent-memory design
// D7): a gateway-aware loop, modeled on the scheduler's tick loop, sweeping
// every agent once per SweepInterval while the gateway is Running and
// skipping ticks while it is Paused. The sweep itself — and the on-demand
// entry point — is the Sweeper; the ticker only decides when a tick sweeps
// and records the heartbeat through it. Sweep failures are isolated per
// agent inside the Sweeper and logged here.
type Ticker struct {
	sweeper *Sweeper
	targets TargetSource
	gw      gateway.Manager

	// loop is the gateway attachment: nil until Attach and again after
	// Stop. A Ticker carries at most one attachment — the composition root
	// attaches it to the application's one gateway.
	loopMu sync.Mutex
	loop   *tickerLoop
}

// tickerLoop is one attachment's live machinery. The gateway invokes the
// state listener synchronously on transitions, so mu is never held while
// waiting on the loop goroutine.
type tickerLoop struct {
	mu     sync.Mutex
	sub    func()             // gateway subscription cancel; nil once detached
	cancel context.CancelFunc // loop cancel; nil while no loop is running
	wg     sync.WaitGroup     // tracks the loop goroutine
	align  func(now time.Time) time.Duration
}

// alignToNextHour is the production alignment: the duration until the next
// whole-hour boundary, so sweeps land on the hour without drift.
func alignToNextHour(now time.Time) time.Duration {
	return now.Truncate(SweepInterval).Add(SweepInterval).Sub(now)
}

// NewTicker returns a sweep ticker over the store and the workspace manager,
// sweeping with its own Sweeper.
func NewTicker(st store.Store, ws *workspaces.Manager) *Ticker {
	return &Ticker{sweeper: NewSweeper(st), targets: NewTargets(st, ws)}
}

// Attach subscribes to the gateway (subscribe-first, like the scheduler's
// attachment) and starts the hour-aligned tick loop on the first transition
// to Running — or immediately when the gateway is already Running. The loop
// keeps ticking through Paused (a later Resume must find the ticker alive)
// and stops on a transition to Stopped; a later Running transition starts a
// fresh loop. Attach does not block on the gateway.
func (t *Ticker) Attach(gw gateway.Manager) {
	// Re-attaching (never done by the composition root) replaces the
	// previous attachment: stop its loop, drop its subscription.
	if prev := t.swapLoop(nil); prev != nil {
		prev.detach()
		prev.stop()
	}

	t.gw = gw
	loop := &tickerLoop{align: alignToNextHour}
	cancel := gw.Subscribe(func(_, to gateway.State) { t.onGatewayState(loop, to) })
	loop.mu.Lock()
	loop.sub = cancel
	loop.mu.Unlock()
	t.swapLoop(loop)

	// Subscribe before reading the state so a concurrent transition cannot
	// fall between the two: the listener and this check are both
	// start-loop-on-Running, and starting twice is a no-op.
	if gw.State() == gateway.StateRunning {
		t.startLoop(loop)
	}
}

// Stop ends the tick loop and the gateway subscription. It is idempotent and
// returns once the loop goroutine has exited (an in-flight sweep completes
// first). Safe to call before Attach and before the gateway ever runs.
func (t *Ticker) Stop() {
	if loop := t.swapLoop(nil); loop != nil {
		loop.detach()
		loop.stop()
	}
}

// swapLoop installs next as the ticker's attachment and returns the previous
// one (nil when there was none).
func (t *Ticker) swapLoop(next *tickerLoop) *tickerLoop {
	t.loopMu.Lock()
	defer t.loopMu.Unlock()
	prev := t.loop
	t.loop = next
	return prev
}

// onGatewayState is the gateway listener: the loop runs while the gateway is
// up (ticks through Paused are skipped inside tickOnce) and stops when the
// gateway goes down; the next Running starts a fresh loop.
func (t *Ticker) onGatewayState(loop *tickerLoop, to gateway.State) {
	switch to {
	case gateway.StateRunning:
		t.startLoop(loop)
	case gateway.StateStopped:
		loop.stop()
	}
}

// startLoop starts the tick loop unless one is already running. The gateway
// check at Attach and the listener can race; the mutex plus the cancel
// sentinel keep exactly one loop alive.
func (t *Ticker) startLoop(loop *tickerLoop) {
	loop.mu.Lock()
	if loop.cancel != nil {
		loop.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	loop.cancel = cancel
	align := loop.align
	loop.wg.Add(1)
	loop.mu.Unlock()

	go func() {
		defer loop.wg.Done()
		t.tickLoop(ctx, align)
	}()
}

// stop cancels the loop and waits for the goroutine to exit. Safe to call
// repeatedly and before any loop started. It never holds mu while waiting,
// so a gateway transition listener may call it while the loop goroutine is
// mid-tick reading the gateway state.
func (l *tickerLoop) stop() {
	l.mu.Lock()
	cancel := l.cancel
	l.cancel = nil
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	l.wg.Wait()
}

// detach cancels the gateway subscription, leaving the loop untouched.
func (l *tickerLoop) detach() {
	l.mu.Lock()
	sub := l.sub
	l.sub = nil
	l.mu.Unlock()
	if sub != nil {
		sub()
	}
}

// tickLoop ticks immediately (a process-off stretch must not leave memory
// unswept until the next hour boundary), then sleeps to each next boundary
// and ticks again, until ctx is cancelled.
func (t *Ticker) tickLoop(ctx context.Context, align func(now time.Time) time.Duration) {
	t.tickOnce(ctx)
	for {
		timer := time.NewTimer(align(time.Now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		t.tickOnce(ctx)
	}
}

// tickOnce runs one pass of the sweep, only while the gateway is Running —
// a tick while Paused (or, defensively, Stopped) is skipped without touching
// the heartbeat (spec agent-memory "Sweep does not run while paused").
func (t *Ticker) tickOnce(ctx context.Context) {
	if t.gw.State() != gateway.StateRunning {
		return
	}
	targets, err := t.targets.SweepTargets(ctx)
	if err != nil {
		slog.WarnContext(ctx, "memory: sweep tick failed to resolve targets", "error", err)
		return
	}
	for _, result := range t.sweeper.Sweep(ctx, targets) {
		if result.Err != nil {
			slog.WarnContext(ctx, "memory: sweep failed", "agent", result.Target.AgentID, "error", result.Err)
		}
	}
}
