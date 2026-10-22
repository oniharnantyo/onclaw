package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

// Snapshot bounds: the result must stay small enough for a tool payload.
const (
	browserMaxSnapshotElements = 150
	browserMaxNameLength       = 120
	browserScrollDelta         = 600.0
)

// browserAXRoleHeading is the one non-interactive role kept in snapshots, for
// page orientation.
const browserAXRoleHeading = "heading"

// runNavigate validates the target, loads it, and re-checks where the load
// actually ended up (design D6: redirects must not smuggle the browser to a
// private target).
func (t *browserTool) runNavigate(ctx context.Context, args browserArgs, cfg BrowserConfig, profile string, allowPrivate bool) (string, error) {
	if strings.TrimSpace(args.URL) == "" {
		return "", errors.New("browser: url is required for navigate")
	}
	u, err := validateNavigateURL(ctx, args.URL, allowPrivate)
	if err != nil {
		return "", err
	}
	return t.withSession(ctx, cfg, profile, func(actx context.Context, s *profileSession) (string, error) {
		finalURL, title, err := s.navigate(actx, u.String(), allowPrivate)
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(browserNavigateResult{URL: finalURL, Title: title})
		if err != nil {
			return "", fmt.Errorf("browser: encode result: %w", err)
		}
		return string(out), nil
	})
}

// validateNavigateURL enforces the navigate policy: http/https only, a host
// present, and — unless allowPrivate — every resolved address public. IP
// literals are classified directly; hostnames are resolved with the default
// resolver.
func validateNavigateURL(ctx context.Context, raw string, allowPrivate bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("browser: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("browser: only http and https URLs are allowed")
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("browser: URL has no host")
	}
	if allowPrivate {
		return u, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return nil, fmt.Errorf("browser: %s is a non-public address and blocked", host)
		}
		return u, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("browser: resolve %s: %w", host, err)
	}
	for _, addr := range addrs {
		if !isPublicIP(addr.IP) {
			return nil, fmt.Errorf("browser: %s resolves to a non-public address and is blocked", host)
		}
	}
	return u, nil
}

// navigate loads target on the session page and waits for it to settle.
// Caller holds s.mu.
func (s *profileSession) navigate(ctx context.Context, target string, allowPrivate bool) (string, string, error) {
	page := s.page.Context(ctx)
	if err := page.Navigate(target); err != nil {
		return "", "", fmt.Errorf("browser: navigate: %w", err)
	}
	settlePage(page)
	s.refs = nil // refs from the previous page are invalid after navigation
	info, err := page.Info()
	if err != nil {
		return "", "", fmt.Errorf("browser: page info: %w", err)
	}
	if err := enforcePostLoadPolicy(ctx, page, info.URL, allowPrivate); err != nil {
		return "", "", err
	}
	return info.URL, info.Title, nil
}

// enforcePostLoadPolicy re-checks the final URL after redirects (design D6)
// and resets the page when it landed somewhere the policy forbids.
func enforcePostLoadPolicy(ctx context.Context, page *rod.Page, finalURL string, allowPrivate bool) error {
	if _, err := validateNavigateURL(ctx, finalURL, allowPrivate); err == nil {
		return nil
	}
	_ = page.Navigate("about:blank") // reset is best-effort; the refusal is the result
	return fmt.Errorf("browser: page landed on %s which violates the private-network policy; reset to about:blank", finalURL)
}

// runSnapshot captures the current page's interactive elements.
func (t *browserTool) runSnapshot(ctx context.Context, s *profileSession) (string, error) {
	page := s.page.Context(ctx)
	elements, err := s.snapshotPage(page)
	if err != nil {
		return "", err
	}
	pageURL, title := pageInfo(page)
	out, err := json.Marshal(browserSnapshotResult{URL: pageURL, Title: title, Elements: elements})
	if err != nil {
		return "", fmt.Errorf("browser: encode result: %w", err)
	}
	return string(out), nil
}

// clickRef clicks the element addressed by ref.
// Caller holds s.mu.
func (s *profileSession) clickRef(ctx context.Context, ref string) ([]browserSnapshotElement, error) {
	page := s.page.Context(ctx)
	el, err := s.resolveRef(page, ref)
	if err != nil {
		return nil, err
	}
	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return nil, fmt.Errorf("browser: click %s: %w", ref, err)
	}
	settlePage(page)
	return s.snapshotPage(page)
}

