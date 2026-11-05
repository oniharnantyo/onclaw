package browseruse

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// The secret-hygiene integration proof (add-browser-login-handover tasks.md
// 7.4, design D1/D10): the real protocol — __request_secret → __deliver_secret
// → receipt — against a real Chromium on loopback fixtures, with a unique
// canary value, asserting after each full flow that the typed value appears in
// no tool result, no captured log byte (slog and the launcher's stderr
// routing), and no
// file the child wrote (screenshots, cookie jar), and that the page itself
// carries no echo after the scrub (including open shadow roots). The takeover
// endings are exercised at the end; no secret is involved there, so the
// canary checks do not apply to them.

// hygieneCanary returns the run-unique canary value the fixtures type.
func hygieneCanary(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return "BUDDIES-CANARY-" + hex.EncodeToString(b)
}

// syncBuffer is a concurrency-safe byte sink for captured log output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureSlog swaps the process default slog logger for one that records
// every record into the returned buffer (restored at cleanup).
func captureSlog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// captureStderr redirects os.Stderr into the returned buffer, so the
// launcher's Chromium output (routed to os.Stderr in launch.go) and anything
// else the process writes during the flow is captured. Restored at cleanup; the
// cleanup is registered before newBrowserTools, so Chromium's own inherited
// stderr fd closes (browser shutdown) before the pipe write end does.
func captureStderr(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	t.Cleanup(func() {
		os.Stderr = orig
		_ = w.Close()
		<-done
		_ = r.Close()
	})
	return buf
}

// deliverCanary drives __deliver_secret with the canary and returns the raw
// result text (secret_browser_test.go's deliverValue is pinned to the shared
// const canary; the hygiene proof needs a per-run value).
func deliverCanary(t *testing.T, b *browserTools, requestID, canary string) (string, mcp.CallToolResult) {
	t.Helper()
	res := callHandler(t, b.handleDeliverSecret, map[string]any{"requestID": requestID, "value": canary})
	return resultText(t, res), res
}

// requestSecret drives __request_secret, records its result text, and decodes
// the ready receipt.
func requestSecret(t *testing.T, b *browserTools, record func(mcp.CallToolResult), args map[string]any) secretReadyResult {
	t.Helper()
	res := callHandler(t, b.handleRequestSecret, args)
	record(res)
	var ready secretReadyResult
	mustJSON(t, res, &ready)
	require.Equal(t, "ready", ready.Status, "prepare receipt: %s", resultText(t, res))
	return ready
}

// scanPage runs the child's own scrub predicate against the live document
// (title, URL, text, values, attributes, open shadow roots) and answers
// whether the canary is still findable anywhere on the page.
func scanPage(t *testing.T, b *browserTools, canary string) bool {
	t.Helper()
	s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, DefaultBrowserProfile, b.cfg)
	require.NoError(t, err)
	page := s.pageCtx
	found, err := defaultScanForValue(context.Background(), page, canary)
	release()
	require.NoError(t, err, "post-flow scrub probe")
	return found
}

// forceJarSave exports the live cookies to the profile's jar file, so the
// on-disk file the child writes can be byte-grepped.
func forceJarSave(t *testing.T, b *browserTools) string {
	t.Helper()
	s, release, err := b.mgr.acquire(context.Background(), b.cfg.BrowserDir, DefaultBrowserProfile, b.cfg)
	require.NoError(t, err)
	dir := s.dir
	s.saveCookieJar()
	release()
	jar := cookieJarPath(dir)
	require.FileExists(t, jar, "the live session exports its cookie jar on demand")
	return jar
}

// assertNoCanaryInFile byte-greps one file for the canary.
func assertNoCanaryInFile(t *testing.T, path, canary string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), canary, "file %s must not carry the typed value", path)
}

// assertNoCanaryUnder byte-greps every regular file under dir. A missing dir
// means the flow wrote nothing there — trivially clean.
func assertNoCanaryUnder(t *testing.T, dir, canary string) {
	t.Helper()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		assertNoCanaryInFile(t, path, canary)
		return nil
	})
	require.NoError(t, err)
}

// assertCanaryNowhere is the whole-flow hygiene assertion (design D1): the
// typed value appears in no tool result, no captured log line, and no file the
// child wrote.
func assertCanaryNowhere(t *testing.T, b *browserTools, canary string, results []string, logs ...*syncBuffer) {
	t.Helper()
	for i, text := range results {
		require.NotContains(t, text, canary, "tool result %d must not carry the typed value", i)
	}
	for _, buf := range logs {
		require.NotContains(t, buf.String(), canary, "captured log output must not carry the typed value")
	}
	assertNoCanaryUnder(t, filepath.Join(b.cfg.BrowserDir, "screenshots"), canary)
	assertNoCanaryInFile(t, cookieJarPath(filepath.Join(b.cfg.BrowserDir, "profiles", DefaultBrowserProfile)), canary)
}

