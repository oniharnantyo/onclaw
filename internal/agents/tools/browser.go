package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// NameBrowserPrefix prefixes every browser tool name.
const NameBrowserPrefix = "browser."

// Browser tool names registered in the tool registry.
const (
	NameBrowserNavigate     = "browser.navigate"
	NameBrowserAct          = "browser.act"
	NameBrowserRead         = "browser.read"
	NameBrowserScreenshot   = "browser.screenshot"
	NameBrowserSnapshot     = "browser.snapshot"
	NameBrowserClick        = "browser.click"
	NameBrowserType         = "browser.type"
	NameBrowserHover        = "browser.hover"
	NameBrowserDrag         = "browser.drag"
	NameBrowserSelectOption = "browser.select_option"
)

// BrowserConfig is the workspace-configured browser runtime configuration
// (design.md D8), resolved from the browser tool's settings with instance
// env merged as fallback.
type BrowserConfig struct {
	// Headless launches local Chromium without a window; ignored when
	// CDPURL is set.
	Headless bool
	// CDPURL attaches to a remote browser over CDP instead of launching one.
	CDPURL string
	// MaxPages caps pages open at once per session.
	MaxPages int
	// IdleTimeout closes sessions idle longer than this; zero disables.
	IdleTimeout time.Duration
	// ActionTimeout bounds each tool call; zero uses the library default.
	ActionTimeout time.Duration
}

// DefaultBrowserConfig mirrors the catalog schema defaults.
func DefaultBrowserConfig() BrowserConfig {
	return BrowserConfig{Headless: true, MaxPages: 3}
}

// BrowserConfigFromToolConfig resolves the workspace browser config from the
// tool settings map (values as stored in workspace_tool_settings.config).
func BrowserConfigFromToolConfig(config map[string]any) BrowserConfig {
	cfg := DefaultBrowserConfig()
	if config == nil {
		return cfg
	}
	if v, ok := config["headless"].(bool); ok {
		cfg.Headless = v
	}
	if v, ok := config["remote_cdp_url"].(string); ok && v != "" {
		cfg.CDPURL = v
	}
	if v, ok := config["max_pages"].(float64); ok && v > 0 {
		cfg.MaxPages = int(v)
	}
	if v, ok := config["idle_timeout_seconds"].(float64); ok && v > 0 {
		cfg.IdleTimeout = time.Duration(v * float64(time.Second))
	}
	if v, ok := config["action_timeout_seconds"].(float64); ok && v > 0 {
		cfg.ActionTimeout = time.Duration(v * float64(time.Second))
	}
	return cfg
}

// EnvBrowserCDPURL attaches the browser to a remote CDP endpoint when set.
// When unset, a local Chromium is discovered (CHROME_PATH → PATH → managed
// download) on first use.
const EnvBrowserCDPURL = "ONCLAW_BROWSER_CDP_URL"

// browserSession is one live browser session scoped to an agent execution.
type browserSession interface {
	// Navigate loads a URL and returns the loaded page title.
	Navigate(ctx context.Context, url string) (title string, err error)
	// Act performs click/type/scroll against the current page.
	Act(ctx context.Context, action, selector, text string) (result string, err error)
	// Read returns the current page's readable text.
	Read(ctx context.Context) (string, error)
	// Screenshot writes a full-page PNG to path.
	Screenshot(ctx context.Context, path string) error
	// Snapshot captures an accessibility snapshot whose interactive elements
	// carry stable refs for the ref-targeted tools.
	Snapshot(ctx context.Context) (string, error)
	// RefAction performs a ref-targeted interaction (click, type, hover,
	// drag, select_option) and returns a fresh snapshot.
	RefAction(ctx context.Context, action, ref, text string) (string, error)
	// Close tears down the session's browser.
	Close()
}

// browserSessions creates sessions keyed by execution session ID and tears
// them down. The production implementation wraps go-rod; tests inject fakes.
type browserSessions interface {
	// Session returns (creating on first use) the browser session for an
	// execution. agentDir is the jail root for screenshots.
	Session(ctx context.Context, sessionID, agentDir string) (browserSession, error)
	// CloseSession tears down the session's browser, if live.
	CloseSession(sessionID string)
}

// sessionKey combines the execution session ID and jail root: screenshots and
// downloads must land in the calling agent's workspace.
type sessionKey struct{ sessionID, agentDir string }