// typeRef focuses the element addressed by ref, replaces its content, and
// returns the fresh snapshot.
// Caller holds s.mu.
func (s *profileSession) typeRef(ctx context.Context, ref, text string) ([]browserSnapshotElement, error) {
	page := s.page.Context(ctx)
	el, err := s.resolveRef(page, ref)
	if err != nil {
		return nil, err
	}
	if err := el.Focus(); err != nil {
		return nil, fmt.Errorf("browser: focus %s: %w", ref, err)
	}
	if err := el.SelectAllText(); err != nil {
		return nil, fmt.Errorf("browser: select text in %s: %w", ref, err)
	}
	if err := el.Input(text); err != nil {
		return nil, fmt.Errorf("browser: type into %s: %w", ref, err)
	}
	settlePage(page)
	return s.snapshotPage(page)
}

// runPressKey presses a named key on the page.
func (t *browserTool) runPressKey(ctx context.Context, args browserArgs, cfg BrowserConfig, profile string) (string, error) {
	if args.Key == "" {
		return "", errors.New("browser: key is required for press_key")
	}
	key, ok := browserKey(args.Key)
	if !ok {
		return "", fmt.Errorf("browser: unsupported key %q; use Enter, Tab, Escape, Backspace, Delete, Space, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Home, End, PageUp, PageDown, or a single character", args.Key)
	}
	return t.withSession(ctx, cfg, profile, func(actx context.Context, s *profileSession) (string, error) {
		page := s.page.Context(actx)
		if err := page.Keyboard.Type(key); err != nil {
			return "", fmt.Errorf("browser: press %s: %w", args.Key, err)
		}
		settlePage(page)
		elements, err := s.snapshotPage(page)
		if err != nil {
			return "", err
		}
		return s.actionResult(elements)
	})
}

// runScroll scrolls the page one notch up or down (empty direction = down).
func (t *browserTool) runScroll(ctx context.Context, args browserArgs, cfg BrowserConfig, profile string) (string, error) {
	direction := args.Direction
	if direction == "" {
		direction = "down"
	}
	var delta float64
	switch direction {
	case "down":
		delta = browserScrollDelta
	case "up":
		delta = -browserScrollDelta
	default:
		return "", fmt.Errorf("browser: direction must be up or down, got %q", args.Direction)
	}
	return t.withSession(ctx, cfg, profile, func(actx context.Context, s *profileSession) (string, error) {
		page := s.page.Context(actx)
		if err := page.Mouse.Scroll(0, delta, 1); err != nil {
			// Wheel input is occasionally rejected (e.g. no viewport focus);
			// window scrolling is equivalent for the snapshot.
			if _, evalErr := page.Eval(fmt.Sprintf("() => window.scrollBy(0, %v)", delta)); evalErr != nil {
				return "", fmt.Errorf("browser: scroll: %w", err)
			}
		}
		settlePage(page)
		elements, err := s.snapshotPage(page)
		if err != nil {
			return "", err
		}
		return s.actionResult(elements)
	})
}

// runBack navigates one step back in the session history.
func (t *browserTool) runBack(ctx context.Context, s *profileSession) (string, error) {
	page := s.page.Context(ctx)
	if err := page.NavigateBack(); err != nil {
		return "", fmt.Errorf("browser: back: %w", err)
	}
	settlePage(page)
	s.refs = nil
	elements, err := s.snapshotPage(page)
	if err != nil {
		return "", err
	}
	return s.actionResult(elements)
}

// runScreenshot captures the page as a PNG under <browserDir>/screenshots and
// reports the file's path and pixel dimensions (design D7).
func (t *browserTool) runScreenshot(ctx context.Context, s *profileSession) (string, error) {
	page := s.page.Context(ctx)
	data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
	if err != nil {
		return "", fmt.Errorf("browser: screenshot: %w", err)
	}
	dir := filepath.Join(t.dir, "screenshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("browser: create screenshot dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.png", time.Now().UnixMilli()))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("browser: write screenshot: %w", err)
	}
	dim, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("browser: decode screenshot: %w", err)
	}
	out, err := json.Marshal(browserScreenshotResult{Path: path, Width: dim.Width, Height: dim.Height})
	if err != nil {
		return "", fmt.Errorf("browser: encode result: %w", err)
	}
	return string(out), nil
}

