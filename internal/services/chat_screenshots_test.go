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

	"github.com/oniharnantyo/buddies/internal/store"
)

// The browser-screenshot bound read's tests (tasks.md 1.3, design D6, spec
// chat-tool-cards "Browser screenshot access"): a well-shaped name is served
// from <browserDir>/screenshots only; anything carrying directory components
// or outside the "<unixmilli>.png" shape is refused before the filesystem is
// touched; a missing file is a not-found error. The service under test needs
// no runtime dependencies — the read touches only the browser directory.

// screenshotService returns a ChatService serving screenshots from root (the
// browser directory) and the screenshots directory it would serve from.
func screenshotService(t *testing.T) (*ChatService, string) {
	t.Helper()
	root := t.TempDir()
	svc := NewChatService(nil, nil, nil, nil, nil, nil, WithChatBrowserDir(root))
	return svc, filepath.Join(root, "screenshots")
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
	svc, dir := screenshotService(t)
	writeScreenshot(t, filepath.Join(dir, "1730000000000.png"))
	want, err := os.ReadFile(filepath.Join(dir, "1730000000000.png"))
	require.NoError(t, err)

	got, err := svc.BrowserScreenshot(context.Background(), "1730000000000.png")
	require.NoError(t, err)
	require.Equal(t, "1730000000000.png", got.Name)
	require.Equal(t, 2, got.Width)
	require.Equal(t, 3, got.Height)
	require.Equal(t, want, mustDecodeBase64(t, got.Data))
}

// TestChatService_BrowserScreenshotRefusesBadNames pins the refusal rules
// (spec: refused before the filesystem is touched): traversal, directory
// components, and foreign shapes are refused even when a file exists at the
// addressed location — and nothing outside the screenshots directory is read.
func TestChatService_BrowserScreenshotRefusesBadNames(t *testing.T) {
	svc, dir := screenshotService(t)
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
		_, err := svc.BrowserScreenshot(context.Background(), name)
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
	svc, _ := screenshotService(t)

	_, err := svc.BrowserScreenshot(context.Background(), "1730000000001.png")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestChatService_BrowserScreenshotUnconfiguredDir refuses the read entirely
// when no browser directory was wired: with no root there is nothing to
// serve from and no path to guess at.
func TestChatService_BrowserScreenshotUnconfiguredDir(t *testing.T) {
	svc := NewChatService(nil, nil, nil, nil, nil, nil)

	_, err := svc.BrowserScreenshot(context.Background(), "1730000000000.png")
	require.Error(t, err)
	require.False(t, errors.Is(err, store.ErrNotFound), "an unconfigured root is a wiring error, not a lookup miss")
}

// mustDecodeBase64 decodes a served payload back to bytes for comparison.
func mustDecodeBase64(t *testing.T, data string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(data)
	require.NoError(t, err)
	return raw
}