// BrowserManager is the production browserSessions: one go-rod browser + page
// per execution session, attached to the workspace-configured remote CDP
// endpoint when set, else a local (headless by default) Chromium. Workspace
// configuration reaches it through the tool context at tool-construction
// time (design.md D8); sessions already live keep their launch configuration.
type BrowserManager struct {
	mu       sync.Mutex
	cdpURL   string
	headless bool
	maxPages int
	idleTTL  time.Duration
	sessions map[sessionKey]browserSession
	lastUsed map[sessionKey]time.Time
	janitor  *time.Ticker
}

// NewBrowserManager builds the manager reading instance configuration from the
// environment (env remains the fallback when a workspace has no settings).
func NewBrowserManager() *BrowserManager {
	cfg := DefaultBrowserConfig()
	if cdp := os.Getenv(EnvBrowserCDPURL); cdp != "" {
		cfg.CDPURL = cdp
	}
	m := &BrowserManager{
		headless: cfg.Headless,
		cdpURL:   cfg.CDPURL,
		maxPages: cfg.MaxPages,
		sessions: map[sessionKey]browserSession{},
		lastUsed: map[sessionKey]time.Time{},
	}
	return m
}

// ApplyConfig applies a workspace's browser configuration to sessions created
// from now on. Live sessions are untouched.
func (m *BrowserManager) ApplyConfig(cfg BrowserConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cdpURL = cfg.CDPURL
	m.headless = cfg.Headless
	m.maxPages = cfg.MaxPages
	m.idleTTL = cfg.IdleTimeout
	m.startJanitorLocked()
}

// startJanitorLocked lazily starts the idle-GC ticker on the first
// configuration that sets an idle timeout. The ticker lives for the process;
// sweeps are cheap no-ops when no sessions are idle.
func (m *BrowserManager) startJanitorLocked() {
	if m.idleTTL <= 0 || m.janitor != nil {
		return
	}
	m.janitor = time.NewTicker(30 * time.Second)
	go func(ticker *time.Ticker) {
		for range ticker.C {
			m.sweepIdle()
		}
	}(m.janitor)
}

// sweepIdle closes sessions whose last use exceeds the idle TTL.
func (m *BrowserManager) sweepIdle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idleTTL <= 0 {
		return
	}
	now := time.Now()
	for key, last := range m.lastUsed {
		if now.Sub(last) > m.idleTTL {
			if s, ok := m.sessions[key]; ok {
				s.Close()
				delete(m.sessions, key)
			}
			delete(m.lastUsed, key)
		}
	}
}

// Session implements browserSessions.
func (m *BrowserManager) Session(ctx context.Context, sessionID, agentDir string) (browserSession, error) {
	key := sessionKey{sessionID, agentDir}
	m.mu.Lock()
	if s, ok := m.sessions[key]; ok {
		m.lastUsed[key] = time.Now()
		m.mu.Unlock()
		return s, nil
	}
	cdp, headless, maxPages := m.cdpURL, m.headless, m.maxPages
	m.mu.Unlock()

	s, err := newRodSession(ctx, cdp, headless, maxPages, agentDir)
	if err != nil {
		return nil, fmt.Errorf("browser unavailable: %w", err)
	}
	m.mu.Lock()
	m.sessions[key] = s
	m.lastUsed[key] = time.Now()
	m.mu.Unlock()
	return s, nil
}

// CloseSession implements browserSessions.
func (m *BrowserManager) CloseSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, s := range m.sessions {
		if key.sessionID == sessionID {
			s.Close()
			delete(m.sessions, key)
			delete(m.lastUsed, key)
		}
	}
}

// BrowserToolCore is the shared, exported plumbing of the browser tools.
// The registry wires Sessions, SessionID, and AgentDir per execution, plus
// the workspace's action timeout.
type BrowserToolCore struct {
	Sessions      browserSessions
	SessionID     string
	AgentDir      string
	ActionTimeout time.Duration
}

// callCtx bounds a tool call by the workspace's action timeout when set.
func (t *BrowserToolCore) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if t.ActionTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, t.ActionTimeout)
}

func (t *BrowserToolCore) session(ctx context.Context) (browserSession, error) {
	return t.Sessions.Session(ctx, t.SessionID, t.AgentDir)
}

// BrowserNavigateTool loads a URL in the session browser.
type BrowserNavigateTool struct{ BrowserToolCore }

func (t *BrowserNavigateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameBrowserNavigate,
		Desc: "Open a URL in the agent browser and wait for the page to load. Returns the page title. Use browser.read afterwards to extract the page text.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {Type: schema.String, Desc: "The absolute http(s) URL to open.", Required: true},
		}),
	}, nil
}

