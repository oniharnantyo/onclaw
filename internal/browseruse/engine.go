package browseruse

import (
	"context"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// The chromedp engine kit shared by every session file.
//
// A session is two contexts created at launch (launch.go): browserCtx, the
// chromedp browser context that owns the browser lifecycle, and pageCtx, the
// target context for the profile's single about:blank tab. A page handle is
// a context.Context that carries the chromedp target; every seam that took a
// *rod.Page now takes that context.
//
// Rules every file in this package codes against:
//
//   - To bound an outer tool-call ctx around chromedp actions, derive a run
//     context with bindCall and run the actions on it:
//
//	runCtx, cancel := bindCall(s.pageCtx, ctx)
//	defer cancel()
//	err := chromedp.Do(runCtx, chromedp.Navigate(url))
//
//     runCtx carries the page's target executor, inherits ctx's deadline, and
//     is cancelled when ctx is cancelled or when the returned cancel runs.
//
//   - Page-domain commands go through the page context: chromedp.Run/Do
//     actions, or cdp.Call against the session from pageTarget(s.pageCtx).
//
//   - Never chromedp.Run/Do the browser context itself. It carries no target,
//     so Run would open a second tab on the profile and break the isolation
//     checks. Browser-domain commands (target.*, browser.*) go through
//     browserSession(s.browserCtx) instead.
//
//   - The cookie jar seam functions (cookies.go) take the bounding ctx plus
//     the session's browser context, like every other seam:
//     exportCookieJar(ctx context.Context, browserCtx context.Context, dir
//     string) error and injectCookieJar with the same signature. Pass a
//     derive of the browser context (to add a deadline) and never a page
//     context.
//
//   - Protocol errors from the browser surface as *cdproto.Error (github.com/
//     chromedp/cdproto); executor and connection failures arrive as chromedp
//     error strings ("target detached", "lost the connection to the browser")
//     or context errors. server.go's readableCdpError owns the mapping.
//
//   - Chromium's process is owned by launch.go, not chromedp: teardown always
//     goes through profileSession.closeFn (browserProc.teardown), which
//     cancels the contexts and kills the process group. Never cancel the
//     contexts directly to close a browser.

// bindCall derives a run context for one tool call from the session's page
// (or browser) context. The returned context carries the chromedp executor
// of page, inherits ctx's deadline, and is cancelled as soon as ctx is
// cancelled; the returned cancel releases both bindings. Callers defer it:
//
//	runCtx, cancel := bindCall(s.pageCtx, ctx)
//	defer cancel()
func bindCall(page, ctx context.Context) (context.Context, context.CancelFunc) {
	runCtx, cancel := context.WithCancel(page)
	if deadline, ok := ctx.Deadline(); ok {
		var deadlineCancel context.CancelFunc
		runCtx, deadlineCancel = context.WithDeadline(runCtx, deadline)
		prevCancel := cancel
		cancel = func() {
			deadlineCancel()
			prevCancel()
		}
	}
	// Link ctx's cancellation (a deadline alone does not carry it) and stop
	// the link once the caller releases the run context.
	done := make(chan struct{})
	var once sync.Once
	stopLink := func() {
		once.Do(func() {
			cancel()
			close(done)
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stopLink()
		case <-done:
		}
	}()
	return runCtx, stopLink
}

// pageTarget returns the chromedp target behind a session page context: the
// cdp.Session its CDP calls go through, with TargetID for target-scoped
// commands (the liveness probe, snapshots). ok is false when the context is a
// stub or has not run anything yet, so no target is attached.
func pageTarget(page context.Context) (*chromedp.Target, bool) {
	c := chromedp.FromContext(page)
	if c == nil || c.Target == nil {
		return nil, false
	}
	return c.Target, true
}

// browserSession returns the browser-level cdp.Session behind a session's
// browser context (the launch attach step populates it). Browser-domain
// commands run on it; see the kit rules for why the browser context is never
// Run directly. ok is false for stub contexts and before attach.
func browserSession(browserCtx context.Context) (cdp.Session, bool) {
	c := chromedp.FromContext(browserCtx)
	if c == nil || c.Browser == nil {
		return nil, false
	}
	return c.Browser, true
}
