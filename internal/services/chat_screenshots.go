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
	"github.com/oniharnantyo/buddies/internal/workspaces"
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

// BrowserScreenshot reads one screenshot the browser tool captured into the
// calling session's workspace: name must be the exact basename the tool
// writes ("<unixmilli>.png"), resolved strictly as
// <workspaces>/<agent slug>/sessions/<sessionID>/browser/<name> — the same
// directory the child writes through the parent's per-call _meta, inside the
// workspace jail (add-browser-login-handover follow-up: the model reads its
// own capture back with read_file). A name with directory components or
// outside the shape is refused before any filesystem call; a well-shaped
// name with no file behind it is a not-found error. No other path is ever
// touched.
func (s *ChatService) BrowserScreenshot(ctx context.Context, sessionID, name string) (BrowserScreenshotContent, error) {
	// Refusal before the filesystem: the shape rule and the basename rule
	// together mean the join below can only ever land inside the session's
	// screenshots directory (spec: refused names touch no path).
	if !screenshotNameShape.MatchString(name) || filepath.Base(name) != name {
		return BrowserScreenshotContent{}, fmt.Errorf("browser screenshot: refused name %q", name)
	}
	if sessionID == "" || s.workspaces == nil {
		return BrowserScreenshotContent{}, errors.New("browser screenshot: no session workspace to resolve")
	}
	sess, err := s.sessions.Session(ctx, sessionID)
	if err != nil {
		return BrowserScreenshotContent{}, fmt.Errorf("browser screenshot %s: resolve session: %w", name, err)
	}
	agent, err := s.store.Agents().ByID(ctx, sess.AgentID)
	if err != nil {
		return BrowserScreenshotContent{}, fmt.Errorf("browser screenshot %s: resolve agent: %w", name, err)
	}
	sessionDir, err := s.workspaces.SessionDir(agent.Slug, sessionID)
	if err != nil {
		return BrowserScreenshotContent{}, fmt.Errorf("browser screenshot %s: resolve session directory: %w", name, err)
	}
	data, err := os.ReadFile(filepath.Join(sessionDir, workspaces.DirBrowser, name))
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

// ComputerScreenshot reads one screenshot a computer-use observation captured
// into the calling session's workspace (add-computer-use, spec chat-tool-cards
// "Computer screenshot bound read"): name must be the exact basename the
// observation writes ("<unixmilli>.png"), resolved strictly as
// <workspaces>/<agent slug>/sessions/<sessionID>/computer/<name> — the
// directory the parent stamps through the tool call's per-call _meta. The
// same strict shape, refusal-before-filesystem ordering, and not-found
// mapping as BrowserScreenshot apply, so a name can never smuggle a path out
// of the session jail; a well-shaped name with no file behind it is a
// not-found error. No other path is ever touched.
func (s *ChatService) ComputerScreenshot(ctx context.Context, sessionID, name string) (BrowserScreenshotContent, error) {
	// Refusal before the filesystem: the shape rule and the basename rule
	// together mean the join below can only ever land inside the session's
	// computer screenshots directory (spec: no file bytes leave the session
	// jail).
	if !screenshotNameShape.MatchString(name) || filepath.Base(name) != name {
		return BrowserScreenshotContent{}, fmt.Errorf("computer screenshot: refused name %q", name)
	}
	if sessionID == "" || s.workspaces == nil {
		return BrowserScreenshotContent{}, errors.New("computer screenshot: no session workspace to resolve")
	}
	sess, err := s.sessions.Session(ctx, sessionID)
	if err != nil {
		return BrowserScreenshotContent{}, fmt.Errorf("computer screenshot %s: resolve session: %w", name, err)
	}
	agent, err := s.store.Agents().ByID(ctx, sess.AgentID)
	if err != nil {
		return BrowserScreenshotContent{}, fmt.Errorf("computer screenshot %s: resolve agent: %w", name, err)
	}
	sessionDir, err := s.workspaces.SessionDir(agent.Slug, sessionID)
	if err != nil {
		return BrowserScreenshotContent{}, fmt.Errorf("computer screenshot %s: resolve session directory: %w", name, err)
	}
	data, err := os.ReadFile(filepath.Join(sessionDir, workspaces.DirComputer, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return BrowserScreenshotContent{}, fmt.Errorf("computer screenshot %s: %w", name, store.ErrNotFound)
		}
		return BrowserScreenshotContent{}, fmt.Errorf("computer screenshot %s: %w", name, err)
	}
	dim, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return BrowserScreenshotContent{}, fmt.Errorf("computer screenshot %s: decode: %w", name, err)
	}
	return BrowserScreenshotContent{
		Name:   name,
		Width:  dim.Width,
		Height: dim.Height,
		Data:   base64.StdEncoding.EncodeToString(data),
	}, nil
}
