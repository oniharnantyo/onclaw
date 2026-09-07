package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func timeNowUnixMilli() int64 { return time.Now().UnixMilli() }

// newRodSession launches or attaches a browser and opens a blank page.
// agentDir is the jail root; the browser's user-data-dir lives under the OS
// temp directory keyed to it so profiles never collide across agents.
// headless applies only to local launches; a cdpURL attach ignores it
// (design.md D8).
func newRodSession(ctx context.Context, cdpURL string, headless bool, maxPages int, agentDir string) (browserSession, error) {
	url := cdpURL
	if url == "" {
		l := launcher.New().
			UserDataDir(filepath.Join(os.TempDir(), "onclaw-browser", fmt.Sprintf("%x", time.Now().UnixNano()))).
			Headless(headless)
		u, err := l.Launch()
		if err != nil {
			return nil, fmt.Errorf("launch chromium: %w", err)
		}
		url = u
	}
	if maxPages <= 0 {
		maxPages = DefaultBrowserConfig().MaxPages
	}

	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect to %s: %w", cdpURLLabel(cdpURL), err)
	}
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		_ = browser.Close()
		return nil, fmt.Errorf("open page: %w", err)
	}
	return &rodSession{browser: browser, page: page, maxPages: maxPages}, nil
}

func cdpURLLabel(cdpURL string) string {
	if cdpURL == "" {
		return "local chromium"
	}
	return cdpURL
}

// rodSession is the go-rod-backed browserSession.
type rodSession struct {
	browser  *rod.Browser
	page     *rod.Page
	maxPages int
	mu       sync.Mutex
	// refs holds the backend DOM node IDs returned by the latest snapshot;
	// ref-targeted actions validate against it.
	refs map[string]bool
}

func (s *rodSession) Navigate(ctx context.Context, url string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enforceMaxPages(); err != nil {
		return "", err
	}
	page := s.page.Context(ctx)
	if err := page.Navigate(url); err != nil {
		return "", err
	}
	if err := page.WaitLoad(); err != nil {
		// SPA pages may never fire load; a stable DOM is good enough.
		_ = page.WaitStable(500 * time.Millisecond)
	}
	s.refs = nil // refs from the previous page are invalid after navigation
	info, err := page.Info()
	if err != nil {
		return "", err
	}
	return info.Title, nil
}

// enforceMaxPages closes the oldest surplus pages before a new one may open.
// Caller holds s.mu.
func (s *rodSession) enforceMaxPages() error {
	pages, err := s.browser.Pages()
	if err != nil {
		return err
	}
	surplus := len(pages) - s.maxPages
	if surplus <= 0 {
		return nil
	}
	// The session's own page is the navigation target and must survive.
	for _, p := range pages {
		if surplus <= 0 {
			break
		}
		if p == s.page {
			continue
		}
		_ = p.Close()
		surplus--
	}
	return nil
}

func (s *rodSession) Act(ctx context.Context, action, selector, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page := s.page.Context(ctx)
	switch action {
	case "click":
		el, err := page.Element(selector)
		if err != nil {
			return "", err
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"clicked": %q}`, selector), nil
	case "type":
		el, err := page.Element(selector)
		if err != nil {
			return "", err
		}
		if err := el.Input(text); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"typed": true, "selector": %q}`, selector), nil
	case "scroll":
		if _, err := page.Eval(`() => window.scrollBy(0, window.innerHeight * 0.8)`); err != nil {
			return "", err
		}
		return `{"scrolled": true}`, nil
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
}

func (s *rodSession) Read(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.page.Context(ctx).Eval(`() => document.body.innerText`)
	if err != nil {
		return "", err
	}
	return res.Value.String(), nil
}

