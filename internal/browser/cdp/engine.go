package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
	"github.com/oniharnantyo/onclaw/internal/browser"
)

// keyMap maps string key names to input.Key values.
// We map common keys that agents use for interactions.
var keyMap = map[string]input.Key{
	"Enter":      input.Enter,
	"Tab":        input.Tab,
	"Backspace":  input.Backspace,
	"Escape":     input.Escape,
	"Space":      input.Space,
	"ArrowUp":    input.ArrowUp,
	"ArrowDown":  input.ArrowDown,
	"ArrowLeft":  input.ArrowLeft,
	"ArrowRight": input.ArrowRight,
	"Home":       input.Home,
	"End":        input.End,
	"PageUp":     input.PageUp,
	"PageDown":   input.PageDown,
	"Delete":     input.Delete,
}

// Engine implements browser.Engine over CDP.
type Engine struct {
	wsURL   string
	browser *rod.Browser
}

// NewEngine creates a new CDP-backed browser Engine.
func NewEngine(wsURL string) *Engine {
	return &Engine{
		wsURL: wsURL,
	}
}

func (e *Engine) Start(ctx context.Context) error {
	if e.wsURL == "" {
		return errors.New("CDP webSocket URL is empty")
	}
	e.browser = rod.New().ControlURL(e.wsURL)
	if err := e.browser.Connect(); err != nil {
		return fmt.Errorf("failed to connect to CDP: %w", err)
	}
	return nil
}

func (e *Engine) Stop(ctx context.Context) error {
	if e.browser != nil {
		err := e.browser.Close()
		e.browser = nil
		return err
	}
	return nil
}

func (e *Engine) NewContext(ctx context.Context, scope string) (browser.Context, error) {
	if e.browser == nil {
		return nil, errors.New("engine not started")
	}
	bCtx, err := e.browser.Incognito()
	if err != nil {
		return nil, fmt.Errorf("failed to create incognito context: %w", err)
	}
	return &cdpContext{
		bCtx: bCtx,
	}, nil
}

type cdpContext struct {
	bCtx *rod.Browser
}

func (c *cdpContext) Close(ctx context.Context) error {
	return c.bCtx.Close()
}

func (c *cdpContext) NewPage(ctx context.Context) (browser.Page, error) {
	page, err := c.bCtx.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, fmt.Errorf("failed to create new page: %w", err)
	}
	p := &cdpPage{
		page: page,
		refs: make(map[string]proto.DOMBackendNodeID),
	}
	p.initConsoleListener()
	return p, nil
}

func (c *cdpContext) Pages(ctx context.Context) ([]browser.Page, error) {
	pages, err := c.bCtx.Pages()
	if err != nil {
		return nil, err
	}
	var res []browser.Page
	for _, p := range pages {
		res = append(res, &cdpPage{
			page: p,
			refs: make(map[string]proto.DOMBackendNodeID),
		})
	}
	return res, nil
}

func (c *cdpContext) Cookies(ctx context.Context) ([]browser.Cookie, error) {
	rawCookies, err := c.bCtx.GetCookies()
	if err != nil {
		return nil, err
	}
	var cookies []browser.Cookie
	for _, rc := range rawCookies {
		cookies = append(cookies, browser.Cookie{
			Name:     rc.Name,
			Value:    rc.Value,
			Domain:   rc.Domain,
			Path:     rc.Path,
			Expires:  time.Unix(int64(rc.Expires), 0),
			HTTPOnly: rc.HTTPOnly,
			Secure:   rc.Secure,
		})
	}
	return cookies, nil
}

func (c *cdpContext) SetCookies(ctx context.Context, cookies []browser.Cookie) error {
	var params []*proto.NetworkCookieParam
	for _, c := range cookies {
		params = append(params, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			Expires:  proto.TimeSinceEpoch(c.Expires.Unix()),
		})
	}
	return c.bCtx.SetCookies(params)
}

type cdpPage struct {
	page *rod.Page
	mu   sync.Mutex
	refs map[string]proto.DOMBackendNodeID
	msgs []browser.ConsoleMsg
}

func (p *cdpPage) Close(ctx context.Context) error {
	return p.page.Close()
}

func (p *cdpPage) Navigate(ctx context.Context, url string) error {
	err := p.page.Context(ctx).Navigate(url)
	if err != nil {
		return err
	}
	return p.page.Context(ctx).WaitLoad()
}

func (p *cdpPage) URL(ctx context.Context) (string, error) {
	info, err := p.page.Info()
	if err != nil {
		return "", err
	}
	return info.URL, nil
}

func (p *cdpPage) Title(ctx context.Context) (string, error) {
	info, err := p.page.Info()
	if err != nil {
		return "", err
	}
	return info.Title, nil
}

func (p *cdpPage) ConsoleMessages(ctx context.Context) ([]browser.ConsoleMsg, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	res := make([]browser.ConsoleMsg, len(p.msgs))
	copy(res, p.msgs)
	return res, nil
}

func (p *cdpPage) initConsoleListener() {
	_, _ = p.page.Call(context.Background(), "", "Runtime.enable", nil)
	go func() {
		p.page.EachEvent(func(e *proto.RuntimeConsoleAPICalled) {
			p.mu.Lock()
			defer p.mu.Unlock()
			var parts []string
			for _, arg := range e.Args {
				if arg.Value.Val() != nil {
					parts = append(parts, fmt.Sprint(arg.Value.Val()))
				} else if arg.Description != "" {
					parts = append(parts, arg.Description)
				}
			}
			p.msgs = append(p.msgs, browser.ConsoleMsg{
				Type: string(e.Type),
				Text: strings.Join(parts, " "),
				Time: time.UnixMilli(int64(e.Timestamp)),
			})
		})
	}()
}

