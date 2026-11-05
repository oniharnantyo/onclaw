package browseruse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// networkIdleWindow is how long the network must stay quiet for a page to
// count as settled after a navigation or an interactive action.
const networkIdleWindow = time.Second

// launchStepTimeout bounds each post-attach step of a launch (protocol attach
// probe, cookie-jar inject, first-tab open). They run on their own derived
// contexts so the browser outlives the tool call that launched it; a Chromium
// wedged before its first answer must still fail the launch eventually.
const launchStepTimeout = 30 * time.Second

// terminateGrace is how long the graceful teardown waits for Chromium to exit
// after SIGTERM before the process group is SIGKILLed.
const terminateGrace = 3 * time.Second

// watchdogInterval is how often the parent-liveness watchdog polls. The
// leakless replacement (design D3) guarantees no Chromium outlives the
// browser-use child's parent by more than one interval.
const watchdogInterval = 2 * time.Second

// parentPID is the pid the launch watchdog treats as the browser-use child's
// parent: os.Getppid() at process start. A var so tests can exercise the
// crash path without killing the test runner's own parent.
var parentPID = os.Getppid()

// devtoolsMarker prefixes the line Chromium prints on stderr once its
// DevTools server listens.
const devtoolsMarker = "DevTools listening on "

// browserBinNames are the Chromium binary names looked up on PATH when
// CHROME_PATH is unset, in preference order per platform (the same spread the
// rod launcher searched).
func browserBinNames() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	case "windows":
		return []string{"chrome", "edge", "chromium", "chromium-browser"}
	default:
		return []string{
			"chrome",
			"google-chrome",
			"/usr/bin/google-chrome",
			"microsoft-edge",
			"/usr/bin/microsoft-edge",
			"chromium",
			"chromium-browser",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	}
}

// browserBinPath resolves the browser binary: CHROME_PATH verbatim (used
// directly, so a bad value fails at exec like the rod launcher's Bin did),
// otherwise a LookPath walk over the common names.
func browserBinPath() (string, error) {
	if bin := os.Getenv("CHROME_PATH"); bin != "" {
		return bin, nil
	}
	for _, name := range browserBinNames() {
		if bin, err := exec.LookPath(name); err == nil {
			return bin, nil
		}
	}
	return "", errors.New("start chromium (set CHROME_PATH to a valid browser binary): no browser binary found")
}

// launchChromiumSession launches Chromium on dir as its user-data directory
// and opens the profile's single page. The child is started and owned
// directly — chromedp only attaches over the DevTools websocket — so the
// process group can be killed on teardown and a parent-liveness watchdog can
// force-kill it after a crash (the leakless replacement, design D3). Cookie
// jar inject-before-first-use order (design D7) is preserved: the jar is
// restored after the attach, before the about:blank page exists.
func launchChromiumSession(ctx context.Context, dir string, cfg Config) (*profileSession, error) {
	bin, err := browserBinPath()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, chromiumArgs(dir, cfg)...)
	// stdout is the MCP JSON-RPC transport: route both of Chromium's output
	// streams into our stderr, which the parent only surfaces as failure
	// excerpts, and capture the DevTools endpoint from the same bytes.
	log := &devtoolsLog{found: make(chan struct{})}
	cmd.Stdout = log
	cmd.Stderr = log

	group, err := startProcGroup(cmd)
	if err != nil {
		return nil, fmt.Errorf("start chromium (set CHROME_PATH to a valid browser binary): %w", err)
	}
	proc := newBrowserProc(cmd, group)

	wsURL, err := waitDevToolsEndpoint(ctx, proc, log)
	if err != nil {
		_ = proc.teardown()
		return nil, err
	}

	// Attach chromedp to the running browser. The allocator context is
	// backgrounded on purpose: the browser must outlive the tool call that
	// launched it (the rod launcher connected the same way), so nothing in
	// the session derives from ctx past this point. The first chromedp call
	// anchors the websocket to its context and the page attach anchors the
	// target session to its context, so neither step may run on a timeout
	// context — cancelling those would drop the connection or detach the
	// session. The launch budget bounds both steps from the outside instead.
	allocCtx, cancelAlloc := remote.NewAllocator(context.Background(), wsURL)
	proc.addCancel(cancelAlloc)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	proc.addCancel(cancelBrowser)

	attachErr := callBounded(launchStepTimeout, func() error {
		_, err := chromedp.CallBrowser(browserCtx, browser.GetVersion, cdp.Empty{})
		return err
	})
	if attachErr != nil {
		_ = proc.teardown()
		return nil, fmt.Errorf("connect to chromium: %w", attachErr)
	}

	// Restore the cookies saved at the previous teardown before anything can
	// observe the profile; missing or corrupt jars are not an error. The jar
	// runs over the browser endpoint, whose session is anchored above, so a
	// timeout context is safe here.
	injectCtx, cancelInject := context.WithTimeout(browserCtx, launchStepTimeout)
	err = injectCookieJar(injectCtx, browserCtx, dir)
	cancelInject()
	if err != nil {
		_ = proc.teardown()
		return nil, err
	}

	// The profile's single page: one about:blank tab, opened at launch like
	// the rod launcher did, so every session has exactly one target. The
	// attach anchors the target session to pageCtx, so this step also gets
	// its bound from the outside.
	pageCtx, cancelPage := chromedp.NewContext(browserCtx)
	proc.addCancel(cancelPage)
	openErr := callBounded(launchStepTimeout, func() error {
		return chromedp.Do(pageCtx)
	})
	if openErr != nil {
		_ = proc.teardown()
		return nil, fmt.Errorf("open page: %w", openErr)
	}

	proc.startWatchdog()

	return &profileSession{
		browserCtx: browserCtx,
		pageCtx:    pageCtx,
		proc:       proc,
		dir:        dir,
		closeFn:    proc.teardown,
		aliveFn: func() bool {
			return probeSessionAlive(pageCtx)
		},
		exportFn: func() error {
			return exportCookieJar(context.Background(), browserCtx, dir)
		},
	}, nil
}

