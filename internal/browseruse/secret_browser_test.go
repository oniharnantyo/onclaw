package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	cdpage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// The loopback sign-in fixtures: the fill/scrub CDP defaults run against a
// real Chromium on an httptest server (design D10's loopback proof). Skipped
// where no browser is installed — the stub-level tests in secret_test.go
// carry the orchestration coverage without one.

// testCanary is the value the fixtures type; every test asserts it appears in
// no tool result the receipt path produces.
const testCanary = "canary-secret-42xQ"

// lookTestBrowser reports whether a Chromium binary is launchable: the
// CHROME_PATH override first, then the common install locations chromedp's
// allocator searches. It never downloads a browser: a test run must not
// fetch Chromium.
func lookTestBrowser() bool {
	if bin := os.Getenv("CHROME_PATH"); bin != "" {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}
	locations := []string{
		// macOS app bundles
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		// PATH names (unix-like), mirroring chromedp's own search
		"headless_shell",
		"headless-shell",
		"chromium",
		"chromium-browser",
		"google-chrome",
		"google-chrome-stable",
		"google-chrome-beta",
		"google-chrome-unstable",
		"chrome",
		"msedge",
	}
	for _, path := range locations {
		if _, err := exec.LookPath(path); err == nil {
			return true
		}
	}
	return false
}

// requireTestBrowser skips the test where there is no browser to launch.
func requireTestBrowser(t *testing.T) {
	t.Helper()
	if !lookTestBrowser() {
		t.Skip("no Chromium available for the loopback fixtures")
	}
}

// newBrowserTools returns browserTools over a private manager that launches
// real (headless) Chromium into a self-managed temp browser dir, with
// allow-private so the loopback http fixture is navigable. The dir is not
// t.TempDir(): Chromium writes asynchronously while it exits, and Go's strict
// TempDir cleanup would fail the test on the race.
func newBrowserTools(t *testing.T) *browserTools {
	t.Helper()
	requireTestBrowser(t)
	dir, err := os.MkdirTemp("", "buddies-browseruse-test-")
	require.NoError(t, err)
	cfg := DefaultConfig()
	cfg.BrowserDir = dir
	cfg.AllowPrivate = true
	b := &browserTools{cfg: cfg, mgr: newManager()}
	t.Cleanup(func() {
		_ = b.mgr.shutdownAll()
		_ = os.RemoveAll(dir) // best-effort: Chromium may still be flushing
	})
	return b
}

// mustJSON decodes a successful tool result's JSON text.
func mustJSON(t *testing.T, res mcp.CallToolResult, out any) {
	t.Helper()
	require.False(t, res.IsError, "unexpected tool error: %s", resultText(t, res))
	require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), out))
}

// navigate drives the navigate handler to the fixture URL and returns the
// result text.
func navigate(t *testing.T, b *browserTools, url string) string {
	t.Helper()
	res := callHandler(t, b.handleNavigate, map[string]any{"url": url})
	require.False(t, res.IsError, "navigate %s: %s", url, resultText(t, res))
	return resultText(t, res)
}

// settle waits for the page's network activity to quiet down, so a read right
// after a navigation sees the loaded document.
func settle(t *testing.T, b *browserTools) {
	t.Helper()
	res := callHandler(t, b.handleWait, map[string]any{})
	require.False(t, res.IsError, "settle: %s", resultText(t, res))
}

// snapshotRefs takes a snapshot and returns the elements by name (falling
// back to ref keys).
func snapshotRefs(t *testing.T, b *browserTools) map[string]SnapshotElement {
	t.Helper()
	res := callHandler(t, b.handleSnapshot, map[string]any{})
	var snap SnapshotResult
	mustJSON(t, res, &snap)
	byName := make(map[string]SnapshotElement, len(snap.Elements))
	for _, el := range snap.Elements {
		key := el.Ref
		if el.Name != "" {
			key = el.Name
		}
		byName[key] = el
	}
	return byName
}

// preparePassword drives __request_secret for one password target plus a
// click submit target and returns the ready receipt.
func preparePassword(t *testing.T, b *browserTools, field, submit string) secretReadyResult {
	t.Helper()
	res := callHandler(t, b.handleRequestSecret, map[string]any{
		"targets": []any{field}, "method": "password", "submission": "click",
		"submitTarget": submit,
	})
	var ready secretReadyResult
	require.NoError(t, json.Unmarshal([]byte(resultText(t, res)), &ready), "raw: %s", resultText(t, res))
	require.Equal(t, "ready", ready.Status, "prepare receipt: %s", resultText(t, res))
	return ready
}

