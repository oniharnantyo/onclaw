package browseruse

import (
	"context"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// maxFixedWaitMs caps the wait tool's fixed delay: an accidental huge value
// would hold the profile's session lock past any useful action timeout
// anyway, but a hard cap keeps the failure mode obvious.
const maxFixedWaitMs = 5 * 60 * 1000

// domStableWindow is how long the DOM must stay mutation-free for the page to
// count as stable — the fallback settle for load-less pages. It matches the
// window rod's WaitStable(500ms) used before the chromedp migration.
const domStableWindow = 500 * time.Millisecond

// settlePollInterval is the tick between two DOM-stability checks and two
// interactability retries; the settle windows above, not this tick, decide
// how long a settle takes.
const settlePollInterval = 100 * time.Millisecond

// The settle/wait primitives. Each waits on the session's page, bound to the
// caller's context through bindCall. Test seams (requestIdleFn, stableWaitFn,
// sleepFn) keep their shapes: a nil seam falls through to the CDP default.

// settle waits for the page to settle after an operation that may navigate:
// network quiet first, falling back to DOM stability for pages that never
// fire a load event (SPAs). Best-effort by design — a settle that cannot
// converge must not fail the action that triggered it.
func (s *profileSession) settle(ctx context.Context) {
	_ = runSettle(ctx, s.waitIdle, s.waitStable)
}

// runSettle waits for network quiet, falling back to a DOM-stability wait
// when the network never quiets or the request tracking breaks.
func runSettle(ctx context.Context, waitIdle, waitStable func(context.Context) error) error {
	if err := waitIdle(ctx); err != nil {
		return waitStable(ctx)
	}
	return nil
}

// waitIdle blocks until the page's network activity has been quiet for the
// settle window (or the context deadline arrives).
func (s *profileSession) waitIdle(ctx context.Context) error {
	if s.requestIdleFn != nil {
		return s.requestIdleFn(ctx, s.pageCtx)
	}
	return waitRequestIdleCDP(ctx, s.pageCtx)
}

// waitStable blocks until the page's DOM has been stable for a moment — the
// fallback for load-less pages where request tracking cannot converge.
func (s *profileSession) waitStable(ctx context.Context) error {
	if s.stableWaitFn != nil {
		return s.stableWaitFn(ctx, s.pageCtx)
	}
	return waitStableCDP(ctx, s.pageCtx)
}

// sleep waits for a fixed duration, aborting on context cancellation.
func (s *profileSession) sleep(ctx context.Context, d time.Duration) error {
	if s.sleepFn != nil {
		return s.sleepFn(ctx, d)
	}
	return sleepCDP(ctx, d)
}

// waitRequestIdleCDP waits for the page's requests to settle with a CDP
// in-flight counter over Network events — the engine mapping of rod's
// WaitRequestIdle: count requests seen from now on (excluding the resource
// types rod excluded by default), wait until the count has stayed at zero
// for the network-idle window. A cancelled context is reported as the error
// that triggers the DOM-stability fallback.
func waitRequestIdleCDP(ctx context.Context, page context.Context) error {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		inflight = map[network.RequestID]bool{}
		quiet    *time.Timer
	)
	idle := make(chan struct{})
	var idleOnce sync.Once
	arm := func() {
		if quiet != nil {
			quiet.Stop()
		}
		quiet = time.AfterFunc(networkIdleWindow, func() {
			idleOnce.Do(func() { close(idle) })
		})
	}
	defer func() {
		mu.Lock()
		if quiet != nil {
			quiet.Stop()
		}
		mu.Unlock()
	}()

	// No request is counted before Network.enable replies, so tracking
	// starts idle and the quiet window starts immediately — the rod
	// IdleCounter armed its timer the same way on an empty list.
	arm()

	// The listener runs on chromedp's reader goroutine: keep it fast (map
	// bookkeeping only). The subscription ends when runCtx is cancelled.
	if err := chromedp.Listen(runCtx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			mu.Lock()
			if settleExcludedResource(e.Type) || inflight[e.RequestID] {
				// Redirects re-send the same request id: count once.
				mu.Unlock()
				return
			}
			inflight[e.RequestID] = true
			if quiet != nil {
				quiet.Stop()
				quiet = nil
			}
			mu.Unlock()
		case *network.EventLoadingFinished:
			settleRequestDone(&mu, inflight, e.RequestID, arm)
		case *network.EventLoadingFailed:
			settleRequestDone(&mu, inflight, e.RequestID, arm)
		}
	}); err != nil {
		return err
	}

	if _, err := chromedp.Run(runCtx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(c, t, network.Enable, network.EnableParams{})
		return err
	})); err != nil {
		return err
	}

	select {
	case <-idle:
		return nil
	case <-runCtx.Done():
		return runCtx.Err()
	}
}

