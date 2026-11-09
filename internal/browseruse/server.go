package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ServerName is the MCP server's registered name; the spawning parent's
// namer surfaces the tools as mcp__browser-use__<tool>.
const ServerName = "browser-use"

// profileArgDesc is the shared description of the optional profile argument
// every tool carries.
const profileArgDesc = "Named browser profile to use (its own persistent cookie jar). Defaults to the configured default profile."

// browserTools carries the spawn-time config snapshot and the browser
// manager the tool handlers run against.
type browserTools struct {
	cfg Config
	mgr *manager
}

// allowPrivate reports the effective private-network policy: the spawn flag
// or the server process's environment permits local targets.
func (b *browserTools) allowPrivate() bool {
	return b.cfg.AllowPrivate || envAllowPrivate()
}

// profile resolves a call's profile name: explicit argument, configured
// default, then DefaultBrowserProfile — validated before any filesystem
// access. Handler code goes through resolveProfile instead, which layers the
// host's bound-profile meta on top; this stays for direct resolution.
func (b *browserTools) profile(arg string) (string, error) {
	return ResolveProfile(arg, b.cfg.DefaultProfile)
}

// profileResolution is one call's resolved profile: Key is the sessions-map
// key and the ProfileDir input — a validated profile name, or the bound
// profile's absolute directory — and Name is the display-only name for
// receipts. Unbound, Key and Name are the same string.
type profileResolution struct {
	Key  string
	Name string
}

// resolveProfile is the one seam every handler resolves its profile through
// (design D3 of add-per-agent-browser-profiles): a host-stamped profile-dir
// meta wins over the profile argument entirely — the directory is created on
// demand, keys the sessions map, and never touches ValidateBrowserProfile —
// while any call without the binding resolves exactly like direct CLI use.
// The values ride the request metadata, never the arguments, so the model can
// neither see nor influence the binding.
func (b *browserTools) resolveProfile(req mcp.CallToolRequest, arg string) (profileResolution, error) {
	if dir, ok := metaProfileDir(req); ok {
		// The bound directory is created on first use (private to the user) so
		// a fresh agent's first action — and its takeover readiness check —
		// never needs a pre-existing profile.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return profileResolution{}, fmt.Errorf("browser: create profile dir: %w", err)
		}
		name := metaProfileName(req)
		if name == "" {
			name = filepath.Base(dir)
		}
		return profileResolution{Key: dir, Name: name}, nil
	}
	key, err := b.profile(arg)
	if err != nil {
		return profileResolution{}, err
	}
	return profileResolution{Key: key, Name: key}, nil
}

// metaProfileDir parses the host-stamped profile-dir binding off the call's
// meta: accepted only as a clean absolute path without ".." (the
// screenshotDir precedent's tolerance); absent, empty, relative, or
// traversal-shaped values leave the named-profile path in charge.
func metaProfileDir(req mcp.CallToolRequest) (string, bool) {
	if req.Params.Meta == nil {
		return "", false
	}
	raw, ok := req.Params.Meta.AdditionalFields[MetaProfileDir].(string)
	if !ok || raw == "" {
		return "", false
	}
	cleaned := filepath.Clean(raw)
	if filepath.IsAbs(cleaned) && !strings.Contains(cleaned, "..") {
		return cleaned, true
	}
	return "", false
}

// metaProfileName parses the display-only profile-name binding: any non-empty
// string the host stamped is echoed as-is (an agent slug can exceed the
// profile-name contract by design); empty falls back to the directory's base.
func metaProfileName(req mcp.CallToolRequest) string {
	if req.Params.Meta == nil {
		return ""
	}
	name, _ := req.Params.Meta.AdditionalFields[MetaProfileName].(string)
	return name
}

// ctxTimeout bounds one action by the configured action timeout.
func (b *browserTools) ctxTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, actionTimeout(b.cfg))
}

