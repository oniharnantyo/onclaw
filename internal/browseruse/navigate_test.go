package browseruse

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestRunSettle(t *testing.T) {
	t.Run("network idle satisfied: no fallback", func(t *testing.T) {
		var stableCalls int
		err := runSettle(context.Background(),
			func(_ context.Context) error { return nil },
			func(_ context.Context) error { stableCalls++; return nil })
		require.NoError(t, err)
		require.Equal(t, 0, stableCalls, "the DOM-stability fallback must not run when the network quiets")
	})

	t.Run("network never quiets: DOM-stability fallback runs", func(t *testing.T) {
		var idleCalls, stableCalls int
		err := runSettle(context.Background(),
			func(_ context.Context) error { idleCalls++; return errors.New("load-less page") },
			func(_ context.Context) error { stableCalls++; return nil })
		require.NoError(t, err)
		require.Equal(t, 1, idleCalls)
		require.Equal(t, 1, stableCalls, "load-less SPA pages must settle through DOM stability")
	})
}

func TestSessionWaitSettle(t *testing.T) {
	ctx := context.Background()

	t.Run("absent ms waits for network idle", func(t *testing.T) {
		s := &profileSession{}
		var idleCalls, stableCalls, sleeps int
		s.requestIdleFn = func(_ context.Context, _ context.Context) error {
			idleCalls++
			return nil
		}
		s.stableWaitFn = func(_ context.Context, _ context.Context) error {
			stableCalls++
			return nil
		}
		s.sleepFn = func(_ context.Context, _ time.Duration) error {
			sleeps++
			return nil
		}
		require.NoError(t, s.waitSettle(ctx, nil))
		require.Equal(t, 1, idleCalls, "no ms argument must mean a network-idle wait")
		require.Equal(t, 0, stableCalls)
		require.Equal(t, 0, sleeps, "a network-idle wait must not sleep a fixed delay")
	})

	t.Run("ms waits a fixed delay", func(t *testing.T) {
		s := &profileSession{}
		var idleCalls int
		var slept time.Duration
		s.requestIdleFn = func(_ context.Context, _ context.Context) error {
			idleCalls++
			return nil
		}
		s.sleepFn = func(_ context.Context, d time.Duration) error {
			slept = d
			return nil
		}
		ms := 500
		require.NoError(t, s.waitSettle(ctx, &ms))
		require.Equal(t, 0, idleCalls, "a fixed delay must not probe the network")
		require.Equal(t, 500*time.Millisecond, slept)
	})

	t.Run("fixed delay aborts on context cancellation", func(t *testing.T) {
		s := &profileSession{}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		require.ErrorIs(t, s.sleep(canceled, time.Hour), context.Canceled)
	})
}

func TestFixedWaitDuration(t *testing.T) {
	require.Equal(t, 0*time.Millisecond, fixedWaitDuration(-5), "negative waits zero")
	require.Equal(t, 500*time.Millisecond, fixedWaitDuration(500))
	capped := fixedWaitDuration(1 << 30)
	require.Equal(t, maxFixedWaitMs*time.Millisecond, capped, "absurd values are capped")
}

// TestBrowser_NavigateAndSettle is the navigate/settle loopback fixture: the
// CDP defaults run against a real Chromium on an httptest server — the
// navigation reports the final URL and title, and the settle (network-idle
// wait) completes on the loaded page. Skipped where no browser is installed —
// a test run must never download one.
func TestBrowser_NavigateAndSettle(t *testing.T) {
	requireTestBrowser(t)
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	url := f.URL + "/form"
	res := callHandler(t, b.handleNavigate, map[string]any{"url": url})
	require.False(t, res.IsError, "navigate: %s", resultText(t, res))
	var got NavigateResult
	mustJSON(t, res, &got)
	require.Equal(t, url, got.URL, "the reported URL is the loaded one")
	require.Equal(t, "Sign in", got.Title, "the reported title is the loaded one")

	wait := callHandler(t, b.handleWait, map[string]any{})
	require.False(t, wait.IsError, "wait: %s", resultText(t, wait))
	var settled WaitResult
	mustJSON(t, wait, &settled)
	require.True(t, settled.OK)
	require.Equal(t, url, settled.URL, "the wait reports the page URL")
}

