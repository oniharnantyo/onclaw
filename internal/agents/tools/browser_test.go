package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// fakeSession is a scripted browserSession.
type fakeSession struct {
	mu          sync.Mutex
	navigations []string
	acts        []string
	readText    string
	screenshots []string
	title       string
	closed      bool
	// refs is the current valid ref set; refActions records ref actions.
	refs       map[string]bool
	refActions []string
}

func (s *fakeSession) Navigate(_ context.Context, url string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.navigations = append(s.navigations, url)
	return s.title, nil
}

func (s *fakeSession) Act(_ context.Context, action, selector, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acts = append(s.acts, action+" "+selector+" "+text)
	switch action {
	case "click":
		return `{"clicked": "` + selector + `"}`, nil
	case "type":
		return `{"typed": true, "selector": "` + selector + `"}`, nil
	case "scroll":
		return `{"scrolled": true}`, nil
	}
	return `{"ok": true}`, nil
}

func (s *fakeSession) Read(_ context.Context) (string, error) {
	return s.readText, nil
}

func (s *fakeSession) Screenshot(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.screenshots = append(s.screenshots, path)
	return os.WriteFile(path, []byte("png"), 0o644)
}

func (s *fakeSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

func (s *fakeSession) Snapshot(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refs == nil {
		s.refs = map[string]bool{"101": true, "102": true}
	}
	out, _ := json.Marshal(map[string]any{"title": s.title, "elements": []map[string]string{
		{"ref": "101", "role": "button", "name": "Submit"},
		{"ref": "102", "role": "textbox", "name": "Query"},
	}})
	return string(out), nil
}

func (s *fakeSession) RefAction(_ context.Context, action, ref, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refs == nil || !s.refs[ref] {
		return "", &unknownRefError{ref}
	}
	s.refActions = append(s.refActions, action+" "+ref+" "+text)
	out, _ := json.Marshal(map[string]any{"title": s.title, "elements": []map[string]string{
		{"ref": "101", "role": "button", "name": "Submit"},
	}})
	return string(out), nil
}

// unknownRefError mirrors the rod session's unknown-ref failure.
type unknownRefError struct{ ref string }

func (e *unknownRefError) Error() string { return "unknown ref " + e.ref }

// fakeSessions is a browserSessions over fake sessions, tracking teardown.
type fakeSessions struct {
	mu       sync.Mutex
	sessions map[string]*fakeSession
	closed   []string
	fail     bool
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{sessions: map[string]*fakeSession{}}
}

func (f *fakeSessions) Session(_ context.Context, sessionID, _ string) (browserSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errBrowserUnavailable
	}
	s, ok := f.sessions[sessionID]
	if !ok {
		s = &fakeSession{readText: "page text", title: "Example"}
		f.sessions[sessionID] = s
	}
	return s, nil
}

func (f *fakeSessions) CloseSession(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[sessionID]; ok {
		s.Close()
	}
	f.closed = append(f.closed, sessionID)
}

var errBrowserUnavailable = &unavailableError{}

type unavailableError struct{}

func (*unavailableError) Error() string { return "browser unavailable" }

type invoker interface {
	InvokableRun(context.Context, string, ...tool.Option) (string, error)
}

func run(t *testing.T, tl tool.BaseTool, args string) (string, error) {
	t.Helper()
	return tl.(invoker).InvokableRun(context.Background(), args)
}

func newBrowserTool(t *testing.T, src browserSessions, core BrowserToolCore) (tool.BaseTool, tool.BaseTool, tool.BaseTool, tool.BaseTool) {
	t.Helper()
	return &BrowserNavigateTool{core}, &BrowserActTool{core}, &BrowserReadTool{core}, &BrowserScreenshotTool{core}
}

func TestBrowserNavigate(t *testing.T) {
	fs := newFakeSessions()
	nav, _, _, _ := newBrowserTool(t, fs, BrowserToolCore{Sessions: fs, SessionID: "s1", AgentDir: t.TempDir()})

	out, err := run(t, nav, `{"url":"https://example.com"}`)
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if !strings.Contains(out, `"title": "Example"`) && !strings.Contains(out, "Example") {
		t.Errorf("expected title in result: %s", out)
	}

	// Missing URL is invalid.
	if _, err := run(t, nav, `{}`); err == nil {
		t.Error("expected error for missing url")
	}
}