// NewMCPServer builds the Browser Use MCP server: the 11 discrete page tools,
// the two model-facing handover tools (submit_secret, request_takeover —
// guarded refusals; the value travels through the parent host only), and the
// five __-prefixed internal channel tools the parent filters from the model
// and invokes itself (design D7). The config snapshot comes from the spawn
// flags (design D8); version tags the server info reported at initialize.
func NewMCPServer(cfg Config, version string) *server.MCPServer {
	b := &browserTools{cfg: cfg, mgr: managerInstance()}
	s := server.NewMCPServer(ServerName, version)
	for _, t := range b.toolset() {
		s.AddTool(t.Tool, t.Handler)
	}
	return s
}

// toolset defines the tools with their schemas and handlers: the 11 page
// tools, the 2 guarded handover tools, and the 5 internal channel tools.
func (b *browserTools) toolset() []server.ServerTool {
	return []server.ServerTool{
		{Tool: navigateTool(), Handler: b.handleNavigate},
		{Tool: snapshotTool(), Handler: b.handleSnapshot},
		{Tool: clickTool(), Handler: b.handleClick},
		{Tool: typeTool(), Handler: b.handleType},
		{Tool: pressKeyTool(), Handler: b.handlePressKey},
		{Tool: scrollTool(), Handler: b.handleScroll},
		{Tool: backTool(), Handler: b.handleBack},
		{Tool: screenshotTool(), Handler: b.handleScreenshot},
		{Tool: cookiesTool(), Handler: b.handleCookies},
		{Tool: readTool(), Handler: b.handleRead},
		{Tool: waitTool(), Handler: b.handleWait},
		{Tool: submitSecretTool(), Handler: b.handleSubmitSecret},
		{Tool: requestTakeoverTool(), Handler: b.handleTakeover},
		{Tool: requestSecretTool(), Handler: b.handleRequestSecret},
		{Tool: deliverSecretTool(), Handler: b.handleDeliverSecret},
		{Tool: requestTakeoverInternalTool(), Handler: b.handleRequestTakeover},
		{Tool: beginTakeoverTool(), Handler: b.handleBeginTakeover},
		{Tool: resolveAttentionTool(), Handler: b.handleResolveAttention},
	}
}

// Tool schemas. Each carries its slice of the old tool Info() text; only the
// genuinely required arguments are marked required.

