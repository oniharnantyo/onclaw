package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// cookieTTL is how long a tool-set cookie lives when the caller scopes no
// expiry: one year. A cookie without an expiry is a session cookie, which
// Chrome discards when the profile's browser closes — exactly what the idle
// reaper does — so the default must be persistent.
const cookieTTL = 365 * 24 * time.Hour

// cookiesArgs is the cookies tool's deserialized argument shape.
type cookiesArgs struct {
	Op      string `json:"op,omitempty"`
	Name    string `json:"name,omitempty"`
	Value   string `json:"value,omitempty"`
	Domain  string `json:"domain,omitempty"`
	Path    string `json:"path,omitempty"`
	Profile string `json:"profile,omitempty"`
}

// validateCookiesOp checks the cookies tool's per-op required arguments.
func validateCookiesOp(args cookiesArgs) error {
	switch args.Op {
	case "set":
		if args.Name == "" || args.Value == "" {
			return errors.New("browser: name and value are required for cookies op set")
		}
	case "delete":
		if args.Name == "" {
			return errors.New("browser: name is required for cookies op delete")
		}
	case "list", "clear":
		return nil
	default:
		return errors.New("browser: op must be one of list, set, delete, clear")
	}
	return nil
}

// cookiesList returns the cookies visible to the current page, or all cookies
// of the profile when no http(s) page is loaded.
// Caller holds s.mu.
func (s *profileSession) cookiesList(ctx context.Context) (CookiesResult, error) {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	pageURL, _ := pageInfo(s.pageCtx)
	var cookies []*network.Cookie
	var err error
	if u, perr := url.Parse(pageURL); perr == nil && (u.Scheme == "http" || u.Scheme == "https") {
		_, err = chromedp.Run(runCtx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
			res, cerr := cdp.Call(c, t, network.GetCookies, network.GetCookiesParams{
				URLs: []string{pageURL},
			})
			if cerr != nil {
				return cerr
			}
			cookies = res.Cookies
			return nil
		}))
	} else {
		// No http(s) page: every cookie of the profile's browser context,
		// over the browser endpoint the jar export uses (a browser-context
		// derive, never a page context — see engine.go).
		browserRunCtx, browserCancel := bindCall(s.browserCtx, ctx)
		defer browserCancel()
		cookies, err = browserCookies(browserRunCtx)
	}
	if err != nil {
		return CookiesResult{}, fmt.Errorf("browser: list cookies: %w", err)
	}
	out := make([]Cookie, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  float64(c.Expires),
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
		})
	}
	return CookiesResult{Cookies: out}, nil
}

// cookiesSet stores one cookie, scoped by the given domain or the current
// page.
// Caller holds s.mu.
func (s *profileSession) cookiesSet(ctx context.Context, args cookiesArgs) error {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	pageURL, _ := pageInfo(s.pageCtx)
	param, err := cookieParam(args.Name, args.Value, args.Domain, args.Path, time.Now(), pageURL)
	if err != nil {
		return err
	}
	_, err = chromedp.Run(runCtx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		_, cerr := cdp.Call(c, t, network.SetCookies, network.SetCookiesParams{
			Cookies: []*network.CookieParam{param},
		})
		return cerr
	}))
	if err != nil {
		return fmt.Errorf("browser: set cookie %s: %w", args.Name, err)
	}
	return nil
}

// cookieParam builds the CDP cookie param for cookies set. Without an
// explicit expiry the cookie is made persistent for one year; without a
// domain it is scoped to pageURL (which must be an http(s) URL).
func cookieParam(name, value, domain, path string, now time.Time, pageURL string) (*network.CookieParam, error) {
	if name == "" || value == "" {
		return nil, errors.New("browser: name and value are required for cookies op set")
	}
	param := &network.CookieParam{
		Name:    name,
		Value:   value,
		Domain:  domain,
		Path:    path,
		Expires: cdp.TimeSinceEpoch(float64(now.Add(cookieTTL).Unix())),
	}
	if param.Path == "" {
		param.Path = "/"
	}
	if param.Domain == "" {
		u, perr := url.Parse(pageURL)
		if perr != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("browser: domain is required when no page is loaded")
		}
		param.URL = pageURL
	}
	return param, nil
}

// cookiesDelete removes the named cookie in the given scope.
// Caller holds s.mu.
func (s *profileSession) cookiesDelete(ctx context.Context, args cookiesArgs) error {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	req := network.DeleteCookiesParams{Name: args.Name, Domain: args.Domain, Path: args.Path}
	if req.Domain == "" {
		if pageURL, _ := pageInfo(s.pageCtx); pageURL != "" {
			req.URL = pageURL
		}
	}
	_, err := chromedp.Run(runCtx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		_, cerr := cdp.Call(c, t, network.DeleteCookies, req)
		return cerr
	}))
	if err != nil {
		return fmt.Errorf("browser: delete cookie %s: %w", args.Name, err)
	}
	return nil
}