// runCookies dispatches the cookie operation (design D8).
func (t *browserTool) runCookies(ctx context.Context, args browserArgs, cfg BrowserConfig, profile string) (string, error) {
	switch args.CookieOp {
	case "set":
		if args.CookieName == "" || args.CookieValue == "" {
			return "", errors.New("browser: cookie_name and cookie_value are required for cookies set")
		}
	case "delete":
		if args.CookieName == "" {
			return "", errors.New("browser: cookie_name is required for cookies delete")
		}
	case "list", "clear":
	default:
		return "", fmt.Errorf("browser: cookie_op must be one of list, set, delete, clear")
	}
	return t.withSession(ctx, cfg, profile, func(actx context.Context, s *profileSession) (string, error) {
		switch args.CookieOp {
		case "list":
			return s.cookiesList(actx)
		case "set":
			return s.cookiesSet(actx, args)
		case "delete":
			return s.cookiesDelete(actx, args)
		default:
			return s.cookiesClear(actx)
		}
	})
}

// cookiesList returns the cookies visible to the current page, or all cookies
// of the profile when no http(s) page is loaded.
// Caller holds s.mu.
func (s *profileSession) cookiesList(ctx context.Context) (string, error) {
	var (
		cookies []*proto.NetworkCookie
		err     error
	)
	pageURL, _ := pageInfo(s.page)
	if u, perr := url.Parse(pageURL); perr == nil && (u.Scheme == "http" || u.Scheme == "https") {
		cookies, err = s.page.Context(ctx).Cookies([]string{pageURL})
	} else {
		cookies, err = s.browser.GetCookies()
	}
	if err != nil {
		return "", fmt.Errorf("browser: list cookies: %w", err)
	}
	out := make([]browserCookie, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, browserCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
		})
	}
	payload, err := json.Marshal(browserCookiesResult{Cookies: out})
	if err != nil {
		return "", fmt.Errorf("browser: encode result: %w", err)
	}
	return string(payload), nil
}

// cookiesSet stores one cookie, scoped by the given domain or the current page.
// Caller holds s.mu.
func (s *profileSession) cookiesSet(ctx context.Context, args browserArgs) (string, error) {
	pageURL, _ := pageInfo(s.page)
	param, err := cookieParamFromArgs(args, time.Now(), pageURL)
	if err != nil {
		return "", err
	}
	if err := s.page.Context(ctx).SetCookies([]*proto.NetworkCookieParam{param}); err != nil {
		return "", fmt.Errorf("browser: set cookie %s: %w", args.CookieName, err)
	}
	return marshalOKResult()
}

// browserCookieTTL is how long a tool-set cookie lives when the caller scopes
// no expiry: one year. A cookie without an expiry is a session cookie, which
// Chrome discards when the profile's browser closes — exactly what the idle
// reaper does — so the default must be persistent.
const browserCookieTTL = 365 * 24 * time.Hour

// cookieParamFromArgs builds the CDP cookie param for cookies set. Without an
// explicit expiry the cookie is made persistent for one year; without a
// domain it is scoped to pageURL (which must be an http(s) URL).
func cookieParamFromArgs(args browserArgs, now time.Time, pageURL string) (*proto.NetworkCookieParam, error) {
	if args.CookieName == "" || args.CookieValue == "" {
		return nil, errors.New("browser: cookie_name and cookie_value are required for cookies set")
	}
	param := &proto.NetworkCookieParam{
		Name:    args.CookieName,
		Value:   args.CookieValue,
		Domain:  args.CookieDomain,
		Path:    args.CookiePath,
		Expires: proto.TimeSinceEpoch(float64(now.Add(browserCookieTTL).Unix())),
	}
	if param.Path == "" {
		param.Path = "/"
	}
	if param.Domain == "" {
		u, perr := url.Parse(pageURL)
		if perr != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("browser: cookie_domain is required when no page is loaded")
		}
		param.URL = pageURL
	}
	return param, nil
}

// cookiesDelete removes the named cookie in the given scope.
// Caller holds s.mu.
func (s *profileSession) cookiesDelete(ctx context.Context, args browserArgs) (string, error) {
	req := proto.NetworkDeleteCookies{Name: args.CookieName, Domain: args.CookieDomain, Path: args.CookiePath}
	if req.Domain == "" {
		if pageURL, _ := pageInfo(s.page); pageURL != "" {
			req.URL = pageURL
		}
	}
	if err := req.Call(s.page.Context(ctx)); err != nil {
		return "", fmt.Errorf("browser: delete cookie %s: %w", args.CookieName, err)
	}
	return marshalOKResult()
}

// cookiesClear removes every cookie of the profile.
// Caller holds s.mu.
func (s *profileSession) cookiesClear(ctx context.Context) (string, error) {
	if err := (proto.NetworkClearBrowserCookies{}).Call(s.page.Context(ctx)); err != nil {
		return "", fmt.Errorf("browser: clear cookies: %w", err)
	}
	return marshalOKResult()
}