// deliverValue drives __deliver_secret and returns the raw result text.
func deliverValue(t *testing.T, b *browserTools, requestID string) (string, mcp.CallToolResult) {
	t.Helper()
	res := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": requestID, "value": testCanary})
	return resultText(t, res), res
}

// fixtureServer is the loopback sign-in site. It never echoes the submitted
// value back into a page; it records what it received for the assertions.
type fixtureServer struct {
	*httptest.Server
	mu       sync.Mutex
	received []string
	loads    map[string]int
}

// record notes a submitted value server-side (the only place the test looks
// for it).
func (f *fixtureServer) record(v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.received = append(f.received, v)
}

// loadCount counts page loads of a path (the reload assertion).
func (f *fixtureServer) loadCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loads[path]
}

func newFixtureServer(t *testing.T) *fixtureServer {
	t.Helper()
	f := &fixtureServer{loads: map[string]int{}}
	mux := http.NewServeMux()
	count := func(path string) {
		f.mu.Lock()
		f.loads[path]++
		f.mu.Unlock()
	}
	mux.HandleFunc("/form", func(w http.ResponseWriter, _ *http.Request) {
		count("/form")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Sign in</title></head><body>
			<form method="POST" action="/done">
				<input id="pw" name="password" type="password" autocomplete="current-password" aria-label="Password">
				<button id="go" type="submit">Sign in</button>
			</form></body></html>`)
	})
	mux.HandleFunc("/done", func(w http.ResponseWriter, r *http.Request) {
		count("/done")
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			f.record(r.FormValue("password"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Welcome</title></head><body><h1>Welcome</h1></body></html>`)
	})
	// /slow trickles a page over ~1.5s so tests can cancel a navigation
	// mid-settle deterministically.
	mux.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) {
		count("/slow")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		time.Sleep(1500 * time.Millisecond)
		fmt.Fprint(w, `<!doctype html><html><head><title>Slow</title></head><body><h1>Slow page</h1></body></html>`)
	})
	mux.HandleFunc("/spa", func(w http.ResponseWriter, _ *http.Request) {
		count("/spa")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Code</title></head><body>
			<form id="f" onsubmit="submitSPA(event)">
			<input id="code" name="code" type="password" aria-label="Code">
			<button id="go" type="submit">Verify</button>
			<script>
			async function submitSPA(e) {
				e.preventDefault();
				const v = document.getElementById('code').value;
				await fetch('/record', { method: 'POST', body: v });
				history.pushState({}, '', '/spa#step2');
			}
			</script></body></html>`)
	})
	mux.HandleFunc("/record", func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		f.record(string(buf[:n]))
	})
	echoPage := func(shadow bool) string {
		mode := "body"
		if shadow {
			mode = "shadow"
		}
		return fmt.Sprintf(`<!doctype html><html><head><title>Echo %d</title></head><body>
			<form id="f">
			<input id="pw" name="password" type="password" aria-label="Password">
			<button id="go" type="submit" onclick="echo(event)">Send</button>
			<div id="host"></div>
			<script>
			async function echo(e) {
				e.preventDefault();
				const v = document.getElementById('pw').value;
				await fetch('/record', { method: 'POST', body: v });
				const where = %q;
				if (where === 'shadow') {
					document.getElementById('host').attachShadow({ mode: 'open' }).textContent = v;
				} else {
					document.body.appendChild(document.createTextNode(v));
				}
			}
			</script></body></html>`, f.loadCount("/echo"), mode)
	}
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		count("/echo")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, echoPage(r.URL.Query().Get("shadow") == "1"))
	})
	frames := func(w http.ResponseWriter, _ *http.Request) {
		count("/frames")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Framed</title></head><body>
			<iframe src="/form"></iframe></body></html>`)
	}
	mux.HandleFunc("/frames", frames)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Server.Close)
	return f
}

