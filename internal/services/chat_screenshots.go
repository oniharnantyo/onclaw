package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/oniharnantyo/buddies/internal/store"
)

// The browser-screenshot bound read (design D6, spec chat-tool-cards
// "Browser screenshot access"): the webview obtains a screenshot's bytes only
// through this call, addressed by the file name the browser tool reported.
// The name is validated against the exact shape the tool writes BEFORE any
// filesystem access, and resolved solely inside the browser tool's
// screenshots directory — the on-disk path is never exposed.

// screenshotNameShape matches exactly the names the browser tool writes
// under <browserDir>/screenshots: a unix-millisecond timestamp plus the png
// extension (tools/browser_actions.go runScreenshot). Anything else —
// including anything carrying directory components — is refused before the
// filesystem is touched, so a name can never smuggle a path.
var screenshotNameShape = regexp.MustCompile(`^\d+\.png$`)

// ChatOption configures the chat service.
type ChatOption func(*ChatService)

// WithChatBrowserDir supplies the chat service's browser tool home directory:
// the screenshot read serves from <browserDir>/screenshots, the same root the
// runtime's browser tool was given (spec add-browser-tool D10). Named apart
// from the settings service's WithBrowserDir option, which scopes to
// SettingsService.
func WithChatBrowserDir(dir string) ChatOption {
	return func(s *ChatService) { s.browserDir = dir }
}

// BrowserScreenshotContent is one screenshot's served content: the name it
// was addressed by, its pixel dimensions (for shift-free rendering), and the
// base64-encoded PNG bytes. The on-disk path never crosses the boundary.
type BrowserScreenshotContent struct {
	Name   string `json:"name"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	// Data is the base64-encoded PNG content.
	Data string `json:"data"`
}

// BrowserScreenshot reads one screenshot the browser tool captured: name
// must be the exact basename the tool writes ("<unixmilli>.png"), resolved
// strictly as <browserDir>/screenshots/<name>. A name with directory
// components or outside the shape is refused before any filesystem call; a
// well-shaped name with no file behind it is a not-found error. No other
// path is ever touched.
func (s *ChatService) BrowserScreenshot(_ context.Context, name string) (BrowserScreenshotContent, error) {
	if s.browserDir == "" {
		return BrowserScreenshotContent{}, errors.New("browser screenshot: no browser directory configured")
	}
	// Refusal before the filesystem: the shape rule and the basename rule
	// together mean the join below can only ever land inside the screenshots
	// directory (spec: refused names touch no path).
	if !screenshotNameShape.MatchString(name) || filepath.Base(name) != name {
		return BrowserScreenshotContent{}, fmt.Errorf("browser screenshot: refused name %q", name)
	}
	data, err := os.ReadFile(filepath.Join(s.browserDir, "screenshots", name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return BrowserScreenshotContent{}, fmt.Errorf("browser screenshot %s: %w", name, store.ErrNotFound)
		}
		return BrowserScreenshotContent{}, fmt.Errorf("browser screenshot %s: %w", name, err)
	}
	dim, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return BrowserScreenshotContent{}, fmt.Errorf("browser screenshot %s: decode: %w", name, err)
	}
	return BrowserScreenshotContent{
		Name:   name,
		Width:  dim.Width,
		Height: dim.Height,
		Data:   base64.StdEncoding.EncodeToString(data),
	}, nil
}