// resolveRef resolves a snapshot ref ("e<backendNodeID>") to a live element.
// Caller holds s.mu.
func (s *profileSession) resolveRef(page *rod.Page, ref string) (*rod.Element, error) {
	if s.refs == nil || !s.refs[ref] {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot", ref)
	}
	var id int64
	if _, err := fmt.Sscanf(ref, "e%d", &id); err != nil {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot", ref)
	}
	obj, err := proto.DOMResolveNode{BackendNodeID: proto.DOMBackendNodeID(id)}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot: %w", ref, err)
	}
	el, err := page.ElementFromObject(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot: %w", ref, err)
	}
	return el, nil
}

// snapshotPage captures the accessibility tree as bounded, ref-carrying
// elements and refreshes the session's ref set (design D5).
// Caller holds s.mu.
func (s *profileSession) snapshotPage(page *rod.Page) ([]browserSnapshotElement, error) {
	_ = proto.PageEnable{}.Call(page)
	if err := (proto.AccessibilityEnable{}).Call(page); err != nil {
		return nil, fmt.Errorf("browser: enable accessibility: %w", err)
	}
	tree, err := (proto.AccessibilityGetFullAXTree{}).Call(page)
	if err != nil {
		return nil, fmt.Errorf("browser: accessibility tree: %w", err)
	}
	s.refs = make(map[string]bool, len(tree.Nodes))
	elements := make([]browserSnapshotElement, 0, min(browserMaxSnapshotElements, len(tree.Nodes)))
	for _, node := range tree.Nodes {
		if node == nil || node.Ignored || node.BackendDOMNodeID == 0 {
			continue
		}
		role := axValueString(node.Role)
		interactive := interactiveAXRole(role)
		if !interactive && role != browserAXRoleHeading {
			continue
		}
		name := truncateRunes(axValueString(node.Name), browserMaxNameLength)
		if !interactive && name == "" {
			continue // orientation node with nothing to show
		}
		ref := fmt.Sprintf("e%d", node.BackendDOMNodeID)
		if s.refs[ref] {
			continue
		}
		if len(elements) >= browserMaxSnapshotElements {
			break // refs the model never saw would only invite stale clicks
		}
		s.refs[ref] = true
		elements = append(elements, browserSnapshotElement{
			Ref:   ref,
			Role:  role,
			Name:  name,
			Value: truncateRunes(axValueString(node.Value), browserMaxNameLength),
		})
	}
	return elements, nil
}

// actionResult builds the {ok, url, title, elements} payload of the
// state-changing actions.
// Caller holds s.mu.
func (s *profileSession) actionResult(elements []browserSnapshotElement) (string, error) {
	pageURL, title := pageInfo(s.page)
	out, err := json.Marshal(browserActionResult{OK: true, URL: pageURL, Title: title, Elements: elements})
	if err != nil {
		return "", fmt.Errorf("browser: encode result: %w", err)
	}
	return string(out), nil
}

// settlePage waits for a navigation triggered by the previous operation; on
// pages that never fire load (SPAs), a stable DOM is good enough.
func settlePage(page *rod.Page) {
	if err := page.WaitLoad(); err != nil {
		_ = page.WaitStable(500 * time.Millisecond) // best-effort settle
	}
}

// pageInfo returns the page's current URL and title, tolerating a closing
// page (both come back empty).
func pageInfo(page *rod.Page) (string, string) {
	info, err := page.Info()
	if err != nil {
		return "", ""
	}
	return info.URL, info.Title
}

// axValueString flattens an accessibility value payload to its string form.
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
	case "button", "link", "textbox", "searchbox", "checkbox", "radio",
		"combobox", "listbox", "option", "menuitem", "menuitemcheckbox",
		"menuitemradio", "tab", "switch", "slider", "spinbutton":
		return true
	default:
		return false
	}
}