type navigateArgs struct {
	URL string `json:"url"`
}

func (t *BrowserNavigateTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	ctx, cancel := t.callCtx(ctx)
	defer cancel()

	var args navigateArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("browser.navigate: %w", err)
	}
	if args.URL == "" {
		return "", fmt.Errorf("browser.navigate: url is required")
	}
	s, err := t.session(ctx)
	if err != nil {
		return "", fmt.Errorf("browser.navigate: %w", err)
	}
	title, err := s.Navigate(ctx, args.URL)
	if err != nil {
		return "", fmt.Errorf("browser.navigate: %w", err)
	}
	return fmt.Sprintf(`{"loaded": true, "title": %q}`, title), nil
}

// browserActTool clicks, types, and scrolls.
type BrowserActTool struct{ BrowserToolCore }

func (t *BrowserActTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameBrowserAct,
		Desc: "Interact with the current page: click an element, type text into a field, or scroll. Address elements by CSS selector. For type, provide the text to enter.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action":   {Type: schema.String, Desc: "One of: click, type, scroll.", Required: true},
			"selector": {Type: schema.String, Desc: "CSS selector of the target element (not needed for scroll)."},
			"text":     {Type: schema.String, Desc: "Text to type when action is type."},
		}),
	}, nil
}

type actArgs struct {
	Action   string `json:"action"`
	Selector string `json:"selector"`
	Text     string `json:"text"`
}

func (t *BrowserActTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	ctx, cancel := t.callCtx(ctx)
	defer cancel()

	var args actArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("browser.act: %w", err)
	}
	switch args.Action {
	case "click", "type", "scroll":
	default:
		return "", fmt.Errorf("browser.act: action must be click, type, or scroll")
	}
	if args.Action != "scroll" && args.Selector == "" {
		return "", fmt.Errorf("browser.act: selector is required for %s", args.Action)
	}
	s, err := t.session(ctx)
	if err != nil {
		return "", fmt.Errorf("browser.act: %w", err)
	}
	result, err := s.Act(ctx, args.Action, args.Selector, args.Text)
	if err != nil {
		return "", fmt.Errorf("browser.act: %w", err)
	}
	return result, nil
}

// browserReadTool extracts the current page's text.
type BrowserReadTool struct{ BrowserToolCore }

func (t *BrowserReadTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        NameBrowserRead,
		Desc:        "Read the visible text content of the current page in the agent browser.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (t *BrowserReadTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	ctx, cancel := t.callCtx(ctx)
	defer cancel()

	s, err := t.session(ctx)
	if err != nil {
		return "", fmt.Errorf("browser.read: %w", err)
	}
	text, err := s.Read(ctx)
	if err != nil {
		return "", fmt.Errorf("browser.read: %w", err)
	}
	out, err := json.Marshal(map[string]string{"content": text})
	if err != nil {
		return "", fmt.Errorf("browser.read: %w", err)
	}
	return string(out), nil
}

// browserScreenshotTool captures a full-page PNG into the agent jail.
type BrowserScreenshotTool struct{ BrowserToolCore }

func (t *BrowserScreenshotTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        NameBrowserScreenshot,
		Desc:        "Capture a full-page screenshot of the current page as a PNG into the agent workspace. Returns the saved file path.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (t *BrowserScreenshotTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	ctx, cancel := t.callCtx(ctx)
	defer cancel()

	s, err := t.session(ctx)
	if err != nil {
		return "", fmt.Errorf("browser.screenshot: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(t.AgentDir, "browser"), 0o755); err != nil {
		return "", fmt.Errorf("browser.screenshot: %w", err)
	}
	path := filepath.Join(t.AgentDir, "browser", fmt.Sprintf("screenshot-%d.png", timeNowUnixMilli()))
	if err := s.Screenshot(ctx, path); err != nil {
		return "", fmt.Errorf("browser.screenshot: %w", err)
	}
	out, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return "", fmt.Errorf("browser.screenshot: %w", err)
	}
	return string(out), nil
}

// browserSnapshotTool captures an accessibility snapshot with element refs.
type BrowserSnapshotTool struct{ BrowserToolCore }