// TestBrowserSecret_ClickSubmission is the end-to-end loopback proof: prepare
// freezes inspection, delivery fills and submits, the POST navigates, and the
// canary appears in no tool result anywhere.
func TestBrowserSecret_ClickSubmission(t *testing.T) {
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	navigate(t, b, f.URL+"/form")
	refs := snapshotRefs(t, b)
	require.Contains(t, refs, "Password", "the snapshot lists the password field")

	ready := preparePassword(t, b, "Password", "Sign in")

	// The freeze is live between request and delivery.
	for _, tc := range []struct {
		name string
		h    func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args map[string]any
	}{
		{"snapshot", b.handleSnapshot, map[string]any{}},
		{"screenshot", b.handleScreenshot, map[string]any{}},
		{"cookies", b.handleCookies, map[string]any{"op": "list"}},
		{"read", b.handleRead, map[string]any{}},
	} {
		t.Run("frozen "+tc.name, func(t *testing.T) {
			res := callHandler(t, tc.h, tc.args)
			require.True(t, res.IsError)
			require.Contains(t, resultText(t, res), "Secure authentication is in progress")
		})
	}

	text, res := deliverValue(t, b, ready.RequestID)
	var got deliverCompletedResult
	mustJSON(t, res, &got)
	require.Equal(t, "completed", got.Status)
	require.NotContains(t, text, testCanary, "the receipt never carries the value")

	settle(t, b)
	require.Equal(t, 1, len(f.received), "the form submitted exactly once")
	require.Equal(t, testCanary, f.received[0], "the typed value reached the site")

	// The POST navigation released the protection entirely.
	read := callHandler(t, b.handleRead, map[string]any{})
	var page ReadResult
	mustJSON(t, read, &page)
	require.Contains(t, page.Text, "Welcome", "the browser is on the signed-in page")
	require.NotContains(t, page.Text+page.URL+page.Title, testCanary)
	require.True(t, f.loadCount("/done") >= 1)
}

// TestBrowserSecret_EnterSubmissionSPAKeep covers the enter submission and the
// clean same-document scrub: fields cleared, SPA-keep with the evaluate hold,
// released by the next real navigation.
func TestBrowserSecret_EnterSubmissionSPAKeep(t *testing.T) {
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	navigate(t, b, f.URL+"/spa")
	res := callHandler(t, b.handleRequestSecret, map[string]any{
		"targets": []any{"Code"}, "method": "password", "submission": "enter",
	})
	var ready secretReadyResult
	mustJSON(t, res, &ready)

	text, res := deliverValue(t, b, ready.RequestID)
	var got deliverCompletedResult
	mustJSON(t, res, &got)
	require.Equal(t, "completed", got.Status)
	require.NotContains(t, text, testCanary)

	settle(t, b)
	require.Equal(t, []string{testCanary}, f.received, "enter submitted the code to the SPA handler")

	// SPA-keep: the page stayed (same document), fields cleared, evaluate
	// hold active, inspection allowed again.
	read := callHandler(t, b.handleRead, map[string]any{})
	require.True(t, read.IsError, "the evaluate hold refuses read after SPA-keep")
	require.Contains(t, resultText(t, read), "until the next navigation")
	snap := callHandler(t, b.handleSnapshot, map[string]any{})
	require.False(t, snap.IsError, "inspection is allowed in the weak state")

	// The next real main-frame navigation ends the hold.
	navigate(t, b, f.URL+"/form")
	read = callHandler(t, b.handleRead, map[string]any{})
	var page ReadResult
	mustJSON(t, read, &page)
	require.NotContains(t, page.Text, testCanary, "the fields are cleared; the value is not on the page")
}

// TestBrowserSecret_EchoReload covers the scrub's echo fallback: the value is
// findable in the document (plain text node, then an open shadow root), so
// the page is replaced with a GET reload and the value appears nowhere.
func TestBrowserSecret_EchoReload(t *testing.T) {
	for _, tc := range []struct {
		name     string
		urlQuery string
	}{
		{"echo in a text node", ""},
		{"echo in an open shadow root", "?shadow=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixtureServer(t)
			b := newBrowserTools(t)

			navigate(t, b, f.URL+"/echo"+tc.urlQuery)
			ready := preparePassword(t, b, "Password", "Send")

			text, res := deliverValue(t, b, ready.RequestID)
			var got deliverCompletedResult
			mustJSON(t, res, &got)
			require.Equal(t, "completed", got.Status)
			require.NotContains(t, text, testCanary)

			require.Equal(t, []string{testCanary}, f.received, "the value reached the site once")
			require.Equal(t, 2, f.loadCount("/echo"), "the scrub reloaded the document")

			settle(t, b)

			// The reload is a real navigation: protection is gone, and the
			// replaced document carries no value.
			read := callHandler(t, b.handleRead, map[string]any{})
			var page ReadResult
			mustJSON(t, read, &page)
			require.False(t, strings.Contains(page.Text, testCanary), "the reloaded page carries no value")
		})
	}
}

