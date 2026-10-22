package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/oniharnantyo/buddies/internal/gateway"
)

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

// alignToNextMinute is the production alignment: the duration until the next
// whole-minute boundary, so ticks land on :00 without drift (design D2).
func alignToNextMinute(now time.Time) time.Duration {
	return now.Truncate(time.Minute).Add(time.Minute).Sub(now)
}

// Attach subscribes to the gateway (subscribe-first, design D2) and starts
// the minute-aligned tick loop on the first transition to Running — or
// immediately when the gateway is already Running. The loop keeps ticking
// through Paused (missed marks need ticks) and stops on a transition to
// Stopped; a later Running transition starts a fresh loop. Attach does not
// block on the gateway.
func (s *Service) Attach(gw gateway.Manager) {
	// Re-attaching (never done by the composition root) replaces the
	// previous attachment: stop its loop, drop its subscription.
	if prev := s.swapLoop(nil); prev != nil {
		prev.detach()
		prev.stop()
	}

	loop := &tickerLoop{align: alignToNextMinute}
	cancel := gw.Subscribe(func(_, to gateway.State) { s.onGatewayState(loop, to) })
	loop.mu.Lock()
	loop.sub = cancel
	loop.mu.Unlock()
	s.swapLoop(loop)

	// Subscribe before reading the state so a concurrent transition cannot
	// fall between the two: the listener and this check are both
	// start-loop-on-Running, and starting twice is a no-op.
	if gw.State() == gateway.StateRunning {
		s.startLoop(loop)
	}
}

// Stop ends the tick loop and the gateway subscription. It is idempotent and
// returns once the loop goroutine has exited (an in-flight tick completes
// first). Safe to call before Attach and before the gateway ever runs.
func (s *Service) Stop() {
	if loop := s.swapLoop(nil); loop != nil {
		loop.detach()
		loop.stop()
	}
}

// swapLoop installs next as the service's attachment and returns the previous
// one (nil when there was none).
func (s *Service) swapLoop(next *tickerLoop) *tickerLoop {
	s.loopMu.Lock()
	defer s.loopMu.Unlock()
	prev := s.loop
	s.loop = next
	return prev
}

// attachedLoop returns the current attachment, nil when detached.
func (s *Service) attachedLoop() *tickerLoop {
	s.loopMu.Lock()
	defer s.loopMu.Unlock()
	return s.loop
}

// onGatewayState is the gateway listener: the loop runs while triggers are
// being accepted or held (missed marks need ticks through Paused) and stops
// when the gateway goes down; the next Running starts a fresh loop.
func (s *Service) onGatewayState(loop *tickerLoop, to gateway.State) {
	switch to {
	case gateway.StateRunning:
		s.startLoop(loop)
	case gateway.StateStopped:
		loop.stop()
	}
}

// startLoop starts the tick loop unless one is already running. The gateway
// check at Attach and the listener can race; the mutex plus the cancel
// sentinel keep exactly one loop alive.
func (s *Service) startLoop(loop *tickerLoop) {
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
		s.tickLoop(ctx, align)
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

// tickLoop ticks immediately (design D2: process-off misses are marked at
// boot and anything due within tolerance fires without waiting up to a
// minute), then sleeps to each next boundary and ticks again, until ctx is
// cancelled.
func (s *Service) tickLoop(ctx context.Context, align func(now time.Time) time.Duration) {
	s.tickOnce(ctx)
	for {
		timer := time.NewTimer(align(time.Now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.tickOnce(ctx)
	}
}

// tickOnce runs one pass of the due state machine at the service's clock. A
// failed pass (the store listing) is logged; the loop keeps ticking.
func (s *Service) tickOnce(ctx context.Context) {
	if err := s.Tick(ctx, s.now()); err != nil {
		slog.WarnContext(ctx, "scheduler: tick failed", "error", err)
	}
}