// truncateRunes cuts s to at most max runes (snapshot bound).
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// browserKey maps a key name to rod's input key. Named special keys and any
// single printable ASCII character are accepted (rod registers every
// US-layout key; unregistered runes would panic in rod's key lookup).
func browserKey(name string) (input.Key, bool) {
	switch name {
	case "Enter":
		return input.Enter, true
	case "Tab":
		return input.Tab, true
	case "Escape":
		return input.Escape, true
	case "Backspace":
		return input.Backspace, true
	case "Delete":
		return input.Delete, true
	case "Space":
		return input.Space, true
	case "ArrowUp":
		return input.ArrowUp, true
	case "ArrowDown":
		return input.ArrowDown, true
	case "ArrowLeft":
		return input.ArrowLeft, true
	case "ArrowRight":
		return input.ArrowRight, true
	case "Home":
		return input.Home, true
	case "End":
		return input.End, true
	case "PageUp":
		return input.PageUp, true
	case "PageDown":
		return input.PageDown, true
	}
	if len(name) == 1 {
		r := []rune(name)[0]
		if r >= 0x20 && r <= 0x7E {
			return input.Key(r), true
		}
	}
	return 0, false
}

// marshalOKResult builds the {ok: true} payload of pure mutations.
func marshalOKResult() (string, error) {
	out, err := json.Marshal(browserOKResult{OK: true})
	if err != nil {
		return "", fmt.Errorf("browser: encode result: %w", err)
	}
	return string(out), nil
}

// cookieJarName is the profile's on-disk cookie jar file. Headless Chrome
// discards session cookies on close even with the restore-on-startup
// preference seeded, so the manager keeps its own jar: exported when a
// browser is torn down, re-injected on the next launch.
const cookieJarName = "buddies-cookies.json"

func cookieJarPath(profileDir string) string {
	return filepath.Join(profileDir, cookieJarName)
}

// cookiesToJar converts live CDP cookies to the on-disk jar form.
func cookiesToJar(cookies []*proto.NetworkCookie) browserCookieJarFile {
	jar := browserCookieJarFile{Cookies: make([]browserStoredCookie, 0, len(cookies))}
	for _, c := range cookies {
		jar.Cookies = append(jar.Cookies, browserStoredCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  float64(c.Expires),
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: string(c.SameSite),
		})
	}
	return jar
}

// jarToParams converts the on-disk jar back into CDP cookie params. A zero
// expiry stays zero: it re-enters the browser as the session cookie it was.
func jarToParams(jar browserCookieJarFile) []*proto.NetworkCookieParam {
	params := make([]*proto.NetworkCookieParam, 0, len(jar.Cookies))
	for _, c := range jar.Cookies {
		params = append(params, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  proto.TimeSinceEpoch(c.Expires),
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: proto.NetworkCookieSameSite(c.SameSite),
		})
	}
	return params
}

// writeCookieJar persists the jar under the profile dir, 0600: it carries
// login state and belongs to the agent's own profile.
func writeCookieJar(profileDir string, jar browserCookieJarFile) error {
	data, err := json.Marshal(jar)
	if err != nil {
		return fmt.Errorf("browser: encode cookie jar: %w", err)
	}
	if err := os.WriteFile(cookieJarPath(profileDir), data, 0o600); err != nil {
		return fmt.Errorf("browser: write cookie jar: %w", err)
	}
	return nil
}

// readCookieJar loads the jar saved at the previous teardown. A missing or
// corrupt jar is not an error: the jar is a cache on top of Chrome's own
// persistence, and a broken file must not wedge the profile.
func readCookieJar(profileDir string) (browserCookieJarFile, error) {
	data, err := os.ReadFile(cookieJarPath(profileDir))
	if os.IsNotExist(err) {
		return browserCookieJarFile{}, nil
	} else if err != nil {
		return browserCookieJarFile{}, fmt.Errorf("browser: read cookie jar: %w", err)
	}
	var jar browserCookieJarFile
	if err := json.Unmarshal(data, &jar); err != nil {
		return browserCookieJarFile{}, nil // corrupt cache: start without it
	}
	return jar, nil
}

// exportCookieJar snapshots every cookie of the profile's browser to the jar
// file. Called while the browser is still connected, at teardown.
func exportCookieJar(browser *rod.Browser, profileDir string) error {
	cookies, err := browser.GetCookies()
	if err != nil {
		return fmt.Errorf("browser: export cookie jar: %w", err)
	}
	return writeCookieJar(profileDir, cookiesToJar(cookies))
}

// injectCookieJar re-injects the jar saved at the previous teardown, right
// after connect and before any action can observe the profile.
func injectCookieJar(browser *rod.Browser, profileDir string) error {
	jar, err := readCookieJar(profileDir)
	if err != nil {
		return err
	}
	params := jarToParams(jar)
	if len(params) == 0 {
		return nil
	}
	if err := browser.SetCookies(params); err != nil {
		return fmt.Errorf("browser: inject cookie jar: %w", err)
	}
	return nil
}