// chromiumArgs builds the Chromium command line: the profile directory, the
// debugging port on a random local port, and the flag set the rod launcher
// sent (minus leakless, which the process-group ownership replaces).
func chromiumArgs(dir string, cfg Config) []string {
	args := []string{
		"--user-data-dir=" + dir,
		"--remote-debugging-port=0",
		"--no-first-run",
		"--no-startup-window",
		"--no-default-browser-check",
		// After Puppeteer's defaults, as the rod launcher sent them.
		"--disable-features=site-per-process,TranslateUI",
		"--disable-dev-shm-usage",
		"--disable-background-networking",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-breakpad",
		"--disable-client-side-phishing-detection",
		"--disable-component-extensions-with-background-pages",
		"--disable-default-apps",
		"--disable-hang-monitor",
		"--disable-ipc-flooding-protection",
		"--disable-popup-blocking",
		"--disable-prompt-on-repost",
		"--disable-renderer-backgrounding",
		"--disable-sync",
		"--disable-site-isolation-trials",
		"--enable-automation",
		"--enable-features=NetworkService,NetworkServiceInProcess",
		"--force-color-profile=srgb",
		"--metrics-recording-only",
		"--use-mock-keychain",
	}
	if cfg.Headless {
		args = append(args, "--headless")
	}
	if inContainer() {
		args = append(args, "--no-sandbox")
	}
	return args
}