func (t *BrowserSnapshotTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        NameBrowserSnapshot,
		Desc:        "Capture an accessibility snapshot of the current page. Interactive elements carry a ref; pass a ref to browser.click, browser.type, browser.hover, browser.drag, or browser.select_option. Take a new snapshot after navigation — refs from an older snapshot are rejected.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (t *BrowserSnapshotTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	ctx, cancel := t.callCtx(ctx)
	defer cancel()
	s, err := t.session(ctx)
	if err != nil {
		return "", fmt.Errorf("%s: %w", NameBrowserSnapshot, err)
	}
	out, err := s.Snapshot(ctx)
	if err != nil {
		return "", fmt.Errorf("%s: %w", NameBrowserSnapshot, err)
	}
	return out, nil
}

// refActionArgs is the shared argument shape of the ref-targeted tools.
type refActionArgs struct {
	Ref   string `json:"ref"`
	Text  string `json:"text,omitempty"`
	Value string `json:"value,omitempty"`
}

// refActionTool is one ref-targeted interaction tool (click, type, hover,
// drag, select_option). Action names the operation; requiresText and
// requiresValue validate the argument pairing per action.
type refActionTool struct {
	BrowserToolCore
	action       string
	requiresText bool
}

func (t *refActionTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	params := map[string]*schema.ParameterInfo{
		"ref": {Type: schema.String, Desc: "Element ref from the latest browser.snapshot.", Required: true},
	}
	desc := ""
	switch t.action {
	case "click":
		desc = "Click the element with the given ref from the latest browser.snapshot. Returns a fresh snapshot."
	case "type":
		desc = "Type text into the element with the given ref (input or textarea). Returns a fresh snapshot."
		params["text"] = &schema.ParameterInfo{Type: schema.String, Desc: "The text to enter.", Required: true}
	case "hover":
		desc = "Hover the pointer over the element with the given ref. Returns a fresh snapshot."
	case "drag":
		desc = "Drag from the element with the given ref to the element named by to_ref. Returns a fresh snapshot."
		params["to_ref"] = &schema.ParameterInfo{Type: schema.String, Desc: "Ref of the drop target element.", Required: true}
	case "select_option":
		desc = "Select the option with the given value on the select element with the given ref. Returns a fresh snapshot."
		params["value"] = &schema.ParameterInfo{Type: schema.String, Desc: "The option value to select.", Required: true}
	}
	return &schema.ToolInfo{
		Name:        NameBrowserPrefix + t.action,
		Desc:        desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

type dragArgs struct {
	Ref   string `json:"ref"`
	ToRef string `json:"to_ref"`
}

func (t *refActionTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	ctx, cancel := t.callCtx(ctx)
	defer cancel()
	text := ""
	if t.action == "drag" {
		var args dragArgs
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("%s: %w", NameBrowserPrefix+t.action, err)
		}
		if args.Ref == "" || args.ToRef == "" {
			return "", fmt.Errorf("%s: ref and to_ref are required", NameBrowserPrefix+t.action)
		}
		text = args.ToRef
	} else {
		var args refActionArgs
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("%s: %w", NameBrowserPrefix+t.action, err)
		}
		if args.Ref == "" {
			return "", fmt.Errorf("%s: ref is required", NameBrowserPrefix+t.action)
		}
		if t.requiresText {
			if args.Text == "" && args.Value == "" {
				return "", fmt.Errorf("%s: %s is required", NameBrowserPrefix+t.action, map[string]string{"type": "text", "select_option": "value"}[t.action])
			}
			text = args.Text
			if t.action == "select_option" {
				text = args.Value
			}
		}
	}
	s, err := t.session(ctx)
	if err != nil {
		return "", fmt.Errorf("%s: %w", NameBrowserPrefix+t.action, err)
	}
	out, err := s.RefAction(ctx, t.action, argsRef(t.action, argsJSON), text)
	if err != nil {
		return "", fmt.Errorf("%s: %w", NameBrowserPrefix+t.action, err)
	}
	return out, nil
}

// argsRef re-extracts the primary ref for the session call.
func argsRef(action, argsJSON string) string {
	if action == "drag" {
		var args dragArgs
		_ = json.Unmarshal([]byte(argsJSON), &args)
		return args.Ref
	}
	var args refActionArgs
	_ = json.Unmarshal([]byte(argsJSON), &args)
	return args.Ref
}

// NewRefActionTool constructs a ref-targeted interaction tool: action is one
// of click, type, hover, drag, select_option; requiresText marks the actions
// that carry a text/value argument.
func NewRefActionTool(core BrowserToolCore, action string, requiresText bool) tool.BaseTool {
	return &refActionTool{BrowserToolCore: core, action: action, requiresText: requiresText}
}