// settleRequestDone retires one finished or failed request; when it was the
// last one in flight the quiet window restarts.
func settleRequestDone(mu *sync.Mutex, inflight map[network.RequestID]bool, id network.RequestID, arm func()) {
	mu.Lock()
	defer mu.Unlock()
	if !inflight[id] {
		return
	}
	delete(inflight, id)
	if len(inflight) == 0 {
		arm()
	}
}

// settleExcludedResource reports whether a resource type never counts toward
// network idle — rod's WaitRequestIdle default exclusions, kept for parity so
// media-heavy pages settle the same way.
func settleExcludedResource(t network.ResourceType) bool {
	switch t {
	case network.ResourceTypeWebSocket,
		network.ResourceTypeEventSource,
		network.ResourceTypeMedia,
		network.ResourceTypeImage,
		network.ResourceTypeFont:
		return true
	default:
		return false
	}
}

// stableStateJS installs (once per document) the state a MutationObserver
// keeps: the time of the last DOM mutation and whether the page finished
// loading. The poll predicate below reads it — the engine mapping of rod's
// WaitStable(500ms), which waited for the load event plus a DOM snapshot that
// stayed unchanged across a 500ms window.
const stableStateJS = `(() => {
	const w = window;
	if (w.__buddiesStable) return;
	const state = { last: Date.now(), loaded: document.readyState === 'complete' };
	w.__buddiesStable = state;
	new MutationObserver(() => { state.last = Date.now(); }).observe(document, {
		childList: true, subtree: true, attributes: true, characterData: true,
	});
	document.addEventListener('readystatechange', () => {
		if (document.readyState === 'complete') state.loaded = true;
	});
})()`

// stablePollJS answers whether the page has loaded and stayed mutation-free
// for the DOM-stability window.
const stablePollJS = `(() => {
	const state = window.__buddiesStable;
	return !!state && state.loaded && Date.now() - state.last >= 500;
})()`

// waitStableCDP waits for DOM stability: install the mutation-observer state,
// then poll until the page is loaded and quiet for domStableWindow. Bounded
// by the run context, like rod's WaitStable was bounded by the page context.
func waitStableCDP(ctx context.Context, page context.Context) error {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	if _, err := chromedp.Run(runCtx, chromedp.Evaluate[chromedp.Void](stableStateJS)); err != nil {
		return err
	}
	_, err := chromedp.Run(runCtx, chromedp.Poll[bool](stablePollJS,
		chromedp.WithPollingInterval(settlePollInterval),
		chromedp.WithPollingTimeout(0), // bound by the run context only
	))
	return err
}

// sleepCDP waits for d with context cancellation support.
func sleepCDP(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// waitSettle implements the wait tool's modes: with no ms argument the call
// waits for network quiet; with ms it waits a fixed delay, bounded by the
// action timeout.
func (s *profileSession) waitSettle(ctx context.Context, ms *int) error {
	if ms == nil {
		return s.waitIdle(ctx)
	}
	return s.sleep(ctx, fixedWaitDuration(*ms))
}

// fixedWaitDuration resolves the wait tool's fixed delay: negative values
// wait zero, absurd values are capped.
func fixedWaitDuration(ms int) time.Duration {
	if ms < 0 {
		return 0
	}
	if ms > maxFixedWaitMs {
		return maxFixedWaitMs * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}