func (p *cdpPage) Screenshot(ctx context.Context, opts browser.ShotOpts) ([]byte, error) {
	return p.page.Context(ctx).Screenshot(opts.FullPage, nil)
}

func (p *cdpPage) Snapshot(ctx context.Context, opts browser.SnapshotOpts) (*browser.Snapshot, error) {
	// 1. Get AXTree
	resBytes, err := p.page.Context(ctx).Call(ctx, "", "Accessibility.getFullAXTree", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get AXTree: %w", err)
	}

	var result proto.AccessibilityGetFullAXTreeResult
	if err := json.Unmarshal(resBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse AXTree: %w", err)
	}

	// Reset ref map
	p.mu.Lock()
	p.refs = make(map[string]proto.DOMBackendNodeID)
	p.mu.Unlock()

	// 2. Format AXTree and collect Refs
	treeLines, refs := FormatAXTree(result.Nodes)

	p.mu.Lock()
	p.refs = refs
	p.mu.Unlock()

	// 3. Get Page Visible Text
	var pageText string
	textObj, err := p.page.Context(ctx).Eval("() => document.body.innerText")
	if err == nil && textObj != nil {
		pageText = textObj.Value.Str()
	}

	return &browser.Snapshot{
		AXTree: strings.Join(treeLines, "\n"),
		Text:   pageText,
	}, nil
}

func (p *cdpPage) Act(ctx context.Context, req browser.ActRequest) error {
	p.mu.Lock()
	backendID, hasRef := p.refs[req.Ref]
	p.mu.Unlock()

	var el *rod.Element
	var err error

	if hasRef {
		el, err = p.page.Context(ctx).ElementFromNode(&proto.DOMNode{
			BackendNodeID: backendID,
		})
		if err != nil {
			return fmt.Errorf("failed to resolve ref %q: %w", req.Ref, err)
		}
	}

	switch strings.ToLower(req.Kind) {
	case "click":
		if el == nil {
			return fmt.Errorf("click requires a valid reference")
		}
		if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("click failed: %w", err)
		}
	case "type":
		if el == nil {
			return fmt.Errorf("type requires a valid reference")
		}
		if err := el.Input(req.Text); err != nil {
			return fmt.Errorf("type failed: %w", err)
		}
	case "hover":
		if el == nil {
			return fmt.Errorf("hover requires a valid reference")
		}
		if err := el.Hover(); err != nil {
			return fmt.Errorf("hover failed: %w", err)
		}
	case "press":
		if el != nil {
			if err := el.Focus(); err != nil {
				return fmt.Errorf("focus failed: %w", err)
			}
		}
		key, ok := keyMap[req.Text]
		if !ok {
			return fmt.Errorf("unsupported key: %s", req.Text)
		}
		if err := p.page.Keyboard.Press(key); err != nil {
			return fmt.Errorf("press key failed: %w", err)
		}
	case "wait":
		delay := req.Delay
		if delay <= 0 {
			delay = 1 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	case "evaluate":
		if el != nil {
			_, err = el.Eval(req.Code)
		} else {
			_, err = p.page.Context(ctx).Eval(req.Code)
		}
		if err != nil {
			return fmt.Errorf("evaluation failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported act kind: %s", req.Kind)
	}

	return nil
}

// FormatAXTree builds a tree from the flat list of accessibility nodes, formats them as lines with indentation,
// and assigns reference IDs (e.g. "e1", "e2") to nodes with non-zero BackendDOMNodeID.
func FormatAXTree(nodes []*proto.AccessibilityAXNode) ([]string, map[string]proto.DOMBackendNodeID) {
	nodeMap := make(map[proto.AccessibilityAXNodeID]*proto.AccessibilityAXNode)
	parentMap := make(map[proto.AccessibilityAXNodeID]proto.AccessibilityAXNodeID)
	for _, node := range nodes {
		nodeMap[node.NodeID] = node
		for _, cid := range node.ChildIDs {
			parentMap[cid] = node.NodeID
		}
	}

	var roots []*proto.AccessibilityAXNode
	for _, node := range nodes {
		if _, hasParent := parentMap[node.NodeID]; !hasParent {
			roots = append(roots, node)
		}
	}

	var treeLines []string
	refs := make(map[string]proto.DOMBackendNodeID)

	var formatNode func(node *proto.AccessibilityAXNode, depth int)
	formatNode = func(node *proto.AccessibilityAXNode, depth int) {
		if node.Ignored {
			for _, cid := range node.ChildIDs {
				if childNode, ok := nodeMap[cid]; ok {
					formatNode(childNode, depth)
				}
			}
			return
		}

		indent := strings.Repeat("  ", depth)
		refStr := ""
		if node.BackendDOMNodeID != 0 {
			refID := len(refs) + 1
			ref := fmt.Sprintf("e%d", refID)
			refs[ref] = node.BackendDOMNodeID
			refStr = fmt.Sprintf("[%s] ", ref)
		}

		role := ""
		if node.Role != nil {
			role = node.Role.Value.Str()
		}
		name := ""
		if node.Name != nil {
			name = node.Name.Value.Str()
		}
		val := ""
		if node.Value != nil {
			val = node.Value.Value.Str()
		}

		var line string
		if name != "" {
			line = fmt.Sprintf("%s%s%s %q", indent, refStr, role, name)
		} else {
			line = fmt.Sprintf("%s%s%s", indent, refStr, role)
		}
		if val != "" {
			line = fmt.Sprintf("%s value=%q", line, val)
		}

		treeLines = append(treeLines, line)

		for _, cid := range node.ChildIDs {
			if childNode, ok := nodeMap[cid]; ok {
				formatNode(childNode, depth+1)
			}
		}
	}

	for _, root := range roots {
		formatNode(root, 0)
	}

	return treeLines, refs
}
