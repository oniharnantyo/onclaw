package browseruse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// Live smoke for add-per-agent-browser-profiles: two headed Chromium windows
// on two host-stamped profile directories, each holding only its own cookie,
// with jar files landing in each bound directory and a cookie surviving a
// kill-and-relaunch on the same bound directory. Skipped unless
// BUDDIES_PROFILE_LIVE_SMOKE=1 (real browser, real windows). Evidence
// (screenshots) lands under .team/smoke/ for the human smoke report.
func TestLiveProfileSmoke(t *testing.T) {
	if os.Getenv("BUDDIES_PROFILE_LIVE_SMOKE") != "1" {
		t.Skip("live smoke: set BUDDIES_PROFILE_LIVE_SMOKE=1 to run against a real Chromium")
	}

	smokeRoot, err := filepath.Abs(filepath.Join("..", "..", ".team", "smoke", "per-agent-profiles"))
	require.NoError(t, err)
	shots := filepath.Join(smokeRoot, "shots")
	require.NoError(t, os.MkdirAll(shots, 0o700))

	// The page echoes document.cookie so isolation is visible in the text
	// extraction AND in the screenshot pixels.
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><body style="font:32px monospace">
<h1>PER-AGENT PROFILE SMOKE</h1>
<p id="jar">document.cookie = <b><script>document.write(document.cookie || "(none yet)")</script></b></p>
</body></html>`)
	}))
	defer page.Close()

	cfg := DefaultConfig()
	cfg.BrowserDir = t.TempDir()
	cfg.Headless = false // real windows — the visible proof of one browser per agent
	cfg.AllowPrivate = true
	b := &browserTools{cfg: cfg, mgr: newManager()}
	t.Cleanup(func() { _ = b.mgr.shutdownAll() })

	type agent struct {
		slug string
		dir  string
	}
	agents := []agent{
		{slug: "scout", dir: filepath.Join(smokeRoot, "workspaces", "scout", "browser-profile")},
		{slug: "hound", dir: filepath.Join(smokeRoot, "workspaces", "hound", "browser-profile")},
	}
	metaFor := func(a agent) map[string]any {
		return map[string]any{
			MetaProfileDir:    a.dir,
			MetaProfileName:   a.slug,
			MetaScreenshotDir: filepath.Join(shots, a.slug),
		}
	}
	resultText := func(res mcp.CallToolResult) string {
		require.False(t, res.IsError, "live smoke step failed: %v", res.Content)
		tc, ok := res.Content[0].(mcp.TextContent)
		require.True(t, ok, "expected a text result")
		return tc.Text
	}
	shotPath := func(a agent) string {
		matches, err := filepath.Glob(filepath.Join(shots, a.slug, "*.png"))
		require.NoError(t, err)
		require.Len(t, matches, 1, "one screenshot per agent")
		return matches[0]
	}

	// 1. Both agents browse, set their own cookie, and re-read the page.
	pageText := map[string]string{}
	for _, a := range agents {
		meta := metaFor(a)
		out := resultText(callHandlerMeta(t, b.handleNavigate, map[string]any{"url": page.URL}, meta))
		require.Contains(t, out, "127.0.0.1", "navigate answers the page payload: %s", out)

		cookieName, cookieValue := a.slug+"-cookie", "owned-by-"+a.slug
		resultText(callHandlerMeta(t, b.handleCookies, map[string]any{
			"op": "set", "name": cookieName, "value": cookieValue,
		}, meta))
		resultText(callHandlerMeta(t, b.handleNavigate, map[string]any{"url": page.URL}, meta))

		read := resultText(callHandlerMeta(t, b.handleRead, map[string]any{}, meta))
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal([]byte(read), &payload)
		body := payload.Text
		if body == "" {
			body = read
		}
		require.Contains(t, body, cookieName+"="+cookieValue, "agent %s must see its own cookie: %s", a.slug, body)
		pageText[a.slug] = body

		resultText(callHandlerMeta(t, b.handleScreenshot, map[string]any{}, meta))
		require.FileExists(t, shotPath(a))
	}

	// 2. Isolation: neither page saw the other's cookie.
	for _, a := range agents {
		for _, other := range agents {
			if other.slug == a.slug {
				continue
			}
			require.NotContains(t, pageText[a.slug], other.slug+"-cookie",
				"agent %s must not see agent %s's cookie", a.slug, other.slug)
		}
	}

	// 3. Two distinct live sessions keyed by the two bound directories.
	b.mgr.mu.Lock()
	require.Len(t, b.mgr.sessions, 2, "one live browser per bound dir")
	_, scoutLive := b.mgr.sessions[agents[0].dir]
	_, houndLive := b.mgr.sessions[agents[1].dir]
	b.mgr.mu.Unlock()
	require.True(t, scoutLive && houndLive, "sessions keyed by the absolute bound dirs")

	// 4. Jar durability: each bound dir carries its own buddies-cookies.json.
	for _, a := range agents {
		require.FileExists(t, cookieJarPath(a.dir), "jar lands in the agent's bound profile dir")
	}

	// 5. Persistence across a kill-and-relaunch on the same bound directory:
	// scout's browser dies, the next bound action relaunches on the same dir
	// and the cookie is still there.
	require.NoError(t, b.mgr.shutdown(agents[0].dir))
	out := resultText(callHandlerMeta(t, b.handleNavigate, map[string]any{"url": page.URL}, metaFor(agents[0])))
	require.Contains(t, out, "127.0.0.1", "navigate answers the page payload: %s", out)
	read := resultText(callHandlerMeta(t, b.handleRead, map[string]any{}, metaFor(agents[0])))
	require.Contains(t, strings.ReplaceAll(read, "\n", " "), "scout-cookie=owned-by-scout",
		"the relaunched page still shows scout's cookie")

	t.Logf("SMOKE OK — screenshots: %s, %s; jars: %s, %s",
		shotPath(agents[0]), shotPath(agents[1]), cookieJarPath(agents[0].dir), cookieJarPath(agents[1].dir))
}