func TestBrowserActValidation(t *testing.T) {
	fs := newFakeSessions()
	_, act, _, _ := newBrowserTool(t, fs, BrowserToolCore{Sessions: fs, SessionID: "s1", AgentDir: t.TempDir()})

	if _, err := run(t, act, `{"action":"jump"}`); err == nil {
		t.Error("expected error for unknown action")
	}
	if _, err := run(t, act, `{"action":"click"}`); err == nil {
		t.Error("expected error for click without selector")
	}
	out, err := run(t, act, `{"action":"type","selector":"#q","text":"hello"}`)
	if err != nil {
		t.Fatalf("type: %v", err)
	}
	if !strings.Contains(out, `"typed": true`) {
		t.Errorf("unexpected result %s", out)
	}
	out, err = run(t, act, `{"action":"scroll"}`)
	if err != nil || !strings.Contains(out, `"scrolled": true`) {
		t.Errorf("scroll failed: %s %v", out, err)
	}
}

func TestBrowserReadAndScreenshot(t *testing.T) {
	fs := newFakeSessions()
	agentDir := t.TempDir()
	_, _, read, shot := newBrowserTool(t, fs, BrowserToolCore{Sessions: fs, SessionID: "s1", AgentDir: agentDir})

	out, err := run(t, read, `{}`)
	if err != nil || !strings.Contains(out, "page text") {
		t.Errorf("read: %s %v", out, err)
	}

	out, err = run(t, shot, `{}`)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if !strings.Contains(out, filepath.Join(agentDir, "browser")) {
		t.Errorf("screenshot path not in jail: %s", out)
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Path == "" {
		t.Errorf("bad screenshot result %q: %v", out, err)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Errorf("screenshot file missing: %v", err)
	}
}

func TestBrowserSessions_ScopedAndTornDown(t *testing.T) {
	fs := newFakeSessions()
	core1 := BrowserToolCore{Sessions: fs, SessionID: "exec-1", AgentDir: t.TempDir()}
	core2 := BrowserToolCore{Sessions: fs, SessionID: "exec-2", AgentDir: t.TempDir()}

	nav1, _, _, _ := newBrowserTool(t, fs, core1)
	nav2, _, _, _ := newBrowserTool(t, fs, core2)
	if _, err := run(t, nav1, `{"url":"https://a.example"}`); err != nil {
		t.Fatalf("nav1: %v", err)
	}
	if _, err := run(t, nav2, `{"url":"https://b.example"}`); err != nil {
		t.Fatalf("nav2: %v", err)
	}

	if len(fs.sessions) != 2 {
		t.Fatalf("expected one session per execution, got %d", len(fs.sessions))
	}

	fs.CloseSession("exec-1")
	if !fs.sessions["exec-1"].closed {
		t.Error("exec-1 session not closed")
	}
	if fs.sessions["exec-2"].closed {
		t.Error("exec-2 session must remain open")
	}
}

func TestBrowserUnavailableError(t *testing.T) {
	fs := newFakeSessions()
	fs.fail = true
	nav, _, _, _ := newBrowserTool(t, fs, BrowserToolCore{Sessions: fs, SessionID: "s1", AgentDir: t.TempDir()})
	_, err := run(t, nav, `{"url":"https://example.com"}`)
	if err == nil || !strings.Contains(err.Error(), "browser unavailable") {
		t.Errorf("expected clear unavailability error, got %v", err)
	}
}

// TestBrowserRodSession_RealChromium is gated: set ONCLAW_BROWSER_INTEGRATION=1
// with a local Chromium available to exercise the go-rod path.
func TestBrowserRodSession_RealChromium(t *testing.T) {
	if os.Getenv("ONCLAW_BROWSER_INTEGRATION") != "1" {
		t.Skip("set ONCLAW_BROWSER_INTEGRATION=1 to run against a real Chromium")
	}
	m := NewBrowserManager()
	s, err := m.Session(context.Background(), "it-1", t.TempDir())
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	defer m.CloseSession("it-1")
	title, err := s.Navigate(context.Background(), "data:text/html,<title>hi</title><h1>hello</h1>")
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if title != "hi" {
		t.Errorf("title %q", title)
	}
}

func TestBrowserSnapshotAndRefTargeting(t *testing.T) {
	fs := newFakeSessions()
	core := BrowserToolCore{Sessions: fs, SessionID: "s1", AgentDir: t.TempDir()}
	snap := &BrowserSnapshotTool{core}
	click := NewRefActionTool(core, "click", false)
	typeArgs := NewRefActionTool(core, "type", true)

	out, err := run(t, snap, `{}`)
	if err != nil || !strings.Contains(out, "101") {
		t.Fatalf("snapshot: %s %v", out, err)
	}

	// A ref from the snapshot works.
	out, err = run(t, click, `{"ref":"101"}`)
	if err != nil || !strings.Contains(out, "elements") {
		t.Fatalf("click by ref: %s %v", out, err)
	}

	// An unknown ref is rejected without touching the session.
	if _, err := run(t, click, `{"ref":"999"}`); err == nil || !strings.Contains(err.Error(), "unknown ref") {
		t.Errorf("expected unknown-ref error, got %v", err)
	}

	// Type requires text.
	if _, err := run(t, typeArgs, `{"ref":"101"}`); err == nil {
		t.Error("expected error for type without text")
	}
	if _, err := run(t, typeArgs, `{"ref":"102","text":"hello"}`); err != nil {
		t.Errorf("type with text: %v", err)
	}
}

func TestRefActionToolValidation(t *testing.T) {
	fs := newFakeSessions()
	core := BrowserToolCore{Sessions: fs, SessionID: "s1", AgentDir: t.TempDir()}
	snap := &BrowserSnapshotTool{core}
	selectOpt := NewRefActionTool(core, "select_option", true)
	drag := NewRefActionTool(core, "drag", false)

	// Refs only validate after a snapshot (mirrors the rod session's rule).
	if _, err := run(t, snap, `{}`); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := run(t, selectOpt, `{"ref":"101"}`); err == nil {
		t.Error("expected error for select_option without value")
	}
	if _, err := run(t, drag, `{"ref":"101"}`); err == nil {
		t.Error("expected error for drag without to_ref")
	}
	if _, err := run(t, drag, `{"ref":"101","to_ref":"102"}`); err != nil {
		t.Errorf("drag with to_ref: %v", err)
	}
}

func TestBrowserActionTimeout(t *testing.T) {
	fs := newFakeSessions()
	core := BrowserToolCore{Sessions: fs, SessionID: "s1", AgentDir: t.TempDir(), ActionTimeout: 20 * time.Millisecond}
	nav := &BrowserNavigateTool{core}

	// The fake session ignores context, so exercise the bound directly:
	// a zero timeout must not wrap, a positive one must produce a deadline.
	ctx, cancel := core.callCtx(context.Background())
	if _, ok := ctx.Deadline(); !ok {
		t.Error("expected deadline with positive action timeout")
	}
	cancel()

	zero := BrowserToolCore{Sessions: fs, SessionID: "s1", AgentDir: t.TempDir()}
	plain, cancelPlain := zero.callCtx(context.Background())
	if _, ok := plain.Deadline(); ok {
		t.Error("expected no deadline with zero action timeout")
	}
	cancelPlain()

	_ = nav
}

func TestBrowserManagerIdleTeardown(t *testing.T) {
	m := NewBrowserManager()
	cfg := BrowserConfig{Headless: true, MaxPages: 3, IdleTimeout: 10 * time.Millisecond}
	m.ApplyConfig(cfg)

	// Register a session through the manager with a fake constructor source —
	// exercise the sweep on fake sessions directly.
	key := sessionKey{sessionID: "s1", agentDir: "dir"}
	fake := &fakeSession{}
	m.mu.Lock()
	m.sessions[key] = fake
	m.lastUsed[key] = time.Now().Add(-50 * time.Millisecond)
	m.mu.Unlock()

	m.sweepIdle()

	m.mu.Lock()
	_, live := m.sessions[key]
	m.mu.Unlock()
	if live {
		t.Error("idle session not swept")
	}
	if !fake.closed {
		t.Error("swept session not closed")
	}
}

func TestBrowserConfigFromToolConfig(t *testing.T) {
	cfg := BrowserConfigFromToolConfig(nil)
	if !cfg.Headless || cfg.MaxPages != 3 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	cfg = BrowserConfigFromToolConfig(map[string]any{
		"headless":               false,
		"remote_cdp_url":         "http://cdp:9222",
		"max_pages":              float64(5),
		"idle_timeout_seconds":   float64(30),
		"action_timeout_seconds": float64(12),
	})
	if cfg.Headless || cfg.CDPURL != "http://cdp:9222" || cfg.MaxPages != 5 ||
		cfg.IdleTimeout != 30*time.Second || cfg.ActionTimeout != 12*time.Second {
		t.Errorf("unexpected config: %+v", cfg)
	}
}