// TestBrowserSecret_PrepareRefusals covers the prepare validations against
// the live browser: framed targets and same-site tabs. The framed case runs
// at probe level — the AX snapshot of this fixture cannot address the iframe
// content, so the full prepare would refuse unresolved before the probe;
// the probe-level check pins the framed detection itself.
func TestBrowserSecret_PrepareRefusals(t *testing.T) {
	t.Run("a target inside an iframe probes framed", func(t *testing.T) {
		f := newFixtureServer(t)
		b := newBrowserTools(t)
		navigate(t, b, f.URL+"/frames")

		s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, "default", b.cfg)
		require.NoError(t, err)
		pageCtx := s.pageCtx
		var ref string
		require.NoError(t, chromedp.Do(pageCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			// The frame tree names the iframe's child frame; DOM.getFrameOwner
			// resolves its owner node, and a call bound to that node steps
			// into the iframe's document and addresses the password input,
			// mirroring what a cross-frame snapshot would hand back.
			tree, err := cdp.Call(ctx, t, cdpage.GetFrameTree, cdp.Empty{})
			if err != nil {
				return err
			}
			if len(tree.FrameTree.ChildFrames) == 0 {
				return errors.New("fixture page has no child frame")
			}
			owner, err := cdp.Call(ctx, t, dom.GetFrameOwner, dom.GetFrameOwnerParams{
				FrameID: tree.FrameTree.ChildFrames[0].Frame.ID,
			})
			if err != nil {
				return err
			}
			iframe, err := cdp.Call(ctx, t, dom.ResolveNode, dom.ResolveNodeParams{
				BackendNodeID: owner.BackendNodeID,
			})
			if err != nil {
				return err
			}
			if iframe.Object == nil {
				return errors.New("the iframe node did not resolve")
			}
			inner, err := cdp.Call(ctx, t, runtime.CallFunctionOn, runtime.CallFunctionOnParams{
				ObjectID:            iframe.Object.ObjectID,
				FunctionDeclaration: `function() { return this.contentDocument.querySelector('#pw'); }`,
			})
			if err != nil {
				return err
			}
			if inner.ExceptionDetails != nil || inner.Result == nil || inner.Result.ObjectID == "" {
				return errors.New("the iframe's password field did not resolve")
			}
			node, err := cdp.Call(ctx, t, dom.DescribeNode, dom.DescribeNodeParams{ObjectID: inner.Result.ObjectID})
			if err != nil {
				return err
			}
			if node.Node == nil {
				return errors.New("the iframe's password field did not describe")
			}
			ref = fmt.Sprintf("e%d", node.Node.BackendNodeID)
			return nil
		})))
		probe, err := defaultSecretProbe(context.Background(), pageCtx, ref)
		release()
		require.NoError(t, err)
		require.True(t, probe.Framed, "a target inside an iframe probes framed")
		require.NoError(t, s.guardNavigation("about:blank"), "sanity: guard untouched")
	})

	t.Run("a second same-site tab breaks isolation", func(t *testing.T) {
		f := newFixtureServer(t)
		b := newBrowserTools(t)
		navigate(t, b, f.URL+"/form")

		// Open a neighbor tab showing the same host.
		s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, "default", b.cfg)
		require.NoError(t, err)
		neighborCtx, cancelNeighbor := chromedp.NewContext(s.browserCtx)
		require.NoError(t, chromedp.Do(neighborCtx, chromedp.Navigate(f.URL+"/spa")))
		release()
		defer cancelNeighbor()

		res := callHandler(t, b.handleRequestSecret, map[string]any{
			"targets": []any{"Password"}, "method": "password", "submission": "click", "submitTarget": "Sign in",
		})
		var got unavailableResult
		mustJSON(t, res, &got)
		require.Equal(t, "unavailable", got.Status)
		require.Equal(t, reasonIsolation, got.ReasonCode)
	})
}

// TestBrowserSecret_FingerprintChanged covers the fail-safe re-check: after a
// ready prepare, a swapped form fails the delivery without typing anything.
func TestBrowserSecret_FingerprintChanged(t *testing.T) {
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	url := f.URL + "/form"
	navigate(t, b, url)
	ready := preparePassword(t, b, "Password", "Sign in")

	// Swap the form under the parked request: replace the password input.
	s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, "default", b.cfg)
	require.NoError(t, err)
	pageCtx := s.pageCtx
	err = chromedp.Do(pageCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, runtime.Evaluate, runtime.EvaluateParams{
			Expression: `(() => {
				const old = document.getElementById('pw');
				const next = document.createElement('textarea');
				next.id = 'pw';
				next.name = 'password';
				old.replaceWith(next);
			})()`,
		})
		if err != nil {
			return err
		}
		if res.ExceptionDetails != nil {
			return errors.New("the form swap failed")
		}
		return nil
	}))
	release()
	require.NoError(t, err)

	text, res := deliverValue(t, b, ready.RequestID)
	require.False(t, res.IsError)
	require.Contains(t, text, reasonTargetChanged)
	require.NotContains(t, text, testCanary)
	require.Empty(t, f.received, "nothing was typed into the swapped form")

	// A second delivery is inert: secrets are not retried.
	again := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": ready.RequestID, "value": testCanary})
	require.Equal(t, inertRefusal, resultText(t, again))

	// The failed delivery keeps the freeze until resolution.
	read := callHandler(t, b.handleRead, map[string]any{})
	require.True(t, read.IsError)
	require.True(t, b.mgr.resolveAttention(ready.RequestID), "the freeze stays resolvable after the failure")
	read = callHandler(t, b.handleRead, map[string]any{})
	require.False(t, read.IsError, "resolution releases the freeze")
}