// TestBrowser_NavigateToleratesCancelledTail pins the turn-stop race: the
// caller's context expires while the page is still settling — after the
// navigation itself already landed — and the tool must still answer the
// successful navigate result, not "page info: context canceled" (the model
// would retry a navigation that happened). The session is warmed first so
// the short deadline can only bite in the settle/tail window, never in the
// launch.
func TestBrowser_NavigateToleratesCancelledTail(t *testing.T) {
	requireTestBrowser(t)
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	navigate(t, b, f.URL+"/form") // warm session: launch is out of the timed path

	// The budget sits just above /slow's 1.5s load (so the navigation phase
	// completes) and well below settle's completion — the cancellation
	// reliably lands in the settle/tail window, never in the launch.
	slowURL := f.URL + "/slow"
	ctx, cancel := context.WithTimeout(context.Background(), 1800*time.Millisecond)
	defer cancel()
	res, err := b.handleNavigate(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: "navigate", Arguments: map[string]any{"url": slowURL}},
	})
	require.NoError(t, err, "domain failures are tool errors, never JSON-RPC errors")
	require.NotNil(t, res)
	require.False(t, res.IsError, "a landed navigation must not fail on a cancelled tail read: %s", resultText(t, *res))
	var got NavigateResult
	mustJSON(t, *res, &got)
	require.Equal(t, slowURL, got.URL, "the detached tail read still reports the landed URL")
}

// TestBrowser_TypePressKeysBack drives the trusted-input actions end to end:
// type focuses, selects, and inserts trusted text into the password field,
// press_key Enter submits the single-input form, and back walks the history —
// the typed value must reach the site exactly once, proving the whole
// trusted-event chain.
func TestBrowser_TypePressKeysBack(t *testing.T) {
	requireTestBrowser(t)
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	formURL := f.URL + "/form"
	navigate(t, b, formURL)

	res := callHandler(t, b.handleType, map[string]any{"name": "Password", "text": "type-check-42"})
	require.False(t, res.IsError, "type: %s", resultText(t, res))
	var typed SlimActionResult
	mustJSON(t, res, &typed)
	require.True(t, typed.OK)
	require.Equal(t, formURL, typed.URL, "typing stays on the form")

	// Enter on the form's only input submits it (implicit submission): the
	// keystroke must land on the focused field as a trusted event.
	res = callHandler(t, b.handlePressKey, map[string]any{"key": "Enter"})
	require.False(t, res.IsError, "press_key: %s", resultText(t, res))
	var pressed SlimActionResult
	mustJSON(t, res, &pressed)
	require.True(t, pressed.OK)
	require.Equal(t, f.URL+"/done", pressed.URL, "Enter submitted the form")
	require.Equal(t, "Welcome", pressed.Title)
	require.Equal(t, []string{"type-check-42"}, f.received,
		"the typed value reached the site exactly once")

	// back returns to the form and resets the refs.
	res = callHandler(t, b.handleBack, map[string]any{})
	require.False(t, res.IsError, "back: %s", resultText(t, res))
	var backed SlimActionResult
	mustJSON(t, res, &backed)
	require.True(t, backed.OK)
	require.Equal(t, formURL, backed.URL, "back returns to the form")
	require.Equal(t, "Sign in", backed.Title)

	// scroll answers the slim result like every interactive action.
	res = callHandler(t, b.handleScroll, map[string]any{"direction": "down"})
	require.False(t, res.IsError, "scroll: %s", resultText(t, res))
	var scrolled SlimActionResult
	mustJSON(t, res, &scrolled)
	require.True(t, scrolled.OK)
	require.Equal(t, formURL, scrolled.URL)
}

// TestBrowser_ClickSubmits is the trusted-click proof: clicking the submit
// button by accessible name dispatches real mouse events at the element's
// center and the POST navigation completes.
func TestBrowser_ClickSubmits(t *testing.T) {
	requireTestBrowser(t)
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	navigate(t, b, f.URL+"/form")

	res := callHandler(t, b.handleClick, map[string]any{"name": "Sign in"})
	require.False(t, res.IsError, "click: %s", resultText(t, res))
	var clicked SlimActionResult
	mustJSON(t, res, &clicked)
	require.True(t, clicked.OK)
	require.Equal(t, f.URL+"/done", clicked.URL, "the trusted click submitted the form")
	require.Equal(t, "Welcome", clicked.Title)
}
