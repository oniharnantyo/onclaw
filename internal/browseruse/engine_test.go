package browseruse

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/stretchr/testify/require"
)

// The engine loopback proof (tasks 1.2/1.3): the launch pipeline drives a
// real Chromium end to end, and the leakless replacement holds — graceful
// teardown kills the process group, and the parent-liveness watchdog reaps
// Chromium after the crash path. Skipped where no browser binary exists (the
// same rule as the loopback sign-in fixtures: a test run never downloads
// one).

// requireEngineBrowser skips the test where launch would find no browser.
func requireEngineBrowser(t *testing.T) {
	t.Helper()
	if _, err := browserBinPath(); err != nil {
		t.Skip("no Chromium available for the engine loopback fixtures")
	}
}

// engineTestDir creates the browser root the fixtures launch into. It is not
// t.TempDir(): Chromium writes asynchronously while it exits, and Go's strict
// TempDir cleanup would fail the test on the race (the newBrowserTools rule).
func engineTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "buddies-engine-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) // best-effort: Chromium may still be flushing
	return dir
}

// launchEngineSession launches a real headless Chromium on dir and wires
// best-effort teardown into the test cleanup.
func launchEngineSession(t *testing.T, dir string) *profileSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := launchChromiumSession(ctx, dir, Config{Headless: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.closeFn() })
	return s
}

// runOnPage derives a run context for direct engine actions on the session's
// page, bounded by a test-local deadline.
func runOnPage(t *testing.T, s *profileSession) (context.Context, *chromedp.Target) {
	t.Helper()
	tgt, ok := pageTarget(s.pageCtx)
	require.True(t, ok, "the launch opened the single about:blank target")
	ctx, cancel := context.WithTimeout(s.pageCtx, 15*time.Second)
	t.Cleanup(cancel)
	return ctx, tgt
}

func TestEngine_LoopbackLifecycle(t *testing.T) {
	requireEngineBrowser(t)

	s := launchEngineSession(t, engineTestDir(t))
	require.True(t, probeSessionAlive(s.pageCtx), "the session's page target answers right after launch")

	ctx, tgt := runOnPage(t, s)
	_, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()

	// Navigate to a data: URL and evaluate JS on it.
	dataURL := "data:text/html,<html><head><title>engine-fixture</title></head><body><p>engine-loopback</p></body></html>"
	require.NoError(t, chromedp.Do(ctx, chromedp.Navigate(dataURL)))
	title, err := chromedp.Run(ctx, chromedp.Evaluate[string]("document.title"))
	require.NoError(t, err)
	require.Equal(t, "engine-fixture", title)

	// Screenshot: a decodable PNG with real dimensions.
	shot, err := cdp.Call(ctx, tgt, page.CaptureScreenshot, page.CaptureScreenshotParams{Format: page.CaptureScreenshotFormatPng})
	require.NoError(t, err)
	require.NotEmpty(t, shot.Data)
	dim, err := png.DecodeConfig(bytes.NewReader(shot.Data))
	require.NoError(t, err, "the screenshot is a decodable PNG")
	require.Positive(t, dim.Width)
	require.Positive(t, dim.Height)

	// Cookie set, then read back through the same session.
	_, err = cdp.Call(ctx, tgt, network.SetCookies, network.SetCookiesParams{Cookies: []*network.CookieParam{{
		Name:  "engine-probe",
		Value: "1",
		URL:   "http://example.com/",
	}}})
	require.NoError(t, err)
	got, err := cdp.Call(ctx, tgt, network.GetCookies, network.GetCookiesParams{URLs: []string{"http://example.com/"}})
	require.NoError(t, err)
	var found bool
	for _, c := range got.Cookies {
		if c.Name == "engine-probe" {
			found = true
		}
	}
	require.True(t, found, "the cookie set over CDP reads back")

	// Graceful close: the whole process group goes away and the child is
	// reaped.
	pid := s.proc.cmd.Process.Pid
	require.NoError(t, s.closeFn())
	require.False(t, pidAlive(pid), "the Chromium main process is gone after teardown")
	require.False(t, groupAlive(s.proc.group, pid), "no process remains in the browser's group")
	require.False(t, probeSessionAlive(s.pageCtx), "the torn-down session probes dead")
	_ = s.proc.reap()

	// A second launch on the same profile directory works (the relaunch and
	// headed-swap paths rely on it).
	s2 := launchEngineSession(t, s.dir)
	require.True(t, probeSessionAlive(s2.pageCtx), "the profile directory relaunches after a clean teardown")
}

func TestEngine_LaunchFailsWithoutBrowser(t *testing.T) {
	t.Setenv("CHROME_PATH", filepath.Join(t.TempDir(), "no-such-browser"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := launchChromiumSession(ctx, t.TempDir(), Config{Headless: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "CHROME_PATH", "the launch failure must name the override")
	require.Contains(t, err.Error(), "start chromium")
}

func TestEngine_WatchdogReapsChromiumAfterParentDeath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the watchdog kill path is exercised on darwin/linux (design D3); windows relies on the Job Object")
	}
	requireEngineBrowser(t)

	// Simulate the crash path: the browser-use child's parent is gone. The
	// dead pid is the lock_test.go convention (above any real pid).
	old := parentPID
	parentPID = 4_000_000_000
	t.Cleanup(func() { parentPID = old })

	s := launchEngineSession(t, engineTestDir(t))
	require.True(t, probeSessionAlive(s.pageCtx))
	pid := s.proc.cmd.Process.Pid

	// LONG WAIT: the watchdog polls every watchdogInterval, so Chromium must
	// be reaped within about two of them (design D3's contract). Six seconds
	// covers two intervals plus kill and reap latency on a loaded machine.
	deadline := time.Now().Add(2*watchdogInterval + 2*time.Second)
	for time.Now().Before(deadline) {
		if !groupAlive(s.proc.group, pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.False(t, groupAlive(s.proc.group, pid),
		"the watchdog must force-kill the browser group after the parent disappears")
	require.False(t, pidAlive(pid), "the watchdog must reap Chromium")

	// The watchdog signals its own exit through watchCtx, which makes the
	// parentPID restore below race-clean under -race.
	select {
	case <-s.proc.watchCtx.Done():
	case <-time.After(watchdogInterval + time.Second):
		t.Fatal("the watchdog goroutine must stop after firing")
	}
}

func TestEngine_WatchdogKillPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the watchdog kill path is exercised on darwin/linux (design D3); windows relies on the Job Object")
	}

	// Hermetic form of the crash path: any process group stands in for
	// Chromium, so the watchdog mechanics are proven without a browser. The
	// fake dead parent must be in place before the proc captures it.
	old := parentPID
	parentPID = 4_000_000_000
	t.Cleanup(func() { parentPID = old })

	cmd := exec.Command("sleep", "30")
	group, err := startProcGroup(cmd)
	require.NoError(t, err)
	proc := newBrowserProc(cmd, group)
	t.Cleanup(func() { _ = proc.teardown() })

	proc.startWatchdog()

	// LONG WAIT: two watchdog intervals plus kill and reap slack.
	deadline := time.Now().Add(2*watchdogInterval + 2*time.Second)
	for time.Now().Before(deadline) {
		if !groupAlive(group, cmd.Process.Pid) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.False(t, groupAlive(group, cmd.Process.Pid), "the watchdog must kill the process group")
	require.False(t, pidAlive(cmd.Process.Pid), "the watched child must be reaped")

	select {
	case <-proc.watchCtx.Done():
	case <-time.After(watchdogInterval + time.Second):
		t.Fatal("the watchdog goroutine must stop after firing")
	}
}
