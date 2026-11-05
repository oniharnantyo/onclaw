package browseruse

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// The screenshot directory resolution (add-browser-login-handover
// follow-up): the parent stamps the calling session's workspace screenshots
// directory through the call's _meta, and every capture lands inside the
// model's workspace jail so read_file can reach it. The meta channel rides
// request metadata — never the arguments — so the model can neither see nor
// influence it; anything absent or malformed falls back to the browser dir's
// own screenshots/ (direct child use, the CLI, the tests).

// TestScreenshotDir_MetaResolution is the pure resolution table: a clean
// absolute meta path wins, everything else falls back.
func TestScreenshotDir_MetaResolution(t *testing.T) {
	fallback := filepath.Join(t.TempDir(), "browser")
	cases := []struct {
		name string
		meta map[string]any
		want string
	}{
		{
			name: "no meta at all",
			meta: nil,
			want: filepath.Join(fallback, "screenshots"),
		},
		{
			name: "meta without the key",
			meta: map[string]any{"other": "value"},
			want: filepath.Join(fallback, "screenshots"),
		},
		{
			name: "clean absolute session dir",
			meta: map[string]any{MetaScreenshotDir: "/home/u/workspaces/scout/sessions/abc/browser"},
			want: "/home/u/workspaces/scout/sessions/abc/browser",
		},
		{
			name: "relative path refused",
			meta: map[string]any{MetaScreenshotDir: "workspaces/scout/sessions/abc/browser"},
			want: filepath.Join(fallback, "screenshots"),
		},
		{
			// Clean resolves the dot-dot before the guard runs, so a
			// traversal-shaped path lands on its plain absolute meaning —
			// still exactly the directory the host named, never a relative
			// or unresolved form.
			name: "traversal cleans to its absolute meaning",
			meta: map[string]any{MetaScreenshotDir: "/home/u/../u/browser"},
			want: "/home/u/browser",
		},
		{
			name: "empty value refused",
			meta: map[string]any{MetaScreenshotDir: ""},
			want: filepath.Join(fallback, "screenshots"),
		},
		{
			name: "non-string value refused",
			meta: map[string]any{MetaScreenshotDir: 42},
			want: filepath.Join(fallback, "screenshots"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req mcp.CallToolRequest
			if tc.meta != nil {
				req.Params.Meta = mcp.NewMetaFromMap(tc.meta)
			}
			require.Equal(t, tc.want, screenshotDir(req, fallback))
		})
	}
}

// TestHandleScreenshot_MetaLandsInTheSessionDir is the live end: a meta-stamped
// screenshot call writes the capture under the supplied session directory —
// not the browser dir's screenshots/ — and the reported path decodes to it.
// The fallback write is covered by the hygiene suite's screenshots.
func TestHandleScreenshot_MetaLandsInTheSessionDir(t *testing.T) {
	b := newBrowserTools(t)
	f := newFixtureServer(t)
	navigate(t, b, f.URL)

	sessionDir := filepath.Join(t.TempDir(), "workspaces", "scout", "sessions", "abc", "browser")
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "screenshot",
			Arguments: map[string]any{},
			Meta:      mcp.NewMetaFromMap(map[string]any{MetaScreenshotDir: sessionDir}),
		},
	}
	res, err := b.handleScreenshot(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.False(t, res.IsError, "screenshot: %s", resultText(t, *res))

	var got ScreenshotResult
	require.NoError(t, json.Unmarshal([]byte(resultText(t, *res)), &got))
	require.True(t, filepath.IsAbs(got.Path) && filepath.Dir(got.Path) == filepath.Clean(sessionDir),
		"the reported path %q stays inside the session directory", got.Path)
	require.DirExists(t, sessionDir)
	require.FileExists(t, got.Path)

	// Nothing leaked into the browser dir's own screenshots directory.
	entries, err := os.ReadDir(filepath.Join(b.cfg.BrowserDir, "screenshots"))
	if err == nil {
		require.Empty(t, entries, "a meta-stamped capture never touches the fallback directory")
	}

	// And the name the chat screenshot read addresses is still the bare
	// "<unixmilli>.png" basename the serving regex pins.
	base := filepath.Base(got.Path)
	require.Regexp(t, `^\d+\.png$`, base)
}
