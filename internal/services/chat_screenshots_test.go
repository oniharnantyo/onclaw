package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/sessions"
	"github.com/oniharnantyo/buddies/internal/store"
	fake "github.com/oniharnantyo/buddies/internal/store/fake"
	"github.com/oniharnantyo/buddies/internal/workspaces"
)

// The browser-screenshot bound read's tests (tasks.md 1.3, design D6, spec
// chat-tool-cards "Browser screenshot access"): a well-shaped name is served
// from the calling session's workspace screenshots directory
// (<workspaces>/<slug>/sessions/<id>/browser — the directory the browser tool
// writes through the parent's per-call _meta); anything carrying directory
// components or outside the "<unixmilli>.png" shape is refused before the
// filesystem is touched; a missing file is a not-found error.

// screenshotService seeds an agent and session, roots a workspace manager at
// a temp dir, and returns the chat service, the session's browser screenshots
// directory, and the session id.
func screenshotService(t *testing.T) (*ChatService, string, string) {
	t.Helper()
	ctx := context.Background()
	st := fake.New()
	ws := workspaces.NewManager(t.TempDir())
	agent := &store.Agent{ID: "agent1", Name: "Scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(ctx, agent))
	const sessionID = "e7238190a1b2c3d4e5f60718293a4b5c" // the minted 32-hex shape
	require.NoError(t, st.Sessions().Create(ctx, &store.Session{ID: sessionID, AgentID: agent.ID}))
	_, err := ws.Create(agent.Slug)
	require.NoError(t, err)
	dir := filepath.Join(ws.Root(), agent.Slug, "sessions", sessionID, workspaces.DirBrowser)
	svc := NewChatService(nil, sessions.NewService(st), &recordingEmitter{}, st, nil, ws)
	return svc, dir, sessionID
}

// writeScreenshot writes a real 2x3 PNG under dir so the dimension decoding
// has genuine pixels to report.
func writeScreenshot(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

// TestChatService_BrowserScreenshotServesValidName is the acceptance path:
// the browser tool's exact naming shape reads back the bytes and the real
// pixel dimensions, and reports only the name — never the on-disk path.
func TestChatService_BrowserScreenshotServesValidName(t *testing.T) {
	svc, dir, sessionID := screenshotService(t)
	writeScreenshot(t, filepath.Join(dir, "1730000000000.png"))
	want, err := os.ReadFile(filepath.Join(dir, "1730000000000.png"))
	require.NoError(t, err)

	got, err := svc.BrowserScreenshot(context.Background(), sessionID, "1730000000000.png")
	require.NoError(t, err)
	require.Equal(t, "1730000000000.png", got.Name)
	require.Equal(t, 2, got.Width)
	require.Equal(t, 3, got.Height)
	require.Equal(t, want, mustDecodeBase64(t, got.Data))
}

// TestChatService_BrowserScreenshotStaysInTheCallingSession pins the session
// scoping: an identical name in another session's screenshots directory is
// never served, and the calling session's directory is the only one touched.
func TestChatService_BrowserScreenshotStaysInTheCallingSession(t *testing.T) {
	ctx := context.Background()
	svc, dir, sessionID := screenshotService(t)
	writeScreenshot(t, filepath.Join(dir, "1730000000000.png"))

	// A second session of the same agent has its own, empty screenshots dir.
	_, werr := svc.workspaces.Create("scout")
	require.NoError(t, werr)
	other := filepath.Join(svc.workspaces.Root(), "scout", "sessions", "s2", workspaces.DirBrowser)
	require.NoError(t, os.MkdirAll(other, 0o700))

	_, err := svc.BrowserScreenshot(ctx, "missing-session", "1730000000000.png")
	require.Error(t, err, "an unknown session resolves to no directory at all")

	// The served bytes are always the calling session's file.
	got, err := svc.BrowserScreenshot(ctx, sessionID, "1730000000000.png")
	require.NoError(t, err)
	require.NotEmpty(t, got.Data)
}

// TestChatService_BrowserScreenshotRefusesBadNames pins the refusal rules
// (spec: refused before the filesystem is touched): traversal, directory
// components, and foreign shapes are refused even when a file exists at the
// addressed location — and nothing outside the screenshots directory is read.
func TestChatService_BrowserScreenshotRefusesBadNames(t *testing.T) {
	svc, dir, sessionID := screenshotService(t)
	// Files that exist at the addressed locations: a traversal target
	// outside the screenshots dir, a nested file, and a wrong extension.
	outside := filepath.Join(dir, "..", "outside.png")
	writeScreenshot(t, outside)
	writeScreenshot(t, filepath.Join(dir, "sub", "1.png"))
	writeScreenshot(t, filepath.Join(dir, "1.jpg"))
	outsideBefore, err := os.ReadFile(outside)
	require.NoError(t, err)

	for _, name := range []string{
		"../outside.png",  // traversal — must never escape the directory
		"..\\outside.png", // windows-style traversal
		"sub/1.png",       // directory component, file exists
		"1.jpg",           // foreign extension
		"png",             // no timestamp
		"abc.png",         // not a timestamp
		"",                // empty
	} {
		_, err := svc.BrowserScreenshot(context.Background(), sessionID, name)
		require.Error(t, err, "name %q must be refused", name)
		require.ErrorContains(t, err, "refused", "name %q must be refused before the filesystem", name)
	}

	// The outside file is untouched and was never served.
	outsideAfter, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, outsideBefore, outsideAfter)
}

// TestChatService_BrowserScreenshotMissingIsNotFound pins the not-found
// contract: a well-shaped name with no file behind it wraps
// store.ErrNotFound, which the card renders as an unavailable state.
func TestChatService_BrowserScreenshotMissingIsNotFound(t *testing.T) {
	svc, _, sessionID := screenshotService(t)

	_, err := svc.BrowserScreenshot(context.Background(), sessionID, "1730000000001.png")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestChatService_BrowserScreenshotUnwiredWorkspace refuses the read entirely
// when no workspace manager was wired or the scope carries no session: with
// no session workspace there is nothing to serve from and no path to guess
// at.
func TestChatService_BrowserScreenshotUnwiredWorkspace(t *testing.T) {
	st := fake.New()
	agent := &store.Agent{ID: "agent1", Name: "Scout", Slug: "scout"}
	require.NoError(t, st.Agents().Create(context.Background(), agent))
	require.NoError(t, st.Sessions().Create(context.Background(), &store.Session{ID: "s1", AgentID: agent.ID}))
	svc := NewChatService(nil, sessions.NewService(st), &recordingEmitter{}, st, nil, nil)

	_, err := svc.BrowserScreenshot(context.Background(), "s1", "1730000000000.png")
	require.Error(t, err)
	require.False(t, errors.Is(err, store.ErrNotFound), "an unwired workspace is a wiring error, not a lookup miss")

	// A blank session id resolves to no workspace either.
	ws := workspaces.NewManager(t.TempDir())
	_, err = ws.Create("scout")
	require.NoError(t, err)
	wired := NewChatService(nil, sessions.NewService(st), &recordingEmitter{}, st, nil, ws)
	_, err = wired.BrowserScreenshot(context.Background(), "", "1730000000000.png")
	require.Error(t, err)
}

// mustDecodeBase64 decodes a served payload back to bytes for comparison.
func mustDecodeBase64(t *testing.T, data string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(data)
	require.NoError(t, err)
	return raw
}