func navigateTool() mcp.Tool {
	return mcp.NewTool("navigate",
		mcp.WithDescription("Navigate the browser to an http(s) URL and wait for it to settle. "+
			"Opens the profile's browser on first use (headless by default); cookies, logins, and local "+
			"storage survive across calls, sessions, and restarts. Refs from earlier snapshots reset on "+
			"every navigate — take a snapshot after navigating. Private and local network addresses are "+
			"blocked unless allow-private is enabled."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The http(s) URL to open.")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func snapshotTool() mcp.Tool {
	return mcp.NewTool("snapshot",
		mcp.WithDescription("List the page's interactive elements (buttons, links, text fields, and headings for "+
			"orientation) with a ref, role, accessible name, and current value. click and type address elements "+
			"by this ref; refs stay valid until the next navigate or back."),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func clickTool() mcp.Tool {
	return mcp.NewTool("click",
		mcp.WithDescription("Click an element addressed by ref (from the latest snapshot) or by its accessible name "+
			"(case-insensitive exact match, falling back to a substring match; an optional role narrows both). "+
			"Ambiguous names are rejected with the candidate list, so one round trip needs no prior snapshot call. "+
			"Returns ok, the final URL and title, and only the elements that changed."),
		mcp.WithString("ref", mcp.Description("Element ref from the latest snapshot.")),
		mcp.WithString("name", mcp.Description("Accessible name of the element to click, e.g. \"Sign in\".")),
		mcp.WithString("role", mcp.Description("Element role (e.g. button, link, textbox) narrowing the name match.")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func typeTool() mcp.Tool {
	return mcp.NewTool("type",
		mcp.WithDescription("Replace an element's text, addressed by ref (from the latest snapshot) or by its "+
			"accessible name (case-insensitive exact match, falling back to a substring match; an optional role "+
			"narrows both). Returns ok, the final URL and title, and only the elements that changed."),
		mcp.WithString("ref", mcp.Description("Element ref from the latest snapshot.")),
		mcp.WithString("name", mcp.Description("Accessible name of the element to type into.")),
		mcp.WithString("role", mcp.Description("Element role (e.g. textbox, searchbox) narrowing the name match.")),
		mcp.WithString("text", mcp.Required(), mcp.Description("The text to enter (replacing existing content).")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func pressKeyTool() mcp.Tool {
	return mcp.NewTool("press_key",
		mcp.WithDescription("Press a named key on the page, e.g. Enter, Tab, Escape, ArrowDown. Returns ok, the final "+
			"URL and title, and only the elements that changed."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key to press, e.g. Enter, Tab, Escape, ArrowDown.")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func scrollTool() mcp.Tool {
	return mcp.NewTool("scroll",
		mcp.WithDescription("Scroll the page one notch up or down. Returns ok, the final URL and title, and only the "+
			"elements that changed."),
		mcp.WithString("direction", mcp.Description("Scroll direction."), mcp.Enum("up", "down")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func backTool() mcp.Tool {
	return mcp.NewTool("back",
		mcp.WithDescription("Navigate one step back in the profile's session history. Refs reset like any navigation. "+
			"Returns ok, the final URL and title, and only the elements that changed."),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func screenshotTool() mcp.Tool {
	return mcp.NewTool("screenshot",
		mcp.WithDescription("Capture the page as a PNG written into the calling session's workspace "+
			"(sessions/<id>/browser/) when the host provides a session directory, otherwise the browser screenshots "+
			"directory; returns the file's path and pixel dimensions. Read the capture back with read_file."),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func cookiesTool() mcp.Tool {
	return mcp.NewTool("cookies",
		mcp.WithDescription("Inspect and manage the profile's cookie jar: list the cookies visible to the current page "+
			"(or all of the profile's cookies when no page is loaded), set a cookie scoped by domain/path and made "+
			"persistent by default, delete a named cookie, or clear every cookie in the profile."),
		mcp.WithString("op", mcp.Required(), mcp.Description("Cookie operation: list, set, delete, or clear."),
			mcp.Enum("list", "set", "delete", "clear")),
		mcp.WithString("name", mcp.Description("Cookie name for set/delete.")),
		mcp.WithString("value", mcp.Description("Cookie value for set.")),
		mcp.WithString("domain", mcp.Description("Cookie domain for set/delete; defaults to the current page.")),
		mcp.WithString("path", mcp.Description("Cookie path for set/delete; defaults to /.")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func readTool() mcp.Tool {
	return mcp.NewTool("read",
		mcp.WithDescription("Read the page's text content, bounded to maxLength characters and truncated mid-page with "+
			"a truncation indication when longer. The full interactive-element list rides snapshot instead."),
		mcp.WithNumber("maxLength", mcp.Description("Maximum characters of page text to return (default 20000, capped at 100000).")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

func waitTool() mcp.Tool {
	return mcp.NewTool("wait",
		mcp.WithDescription("Wait until the page's network activity settles (no ms argument), or for a fixed delay in "+
			"milliseconds (ms given). Reports the page URL."),
		mcp.WithNumber("ms", mcp.Description("Fixed delay in milliseconds; omit to wait for network idle instead.")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
	)
}

// Argument shapes. The JSON keys are the schemas' argument names.

type navigateArgs struct {
	URL     string `json:"url"`
	Profile string `json:"profile"`
}

type targetArgs struct {
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Text    string `json:"text"`
	Profile string `json:"profile"`
}

type keyArgs struct {
	Key     string `json:"key"`
	Profile string `json:"profile"`
}

type scrollArgs struct {
	Direction string `json:"direction"`
	Profile   string `json:"profile"`
}

type profileArgs struct {
	Profile string `json:"profile"`
}

type readArgs struct {
	MaxLength *int   `json:"maxLength"`
	Profile   string `json:"profile"`
}

type waitArgs struct {
	Ms      *int   `json:"ms"`
	Profile string `json:"profile"`
}

// Handlers. Domain failures — refusals, missing arguments, stale targets,
// timeouts — come back as readable tool-error results so the calling turn
// continues; they are never JSON-RPC errors.

func (b *browserTools) handleNavigate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args navigateArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	if strings.TrimSpace(args.URL) == "" {
		return errResult(errors.New("browser: url is required for navigate")), nil
	}
	u, err := validateNavigateURL(ctx, args.URL, b.allowPrivate())
	if err != nil {
		return errResult(err), nil
	}
	// releasedWeakID captures the request whose SPA-keep hold this navigation
	// released, so its delivered registry entry can be dropped after the
	// session lock releases (never hold s.mu while taking the registry's
	// mutex).
	var releasedWeakID string
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		// The urlCarriesSecret guard blocks any navigation whose URL would
		// carry the protected value (design D1's page-echo closure).
		if err := s.guardNavigation(u.String()); err != nil {
			return nil, err
		}
		finalURL, title, err := s.navigate(actx, u.String(), b.allowPrivate())
		if err != nil {
			return nil, err
		}
		// A completed navigation ends the SPA-keep evaluate hold (design D4).
		releasedWeakID = s.releaseWeakProtection()
		return NavigateResult{URL: finalURL, Title: title}, nil
	})
	if releasedWeakID != "" {
		b.mgr.dropDeliveredAttention(releasedWeakID)
	}
	return renderResult(res, err)
}

func (b *browserTools) handleSnapshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args profileArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		// Protection freeze: inspection is refused from request to
		// scrub-release (design D1).
		if err := s.guardInspection(); err != nil {
			return nil, err
		}
		runCtx, cancel := bindCall(s.pageCtx, actx)
		defer cancel()
		elements, err := s.snapshotPage(runCtx)
		if err != nil {
			return nil, err
		}
		pageURL, title := pageInfo(runCtx)
		return SnapshotResult{URL: pageURL, Title: title, Elements: elements}, nil
	})
	return renderResult(res, err)
}

func (b *browserTools) handleClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args targetArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	if args.Ref == "" && args.Name == "" {
		return errResult(errors.New("browser: either ref or name is required for click")), nil
	}
	res, err := b.targetSessionAction(ctx, req, args, func(actx context.Context, s *profileSession) (SlimActionResult, error) {
		return s.clickTarget(actx, TargetQuery{Ref: args.Ref, Name: args.Name, Role: args.Role})
	})
	return renderResult(res, err)
}

func (b *browserTools) handleType(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args targetArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	if args.Text == "" {
		return errResult(errors.New("browser: text is required for type")), nil
	}
	if args.Ref == "" && args.Name == "" {
		return errResult(errors.New("browser: either ref or name is required for type")), nil
	}
	res, err := b.targetSessionAction(ctx, req, args, func(actx context.Context, s *profileSession) (SlimActionResult, error) {
		return s.typeTarget(actx, TargetQuery{Ref: args.Ref, Name: args.Name, Role: args.Role}, args.Text)
	})
	return renderResult(res, err)
}

// targetSessionAction resolves the profile and runs a click/type flow under
// the session's action timeout. The request rides along so the resolution
// sees the host's profile binding.
func (b *browserTools) targetSessionAction(ctx context.Context, req mcp.CallToolRequest, args targetArgs, fn func(actx context.Context, s *profileSession) (SlimActionResult, error)) (any, error) {
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return nil, err
	}
	return b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		return fn(actx, s)
	})
}

func (b *browserTools) handlePressKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args keyArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	if args.Key == "" {
		return errResult(errors.New("browser: key is required for press_key")), nil
	}
	// The key map resolves before the session is acquired, so an unsupported
	// key never launches a browser.
	key, ok := browserKey(args.Key)
	if !ok {
		return errResult(fmt.Errorf("browser: unsupported key %q; use Enter, Tab, Escape, Backspace, Delete, Space, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Home, End, PageUp, PageDown, or a single character", args.Key)), nil
	}
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		return s.pressKey(actx, key, args.Key)
	})
	return renderResult(res, err)
}

func (b *browserTools) handleScroll(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args scrollArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	switch args.Direction {
	case "", "up", "down":
	default:
		return errResult(fmt.Errorf("browser: direction must be up or down, got %q", args.Direction)), nil
	}
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		return s.scroll(actx, args.Direction)
	})
	return renderResult(res, err)
}

func (b *browserTools) handleBack(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args profileArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		return s.back(actx)
	})
	return renderResult(res, err)
}

func (b *browserTools) handleScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args profileArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		// Protection freeze: inspection is refused from request to
		// scrub-release (design D1).
		if err := s.guardInspection(); err != nil {
			return nil, err
		}
		return s.screenshot(actx, screenshotDir(req, b.cfg.BrowserDir))
	})
	return renderResult(res, err)
}

// MetaScreenshotDir is the _meta key the parent stamps on every browser-use
// tool call it forwards for a chat session, carrying the calling session's
// workspace screenshots directory (<workspaces>/<slug>/sessions/<id>/browser).
// The value rides the request metadata — never the arguments, which the
// transcript records — so the model can neither see nor influence it.
const MetaScreenshotDir = "buddies.dev/screenshot-dir"

// MetaProfileDir is the _meta key the parent stamps on every browser-use tool
// call it forwards on behalf of an agent, carrying the agent's own profile
// directory (<workspaces>/<slug>/browser-profile). A call carrying it binds
// the profile: the bound directory becomes the user-data directory and the
// sessions key, and the profile argument is ignored entirely — the model can
// neither see nor influence the binding (design D3). Accepted only as a clean
// absolute path; anything else falls back to named resolution.
const MetaProfileDir = "buddies.dev/profile-dir"

// MetaProfileName is the _meta key carrying the bound profile's display name
// (the agent slug) for receipts such as the takeover-ready echo. Display
// only: any non-empty value is accepted and never validated against the
// profile-name contract, because bound profiles never touch it.
const MetaProfileName = "buddies.dev/profile-name"

// screenshotDir resolves where one screenshot call writes: the meta dir when
// the host supplied a clean absolute path, otherwise the browser dir's own
// screenshots/ (direct child use, the CLI, and the tests). The model-visible
// schema never mentions the directory — the child accepts it only from the
// host's request metadata.
func screenshotDir(req mcp.CallToolRequest, browserDir string) string {
	if req.Params.Meta != nil {
		if raw, ok := req.Params.Meta.AdditionalFields[MetaScreenshotDir].(string); ok && raw != "" {
			cleaned := filepath.Clean(raw)
			if filepath.IsAbs(cleaned) && !strings.Contains(cleaned, "..") {
				return cleaned
			}
		}
	}
	return filepath.Join(browserDir, "screenshots")
}

func (b *browserTools) handleCookies(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args cookiesArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	if err := validateCookiesOp(args); err != nil {
		return errResult(err), nil
	}
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		// Protection freeze: inspection is refused from request to
		// scrub-release (design D1).
		if err := s.guardInspection(); err != nil {
			return nil, err
		}
		switch args.Op {
		case "list":
			return s.cookiesList(actx)
		case "set":
			if err := s.cookiesSet(actx, args); err != nil {
				return nil, err
			}
			return OKResult{OK: true}, nil
		case "delete":
			if err := s.cookiesDelete(actx, args); err != nil {
				return nil, err
			}
			return OKResult{OK: true}, nil
		default:
			if err := s.cookiesClear(actx); err != nil {
				return nil, err
			}
			return OKResult{OK: true}, nil
		}
	})
	return renderResult(res, err)
}

func (b *browserTools) handleRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args readArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	maxLength := 0
	if args.MaxLength != nil {
		maxLength = *args.MaxLength
	}
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		// Protection freeze + SPA-keep hold: the evaluate paths are refused
		// for the full protection arc (design D1/D4).
		if err := s.guardEvaluate(); err != nil {
			return nil, err
		}
		return s.readPage(actx, maxLength)
	})
	return renderResult(res, err)
}

func (b *browserTools) handleWait(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args waitArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	res, err := b.withSession(ctx, prof.Key, func(actx context.Context, s *profileSession) (any, error) {
		if err := s.waitSettle(actx, args.Ms); err != nil {
			return nil, err
		}
		pageURL, _ := pageInfo(s.pageCtx)
		return WaitResult{OK: true, URL: pageURL}, nil
	})
	return renderResult(res, err)
}

// withSession acquires the profile's session, bounds the action by the
// configured timeout, runs fn under the session lock, and exports the cookie
// jar after the action completes — jar durability is what makes a respawn at
// the idle boundary safe. A takeover-held session refuses every public tool.
func (b *browserTools) withSession(ctx context.Context, profile string, fn func(actx context.Context, s *profileSession) (any, error)) (any, error) {
	return b.withSessionInternal(ctx, profile, func(actx context.Context, s *profileSession) (any, error) {
		if err := s.checkHeld(); err != nil {
			return nil, err
		}
		return fn(actx, s)
	})
}

// withSessionInternal is withSession without the takeover-held refusal: the
// internal handover channel must keep reaching a held session (its own fixed
// reason codes decide), while public tools never do.
func (b *browserTools) withSessionInternal(ctx context.Context, profile string, fn func(actx context.Context, s *profileSession) (any, error)) (any, error) {
	s, release, err := b.mgr.acquire(ctx, b.cfg.BrowserDir, profile, b.cfg)
	if err != nil {
		return nil, err
	}
	defer release()
	actx, cancel := b.ctxTimeout(ctx)
	defer cancel()
	res, err := fn(actx, s)
	s.saveCookieJar()
	return res, err
}

// renderResult converts a handler outcome into the tool result: payloads as
// JSON text, errors as readable tool errors (the turn continues).
func renderResult(res any, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return errResult(readableCdpError(err)), nil
	}
	out, err := json.Marshal(res)
	if err != nil {
		return errResult(fmt.Errorf("browser: encode result: %w", err)), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

// cdpSessionNotFoundCode is the JSON-RPC error code the browser answers a
// command with when its CDP session is gone (rod pinned the same code in its
// ErrSessionNotFound).
const cdpSessionNotFoundCode = -32001

// readableCdpError renders protocol and dead-session errors as readable
// text: cdproto's Error.Error() prints "message (code)", and the mapping
// keeps the message only — no raw struct. The dead-session errors (the
// session-not-found protocol code, chromedp's detached-target error) carry
// the recovery hint — the manager relaunches a profile whose browser died
// underneath it on the action's next call. Everything else keeps its message.
func readableCdpError(err error) error {
	var e *cdproto.Error
	if errors.As(err, &e) {
		if e.Code == cdpSessionNotFoundCode {
			return fmt.Errorf("browser: %s — the browser was closed or crashed; the profile relaunches on the next call", e.Message)
		}
		return fmt.Errorf("browser: %s", e.Message)
	}
	if errors.Is(err, chromedp.ErrTargetDetached) {
		return errors.New("browser: the page closed or crashed — the browser was closed or crashed; the profile relaunches on the next call")
	}
	return err
}

// errResult renders a domain error as a readable tool-error result.
func errResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}