// inContainer reports whether the server runs inside a container, where
// Chromium needs --no-sandbox (the same detection the rod launcher did).
func inContainer() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	for _, marker := range []string{"/.dockerenv", "/.kubepod"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

// devtoolsLog mirrors Chromium's output into our stderr and captures the
// DevTools websocket endpoint the browser prints at startup.
type devtoolsLog struct {
	mu    sync.Mutex
	buf   []byte
	wsURL string
	found chan struct{}
}

// Write inspects complete lines for the endpoint and forwards everything to
// os.Stderr, so no Chromium byte can reach the MCP JSON-RPC transport.
func (l *devtoolsLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.buf = append(l.buf, p...)
	for {
		idx := bytes.IndexByte(l.buf, '\n')
		if idx < 0 {
			// Keep a bounded remainder: a line without an endpoint is
			// worthless once it grows past any plausible Chromium line.
			if len(l.buf) > 8192 {
				l.buf = nil
			}
			break
		}
		line := string(l.buf[:idx])
		l.buf = l.buf[idx+1:]
		if l.wsURL != "" {
			continue
		}
		at := strings.Index(line, devtoolsMarker)
		if at < 0 {
			continue
		}
		if url := strings.TrimSpace(line[at+len(devtoolsMarker):]); strings.HasPrefix(url, "ws://") {
			l.wsURL = url
			close(l.found)
		}
	}
	l.mu.Unlock()
	_, _ = os.Stderr.Write(p)
	return len(p), nil
}

// endpoint returns the captured websocket URL once Chromium prints it.
func (l *devtoolsLog) endpoint() (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.wsURL, l.wsURL != ""
}

// waitDevToolsEndpoint blocks until the browser prints its DevTools endpoint,
// the launch context is cancelled, or the child exits without printing one.
func waitDevToolsEndpoint(ctx context.Context, proc *browserProc, log *devtoolsLog) (string, error) {
	select {
	case <-log.found:
	case <-ctx.Done():
		return "", fmt.Errorf("start chromium: %w", ctx.Err())
	case <-proc.exited():
		if url, ok := log.endpoint(); ok {
			return url, nil
		}
		return "", errors.New("start chromium (set CHROME_PATH to a valid browser binary): exited before printing its DevTools endpoint")
	}
	url, _ := log.endpoint()
	return url, nil
}

// callBounded runs f on its own goroutine and bounds it by d. The launch
// steps that chromedp anchors to a context (the websocket, the target
// session) cannot take context timeouts — cancelling those contexts would
// drop the browser or detach the page session — so their budget must come
// from outside. On timeout the goroutine drains into teardown's cancels.
func callBounded(d time.Duration, f func() error) error {
	ch := make(chan error, 1)
	go func() { ch <- f() }()
	select {
	case err := <-ch:
		return err
	case <-time.After(d):
		return fmt.Errorf("did not finish within %s", d)
	}
}

// browserProc is the Chromium child a launch owns: its command, process
// group, chromedp context cancels, and the parent-liveness watchdog. All of
// it is built by launchChromiumSession; teardown is the single graceful path,
// and the watchdog the single crash path (design D3).
type browserProc struct {
	cmd   *exec.Cmd
	group groupHandle

	// cancels holds the chromedp context cancels in creation order
	// (allocator, browser, page); teardown runs them in reverse so the tab
	// closes before the browser connection drops. Kept alongside the
	// contexts so teardown is explicit and ordered.
	cancelsMu sync.Mutex
	cancels   []context.CancelFunc

	// exitedCh closes when cmd.Wait returns; the reaper goroutine below is
	// the only Wait caller, and waitErr is read only after the close.
	exitedCh chan struct{}
	waitErr  error

	// watchCtx stops the watchdog: teardown cancels it, and the watchdog
	// cancels it itself after a forced kill. watchParent is the parent pid
	// the liveness poll checks (parentPID at launch time).
	watchCtx    context.Context
	watchStop   context.CancelFunc
	watchParent int
}

// newBrowserProc wraps a started child: it records the process group and
// starts the reaper goroutine that owns cmd.Wait.
func newBrowserProc(cmd *exec.Cmd, group groupHandle) *browserProc {
	p := &browserProc{
		cmd:         cmd,
		group:       group,
		exitedCh:    make(chan struct{}),
		watchParent: parentPID,
	}
	go func() {
		p.waitErr = cmd.Wait() // waits out the stdout/stderr copy goroutines
		close(p.exitedCh)
	}()
	p.watchCtx, p.watchStop = context.WithCancel(context.Background())
	return p
}

// addCancel registers a chromedp context cancel for teardown.
func (p *browserProc) addCancel(cancel context.CancelFunc) {
	p.cancelsMu.Lock()
	defer p.cancelsMu.Unlock()
	p.cancels = append(p.cancels, cancel)
}

// cancelContexts runs the registered cancels in reverse creation order.
func (p *browserProc) cancelContexts() {
	p.cancelsMu.Lock()
	cancels := make([]context.CancelFunc, len(p.cancels))
	copy(cancels, p.cancels)
	p.cancelsMu.Unlock()
	for i := len(cancels) - 1; i >= 0; i-- {
		cancels[i]()
	}
}

// exited reports the channel closed when the child has been reaped.
func (p *browserProc) exited() <-chan struct{} {
	return p.exitedCh
}

// reap waits for the child to exit exactly once and returns its wait result.
// Any exit status is teardown success: the kills are ours.
func (p *browserProc) reap() error {
	<-p.exitedCh
	return p.waitErr
}

// startWatchdog runs the crash-path goroutine (design D3): while the session
// lives it polls the browser-use child's parent liveness, and when the parent
// disappears it cancels the chromedp contexts and force-kills the Chromium
// process group, so no Chromium outlives the parent by more than one
// watchdogInterval. A child whose browser already exited has nothing to
// guard, so the watchdog stops.
func (p *browserProc) startWatchdog() {
	go func() {
		defer p.watchStop()
		ticker := time.NewTicker(watchdogInterval)
		defer ticker.Stop()
		for {
			select {
			case <-p.watchCtx.Done():
				return
			case <-p.exitedCh:
				return
			case <-ticker.C:
			}
			if !parentDied(p.watchParent) {
				continue
			}
			p.cancelContexts()
			_ = forceKillGroup(p.group, p.cmd.Process.Pid)
			return
		}
	}()
}

// teardown is the graceful path (design D3): cancel the chromedp contexts
// (releases the CDP session and the websocket), then terminate the process
// group — SIGTERM first so Chromium exits cleanly and flushes its profile
// store, SIGKILL for whatever outlives the grace — and reap the child. A
// browser that already died is not an error. Safe to call more than once.
func (p *browserProc) teardown() error {
	p.watchStop()
	p.cancelContexts()
	if err := terminateGroup(p.group, p.cmd.Process.Pid, terminateGrace); err != nil {
		return fmt.Errorf("browser: terminate chromium process group: %w", err)
	}
	p.reap() // any exit status is success once the kills are done
	return nil
}

// killNow crashes the browser the way a person closing the window or a hard
// kill would: the whole process group dies immediately and the child is
// reaped, while the session keeps its now-dead handles. Test seam for the
// dead-session relaunch path.
func (p *browserProc) killNow() error {
	_ = forceKillGroup(p.group, p.cmd.Process.Pid)
	p.reap()
	return nil
}