// TestBrowserSecret_TakeoverHeadedRelaunch covers the takeover flow against
// the live manager: the relaunch swaps the live browser, the session holds,
// and resolution hands it back. The relaunch runs headed per design D8, so
// this test is best-effort about window focus and only asserts the manager
// facts.
func TestBrowserSecret_TakeoverHeadedRelaunch(t *testing.T) {
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	navigate(t, b, f.URL+"/form")
	res := callHandler(t, b.handleRequestTakeover, map[string]any{"reason": "sign-in-blocked"})
	var ready takeoverReadyResult
	mustJSON(t, res, &ready)
	require.Equal(t, "ready", ready.Status)

	begin := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": ready.RequestID})
	var active takeoverActiveResult
	mustJSON(t, begin, &active)
	require.Equal(t, "active", active.Status)

	// The headed window landed on the page the agent was browsing, not the
	// launch blank (the person takes over the page in front of them).
	require.Eventually(t, func() bool {
		s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, b.cfg.DefaultProfile, b.cfg)
		require.NoError(t, err)
		url, _ := pageInfo(s.pageCtx)
		release()
		return url == f.URL+"/form"
	}, 15*time.Second, 100*time.Millisecond, "the headed relaunch carries the browsed URL over")

	// The held session refuses everything with the fixed refusal.
	held := callHandler(t, b.handleSnapshot, map[string]any{})
	require.True(t, held.IsError)
	require.Equal(t, heldRefusal, resultText(t, held))

	// Resolution ends the hold and hands the page back.
	resolve := callHandler(t, b.handleResolveAttention, map[string]any{
		"requestID": ready.RequestID, "outcome": "completed",
	})
	var resolved attentionResolvedResult
	mustJSON(t, resolve, &resolved)
	require.Equal(t, "resolved", resolved.Status)

	// The relaunched (headed) browser is live again for inspection.
	navigate(t, b, f.URL+"/form")
	snap := callHandler(t, b.handleSnapshot, map[string]any{})
	var after SnapshotResult
	mustJSON(t, snap, &after)
	require.NotEmpty(t, after.Elements, "the headed browser is live again for inspection")

	// Takeover is not retried: the ID is spent.
	again := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": ready.RequestID})
	require.Equal(t, inertRefusal, resultText(t, again))
}

// TestBrowserSecret_TakeoverExplicitURL pins the agent-named landing page:
// an explicit url argument on request_takeover wins over the session's
// current page — the headed window opens the page the person must act on.
func TestBrowserSecret_TakeoverExplicitURL(t *testing.T) {
	f := newFixtureServer(t)
	b := newBrowserTools(t)

	// The session sits on /first; the person is needed on /form.
	navigate(t, b, f.URL+"/first")
	res := callHandler(t, b.handleRequestTakeover, map[string]any{
		"reason": "sign-in-blocked", "url": f.URL + "/form",
	})
	var ready takeoverReadyResult
	mustJSON(t, res, &ready)
	require.Equal(t, "ready", ready.Status)

	begin := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": ready.RequestID})
	var active takeoverActiveResult
	mustJSON(t, begin, &active)
	require.Equal(t, "active", active.Status)

	require.Eventually(t, func() bool {
		s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, b.cfg.DefaultProfile, b.cfg)
		require.NoError(t, err)
		url, _ := pageInfo(s.pageCtx)
		release()
		return url == f.URL+"/form"
	}, 15*time.Second, 100*time.Millisecond, "the headed window lands on the agent-named url")

	// The hold is live as usual.
	held := callHandler(t, b.handleSnapshot, map[string]any{})
	require.True(t, held.IsError)
	require.Equal(t, heldRefusal, resultText(t, held))
}