func (s *rodSession) Screenshot(ctx context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.page.Context(ctx).Screenshot(true, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// snapshotElement is one interactive element of a snapshot payload.
type snapshotElement struct {
	Ref   string `json:"ref"`
	Role  string `json:"role"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

func (s *rodSession) Snapshot(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(s.page.Context(ctx))
}

// axValueString flattens an AXValue payload to its string form.
func axValueString(v *proto.AccessibilityAXValue) string {
	if v == nil {
		return ""
	}
	if s, ok := v.Value.Val().(string); ok {
		return s
	}
	return ""
}

// interactiveAXRole reports whether an accessibility role is worth exposing
// as a ref target to the model.
func interactiveAXRole(role string) bool {
	switch role {
	case "button", "link", "textbox", "searchbox", "combobox", "listbox",
		"option", "checkbox", "radio", "menuitem", "menuitemcheckbox",
		"menuitemradio", "slider", "spinbutton", "switch", "tab", "treeitem":
		return true
	default:
		return false
	}
}

func (s *rodSession) RefAction(ctx context.Context, action, ref, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refs == nil || !s.refs[ref] {
		return "", fmt.Errorf("unknown ref %q — take a fresh browser.snapshot and use a ref it returned", ref)
	}
	page := s.page.Context(ctx)
	el, err := resolveRef(page, ref)
	if err != nil {
		return "", err
	}

	switch action {
	case "click":
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return "", err
		}
	case "type":
		if err := el.Input(text); err != nil {
			return "", err
		}
	case "hover":
		if err := el.Hover(); err != nil {
			return "", err
		}
	case "drag":
		target, err := resolveRef(page, text)
		if err != nil {
			return "", fmt.Errorf("resolve to_ref: %w", err)
		}
		if err := dragElement(page, el, target); err != nil {
			return "", err
		}
	case "select_option":
		if err := el.Select([]string{text}, true, rod.SelectorTypeCSSSector); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}

	// Every interaction invalidates the layout the refs describe; refresh so
	// the model always reasons over the current page state.
	return s.snapshotLocked(page)
}

// resolveRef resolves a backend DOM node ID to a rod element.
func resolveRef(page *rod.Page, ref string) (*rod.Element, error) {
	var backendID int64
	if _, err := fmt.Sscanf(ref, "%d", &backendID); err != nil {
		return nil, fmt.Errorf("invalid ref %q", ref)
	}
	obj, err := proto.DOMResolveNode{BackendNodeID: proto.DOMBackendNodeID(backendID)}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("resolve ref %s: %w", ref, err)
	}
	el, err := page.ElementFromObject(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("element for ref %s: %w", ref, err)
	}
	return el, nil
}

// dragElement drags source onto target with raw mouse events: press at the
// source center, move in steps to the target center, release.
func dragElement(page *rod.Page, source, target *rod.Element) error {
	center := func(el *rod.Element) (proto.Point, error) {
		shape, err := el.Shape()
		if err != nil {
			return proto.Point{}, err
		}
		box := shape.Box()
		return proto.Point{X: box.X + box.Width/2, Y: box.Y + box.Height/2}, nil
	}
	from, err := center(source)
	if err != nil {
		return err
	}
	to, err := center(target)
	if err != nil {
		return err
	}
	mouse := page.Mouse
	if err := mouse.MoveTo(from); err != nil {
		return err
	}
	if err := mouse.Down(proto.InputMouseButtonLeft, 1); err != nil {
		return err
	}
	if err := mouse.MoveLinear(to, 8); err != nil {
		return err
	}
	return mouse.Up(proto.InputMouseButtonLeft, 1)
}

// snapshotLocked captures the AX snapshot with the mutex held; RefAction's
// interaction path has already validated the ref set.
func (s *rodSession) snapshotLocked(page *rod.Page) (string, error) {
	_ = proto.PageEnable{}.Call(page)
	if err := (proto.AccessibilityEnable{}).Call(page); err != nil {
		return "", err
	}
	tree, err := (proto.AccessibilityGetFullAXTree{}).Call(page)
	if err != nil {
		return "", err
	}

	s.refs = make(map[string]bool)
	elements := make([]snapshotElement, 0, len(tree.Nodes))
	for _, node := range tree.Nodes {
		if node == nil || node.Ignored || node.BackendDOMNodeID == 0 {
			continue
		}
		role := axValueString(node.Role)
		if !interactiveAXRole(role) {
			continue
		}
		ref := fmt.Sprintf("%d", node.BackendDOMNodeID)
		if s.refs[ref] {
			continue
		}
		s.refs[ref] = true
		elements = append(elements, snapshotElement{
			Ref:   ref,
			Role:  role,
			Name:  axValueString(node.Name),
			Value: axValueString(node.Value),
		})
	}

	title := ""
	if info, err := page.Info(); err == nil {
		title = info.Title
	}
	payload := struct {
		Title    string            `json:"title"`
		Elements []snapshotElement `json:"elements"`
	}{Title: title, Elements: elements}
	out, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *rodSession) Close() {
	_ = s.browser.Close()
}