// TestChildSecretHygiene is the child-side secret-hygiene proof (tasks.md 7.4):
// the full handover protocol per fixture shape, with the canary checked
// against every channel the child could leak through.
func TestChildSecretHygiene(t *testing.T) {
	requireTestBrowser(t)

	t.Run("form post navigation", func(t *testing.T) {
		slogOut := captureSlog(t)
		stderrOut := captureStderr(t)
		canary := hygieneCanary(t)
		f := newFixtureServer(t)
		b := newBrowserTools(t)

		var results []string
		record := func(res mcp.CallToolResult) { results = append(results, resultText(t, res)) }

		navigate(t, b, f.URL+"/form")
		res := callHandler(t, b.handleSnapshot, map[string]any{})
		record(res)
		var snap SnapshotResult
		mustJSON(t, res, &snap)
		require.Contains(t, snapElementNames(snap), "Password", "the snapshot lists the password field")

		shot := callHandler(t, b.handleScreenshot, map[string]any{})
		record(shot)
		require.False(t, shot.IsError, "pre-flow screenshot: %s", resultText(t, shot))

		ready := requestSecret(t, b, record, map[string]any{
			"targets": []any{"Password"}, "method": "password", "submission": "click",
			"submitTarget": "Sign in",
		})

		text, res := deliverCanary(t, b, ready.RequestID, canary)
		record(res)
		var got deliverCompletedResult
		mustJSON(t, res, &got)
		require.Equal(t, "completed", got.Status)
		require.NotContains(t, text, canary, "the delivery receipt never carries the value")

		settle(t, b)
		require.Equal(t, []string{canary}, f.received,
			"positive control: the typed value reached the site exactly once")

		read := callHandler(t, b.handleRead, map[string]any{})
		record(read)
		var page ReadResult
		mustJSON(t, read, &page)
		require.NotContains(t, page.Text+page.URL+page.Title, canary,
			"the post-navigation page state carries no value")

		shot = callHandler(t, b.handleScreenshot, map[string]any{})
		record(shot)
		require.False(t, shot.IsError, "post-flow screenshot: %s", resultText(t, shot))

		resolve := callHandler(t, b.handleResolveAttention, map[string]any{
			"requestID": ready.RequestID, "outcome": "completed",
		})
		record(resolve)
		require.False(t, resolve.IsError, "the delivered request stays resolvable: %s", resultText(t, resolve))

		require.False(t, scanPage(t, b, canary), "the scrub predicate finds no value on the live document")
		jar := forceJarSave(t, b)
		assertNoCanaryInFile(t, jar, canary)
		assertCanaryNowhere(t, b, canary, results, slogOut, stderrOut)
	})

	t.Run("spa same-document scrub keeps and holds", func(t *testing.T) {
		slogOut := captureSlog(t)
		stderrOut := captureStderr(t)
		canary := hygieneCanary(t)
		f := newFixtureServer(t)
		b := newBrowserTools(t)

		var results []string
		record := func(res mcp.CallToolResult) { results = append(results, resultText(t, res)) }

		navigate(t, b, f.URL+"/spa")
		res := callHandler(t, b.handleSnapshot, map[string]any{})
		record(res)

		ready := requestSecret(t, b, record, map[string]any{
			"targets": []any{"Code"}, "method": "password", "submission": "enter",
		})

		text, res := deliverCanary(t, b, ready.RequestID, canary)
		record(res)
		var got deliverCompletedResult
		mustJSON(t, res, &got)
		require.Equal(t, "completed", got.Status)
		require.NotContains(t, text, canary)

		settle(t, b)
		require.Equal(t, []string{canary}, f.received,
			"positive control: the SPA handler received the typed value")

		// SPA-keep: the evaluate hold refuses the evaluate paths with the
		// weak-hold text, while inspection stays allowed.
		read := callHandler(t, b.handleRead, map[string]any{})
		record(read)
		require.True(t, read.IsError, "the SPA-keep hold refuses read")
		require.Contains(t, resultText(t, read), "Secure authentication just completed")
		require.Contains(t, resultText(t, read), "until the next navigation")
		snap := callHandler(t, b.handleSnapshot, map[string]any{})
		record(snap)
		require.False(t, snap.IsError, "inspection is allowed in the weak state: %s", resultText(t, snap))

		// The scrub cleared the fields: the child's own predicate (title, URL,
		// text, values, attributes, open shadow roots) finds nothing.
		require.False(t, scanPage(t, b, canary), "the scrubbed same-document page carries no value")

		shot := callHandler(t, b.handleScreenshot, map[string]any{})
		record(shot)
		require.False(t, shot.IsError, "weak-state screenshot: %s", resultText(t, shot))

		resolve := callHandler(t, b.handleResolveAttention, map[string]any{
			"requestID": ready.RequestID, "outcome": "completed",
		})
		record(resolve)
		require.False(t, resolve.IsError, "the delivered request stays resolvable: %s", resultText(t, resolve))

		// A real main-frame navigation releases the hold.
		navigate(t, b, f.URL+"/form")
		read = callHandler(t, b.handleRead, map[string]any{})
		record(read)
		var page ReadResult
		mustJSON(t, read, &page)
		require.NotContains(t, page.Text, canary, "the released page carries no value")

		jar := forceJarSave(t, b)
		assertNoCanaryInFile(t, jar, canary)
		assertCanaryNowhere(t, b, canary, results, slogOut, stderrOut)
	})

	t.Run("shadow root echo scrubs by reload", func(t *testing.T) {
		slogOut := captureSlog(t)
		stderrOut := captureStderr(t)
		canary := hygieneCanary(t)
		f := newFixtureServer(t)
		b := newBrowserTools(t)

		var results []string
		record := func(res mcp.CallToolResult) { results = append(results, resultText(t, res)) }

		navigate(t, b, f.URL+"/echo?shadow=1")
		res := callHandler(t, b.handleSnapshot, map[string]any{})
		record(res)

		ready := requestSecret(t, b, record, map[string]any{
			"targets": []any{"Password"}, "method": "password", "submission": "click",
			"submitTarget": "Send",
		})

		text, res := deliverCanary(t, b, ready.RequestID, canary)
		record(res)
		var got deliverCompletedResult
		mustJSON(t, res, &got)
		require.Equal(t, "completed", got.Status)
		require.NotContains(t, text, canary)

		require.Equal(t, []string{canary}, f.received,
			"positive control: the page's echo handler received the typed value")
		require.Equal(t, 2, f.loadCount("/echo"), "the scrub replaced the echo document with a GET reload")

		settle(t, b)
		read := callHandler(t, b.handleRead, map[string]any{})
		record(read)
		var page ReadResult
		mustJSON(t, read, &page)
		require.NotContains(t, page.Text+page.URL+page.Title, canary,
			"the reloaded document carries no value, including no shadow-root echo")

		require.False(t, scanPage(t, b, canary), "the scrub predicate finds no value after the reload")

		shot := callHandler(t, b.handleScreenshot, map[string]any{})
		record(shot)
		require.False(t, shot.IsError, "post-reload screenshot: %s", resultText(t, shot))

		resolve := callHandler(t, b.handleResolveAttention, map[string]any{
			"requestID": ready.RequestID, "outcome": "completed",
		})
		record(resolve)
		require.False(t, resolve.IsError, "the delivered request stays resolvable: %s", resultText(t, resolve))

		jar := forceJarSave(t, b)
		assertNoCanaryInFile(t, jar, canary)
		assertCanaryNowhere(t, b, canary, results, slogOut, stderrOut)
	})

	t.Run("takeover endings hold and release", func(t *testing.T) {
		f := newFixtureServer(t)
		b := newBrowserTools(t)

		// Round 1: the cancelled ending.
		navigate(t, b, f.URL+"/form")
		ready := callHandler(t, b.handleRequestTakeover, map[string]any{"reason": "sign-in-blocked"})
		var take takeoverReadyResult
		mustJSON(t, ready, &take)
		require.Equal(t, "ready", take.Status)

		begin := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": take.RequestID})
		var active takeoverActiveResult
		mustJSON(t, begin, &active)
		require.Equal(t, "active", active.Status)

		held := callHandler(t, b.handleSnapshot, map[string]any{})
		require.True(t, held.IsError)
		require.Equal(t, heldRefusal, resultText(t, held))

		cancel := callHandler(t, b.handleResolveAttention, map[string]any{
			"requestID": take.RequestID, "outcome": "cancelled",
		})
		var ended attentionResolvedResult
		mustJSON(t, cancel, &ended)
		require.Equal(t, "resolved", ended.Status)

		// Round 2: the completed ending, on a fresh request.
		ready2 := callHandler(t, b.handleRequestTakeover, map[string]any{"reason": "sign-in-blocked"})
		var take2 takeoverReadyResult
		mustJSON(t, ready2, &take2)
		require.Equal(t, "ready", take2.Status)
		require.NotEqual(t, take.RequestID, take2.RequestID)

		begin2 := callHandler(t, b.handleBeginTakeover, map[string]any{"requestID": take2.RequestID})
		var active2 takeoverActiveResult
		mustJSON(t, begin2, &active2)
		require.Equal(t, "active", active2.Status)

		held2 := callHandler(t, b.handleRead, map[string]any{})
		require.True(t, held2.IsError)
		require.Equal(t, heldRefusal, resultText(t, held2))

		done := callHandler(t, b.handleResolveAttention, map[string]any{
			"requestID": take2.RequestID, "outcome": "completed",
		})
		var ended2 attentionResolvedResult
		mustJSON(t, done, &ended2)
		require.Equal(t, "resolved", ended2.Status)

		// Both endings released the hold: the session inspects again.
		navigate(t, b, f.URL+"/form")
		snap := callHandler(t, b.handleSnapshot, map[string]any{})
		var after SnapshotResult
		mustJSON(t, snap, &after)
		require.NotEmpty(t, after.Elements, "the hold released after resolution")
	})
}

// snapElementNames flattens a snapshot result into the element name/ref set.
func snapElementNames(snap SnapshotResult) []string {
	out := make([]string, 0, len(snap.Elements))
	for _, el := range snap.Elements {
		key := el.Ref
		if el.Name != "" {
			key = el.Name
		}
		out = append(out, key)
	}
	return out
}