// cookiesClear removes every cookie of the profile.
// Caller holds s.mu.
func (s *profileSession) cookiesClear(ctx context.Context) error {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	_, err := chromedp.Run(runCtx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		_, cerr := cdp.Call(c, t, network.ClearBrowserCookies, cdp.Empty{})
		return cerr
	}))
	if err != nil {
		return fmt.Errorf("browser: clear cookies: %w", err)
	}
	return nil
}

// cookieJarName is the profile's on-disk cookie jar file. Headless Chrome
// discards session cookies on close even with the restore-on-startup
// preference seeded, so the server keeps its own jar: exported when an action
// completes and again at teardown, re-injected on the next launch.
const cookieJarName = "buddies-cookies.json"

func cookieJarPath(profileDir string) string {
	return filepath.Join(profileDir, cookieJarName)
}

// cookiesToJar converts live CDP cookies to the on-disk jar form.
func cookiesToJar(cookies []*network.Cookie) CookieJarFile {
	jar := CookieJarFile{Cookies: make([]StoredCookie, 0, len(cookies))}
	for _, c := range cookies {
		jar.Cookies = append(jar.Cookies, StoredCookie{
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
func jarToParams(jar CookieJarFile) []*network.CookieParam {
	params := make([]*network.CookieParam, 0, len(jar.Cookies))
	for _, c := range jar.Cookies {
		params = append(params, &network.CookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  cdp.TimeSinceEpoch(c.Expires),
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: network.CookieSameSite(c.SameSite),
		})
	}
	return params
}

// writeCookieJar persists the jar under the profile dir, 0600: it carries
// login state and belongs to the agent's own profile.
func writeCookieJar(profileDir string, jar CookieJarFile) error {
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
func readCookieJar(profileDir string) (CookieJarFile, error) {
	data, err := os.ReadFile(cookieJarPath(profileDir))
	if os.IsNotExist(err) {
		return CookieJarFile{}, nil
	} else if err != nil {
		return CookieJarFile{}, fmt.Errorf("browser: read cookie jar: %w", err)
	}
	var jar CookieJarFile
	if err := json.Unmarshal(data, &jar); err != nil {
		return CookieJarFile{}, nil // corrupt cache: start without it
	}
	return jar, nil
}

// browserCookies reads every cookie of the profile's browser context over
// the browser endpoint — the lookup rod's browser.GetCookies performed (the
// page-level Network.getCookies cannot enumerate the whole store). The
// context may be page- or browser-derived; both carry the browser session.
func browserCookies(ctx context.Context) ([]*network.Cookie, error) {
	browser, ok := browserSession(ctx)
	if !ok {
		return nil, errors.New("browser: no browser session")
	}
	c := chromedp.FromContext(ctx)
	res, err := cdp.Call(ctx, browser, storage.GetCookies, storage.GetCookiesParams{
		BrowserContextID: c.BrowserContextID,
	})
	if err != nil {
		return nil, err
	}
	return res.Cookies, nil
}

// exportCookieJar snapshots every cookie of the profile's browser to the jar
// file. Called while the browser is still connected: after every completed
// action (jar durability for idle reaping) and at teardown. browserCtx is the
// session's browser-level chromedp context; the run context derives from it
// and inherits ctx's deadline (see engine.go).
func exportCookieJar(ctx context.Context, browserCtx context.Context, profileDir string) error {
	runCtx, cancel := bindCall(browserCtx, ctx)
	defer cancel()
	cookies, err := browserCookies(runCtx)
	if err != nil {
		return fmt.Errorf("browser: export cookie jar: %w", err)
	}
	return writeCookieJar(profileDir, cookiesToJar(cookies))
}

// injectCookieJar re-injects the jar saved at the previous teardown, right
// after connect and before any action can observe the profile. Cookies enter
// through the browser endpoint (Storage.setCookies with the profile's browser
// context), the same route rod used — inject runs before the page exists.
func injectCookieJar(ctx context.Context, browserCtx context.Context, profileDir string) error {
	jar, err := readCookieJar(profileDir)
	if err != nil {
		return err
	}
	params := jarToParams(jar)
	if len(params) == 0 {
		return nil
	}
	runCtx, cancel := bindCall(browserCtx, ctx)
	defer cancel()
	browser, ok := browserSession(runCtx)
	if !ok {
		return errors.New("browser: no browser session")
	}
	c := chromedp.FromContext(runCtx)
	_, err = cdp.Call(runCtx, browser, storage.SetCookies, storage.SetCookiesParams{
		Cookies:          params,
		BrowserContextID: c.BrowserContextID,
	})
	if err != nil {
		return fmt.Errorf("browser: inject cookie jar: %w", err)
	}
	return nil
}
