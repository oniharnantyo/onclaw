package browseruse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

// This file is the browser-takeover handover (design D8): the public
// request_takeover tool (a guarded refusal — the parent host parks the
// attention card), the internal __request_takeover / __begin_takeover /
// __resolve_attention channel tools, and the headed relaunch that hands the
// browser to a person. Held state ends only at resolution or tab close — no
// timeout.

// takeoverArgs is the internal __request_takeover argument shape.
type takeoverArgs struct {
	Reason  string `json:"reason"`
	Profile string `json:"profile"`
	// URL is where the headed window should land — the page the person must
	// act on, named by the agent. Empty falls back to the session's current
	// page.
	URL string `json:"url"`
}

// requestIDArgs is the argument shape of the tools that address a parked
// request by ID (__begin_takeover).
type requestIDArgs struct {
	RequestID string `json:"requestID"`
}

// resolveAttentionArgs is the internal __resolve_attention argument shape.
type resolveAttentionArgs struct {
	RequestID string `json:"requestID"`
	Outcome   string `json:"outcome"`
}

// takeoverReadyResult is the internal channel's takeover-ready receipt.
type takeoverReadyResult struct {
	Status    string `json:"status"` // "ready"
	RequestID string `json:"requestID"`
	Profile   string `json:"profile"`
}

// takeoverActiveResult reports the headed browser took over the session.
type takeoverActiveResult struct {
	Status string `json:"status"` // "active"
}

// attentionResolvedResult reports the attention request ended.
type attentionResolvedResult struct {
	Status string `json:"status"` // "resolved"
}

// requestTakeoverTool builds the public request_takeover schema (design D8).
func requestTakeoverTool() mcp.Tool {
	return mcp.NewTool("request_takeover",
		mcp.WithDescription("Hand the browser to a person: request a headed takeover when sign-in is blocked "+
			"(captcha, SSO, risk checks). The person signs in by hand in the browser window while the session "+
			"stays frozen; the buddies host shows the approval card and drives the handover — invoking this "+
			"tool directly is refused. After they finish, take a fresh snapshot before acting."),
		mcp.WithString("reason", mcp.Required(),
			mcp.Description("Why the handover is needed."),
			mcp.Enum("sign-in-blocked")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
		mcp.WithString("url", mcp.Description("Where the headed window lands: the page the person must act on "+
			"(e.g. the blocked login page). Defaults to the session's current page. http/https only.")),
	)
}

// requestTakeoverInternalTool builds the internal __request_takeover channel
// tool: same contract, addressed by the parent host only (design D7).
func requestTakeoverInternalTool() mcp.Tool {
	return mcp.NewTool("__request_takeover",
		mcp.WithDescription("Internal buddies host channel: park a browser takeover for the person's "+
			"approval. Invoked only by the buddies host; never by the model."),
		mcp.WithString("reason", mcp.Required(),
			mcp.Description("Why the handover is needed."),
			mcp.Enum("sign-in-blocked")),
		mcp.WithString("profile", mcp.Description(profileArgDesc)),
		mcp.WithString("url", mcp.Description("Where the headed window lands; defaults to the session's current page.")),
	)
}

// beginTakeoverTool builds the internal __begin_takeover channel tool.
func beginTakeoverTool() mcp.Tool {
	return mcp.NewTool("__begin_takeover",
		mcp.WithDescription("Internal buddies host channel: relaunch the parked takeover's profile browser "+
			"headed and focused, and hold the session for the person. Invoked only by the buddies host; "+
			"never by the model."),
		mcp.WithString("requestID", mcp.Required(),
			mcp.Description("The requestID returned by the ready receipt.")),
	)
}

// resolveAttentionTool builds the internal __resolve_attention channel tool.
func resolveAttentionTool() mcp.Tool {
	return mcp.NewTool("__resolve_attention",
		mcp.WithDescription("Internal buddies host channel: end an attention request (secret protection or "+
			"takeover hold) without filling anything. Invoked only by the buddies host; never by the model."),
		mcp.WithString("requestID", mcp.Required(),
			mcp.Description("The requestID of the attention request.")),
		mcp.WithString("outcome", mcp.Required(),
			mcp.Description("How the person's attention ended."),
			mcp.Enum("completed", "cancelled", "expired")),
	)
}

// handleTakeover is the public tool's handler: a guarded refusal, always.
func (b *browserTools) handleTakeover(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return errResult(errors.New(takeoverGuard)), nil
}

// handleRequestTakeover parks a takeover request for the parent host. The
// profile must exist: a live session or an on-disk profile directory — a
// bound profile always qualifies, because the resolution seam creates its
// directory on demand (design D4 of add-per-agent-browser-profiles). An
// explicit url names the headed window's landing page; an invalid one
// refuses the request — silently ignoring it would land the person
// somewhere else. The parked entry carries the sessions key (the bound
// directory when bound) so the relaunch opens the right browser; the echo's
// Profile field is the display name.
func (b *browserTools) handleRequestTakeover(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args takeoverArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	if args.Reason != "sign-in-blocked" {
		return errResult(fmt.Errorf("browser: reason must be sign-in-blocked, got %q", args.Reason)), nil
	}
	prof, err := b.resolveProfile(req, args.Profile)
	if err != nil {
		return errResult(err), nil
	}
	if err := b.mgr.profileExists(b.cfg.BrowserDir, prof.Key); err != nil {
		return renderResult(secretUnavailable(reasonProfileMissing), nil)
	}
	if args.URL != "" {
		if _, err := validateNavigateURL(ctx, args.URL, b.allowPrivate()); err != nil {
			return errResult(fmt.Errorf("browser: invalid takeover url: %w", err)), nil
		}
	}
	id := newRequestID()
	b.mgr.parkAttention(&attentionRequest{ID: id, Kind: attentionTakeover, Profile: prof.Key, URL: args.URL})
	return renderResult(takeoverReadyResult{Status: "ready", RequestID: id, Profile: prof.Name}, nil)
}

// handleBeginTakeover relaunches the parked takeover's profile browser headed
// and focused, invalidates the session's refs and snapshot, and holds the
// session: every public tool refuses until __resolve_attention or tab close.
func (b *browserTools) handleBeginTakeover(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args requestIDArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	if args.RequestID == "" {
		return errResult(errors.New("browser: requestID is required for __begin_takeover")), nil
	}
	entry, ok := b.mgr.findAttention(args.RequestID)
	if !ok || entry.Kind != attentionTakeover {
		return errResult(errors.New(inertRefusal)), nil
	}
	s, release, err := b.mgr.relaunchHeaded(ctx, b.cfg.BrowserDir, entry.Profile, b.cfg, entry.URL)
	if err != nil {
		// The takeover cannot happen: end the request so the channel goes
		// inert and the parent can surface the fallback.
		b.mgr.dropAttention(args.RequestID)
		return renderResult(channelFailedResult{Status: "failed", ReasonCode: reasonRelaunchFailed}, nil)
	}
	// A relaunch is a fresh session: refs and the snapshot cache died with the
	// old browser, and the held mark is the only state on the new one.
	s.holdSession(args.RequestID)
	release()
	return renderResult(takeoverActiveResult{Status: "active"}, nil)
}

// handleResolveAttention ends an attention request: the held or protection
// state clears, and any parked request releases without filling. Unknown or
// expired request IDs answer the inert refusal.
func (b *browserTools) handleResolveAttention(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args resolveAttentionArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	if args.Outcome != "completed" && args.Outcome != "cancelled" && args.Outcome != "expired" {
		return errResult(fmt.Errorf("browser: outcome must be completed, cancelled, or expired, got %q", args.Outcome)), nil
	}
	if !b.mgr.resolveAttention(args.RequestID) {
		return errResult(errors.New(inertRefusal)), nil
	}
	return renderResult(attentionResolvedResult{Status: "resolved"}, nil)
}

// profileExists reports whether the profile is usable for a takeover: a live
// session, or an on-disk profile directory from earlier use.
func (m *manager) profileExists(browserDir, name string) error {
	m.mu.Lock()
	_, live := m.sessions[name]
	m.mu.Unlock()
	if live {
		return nil
	}
	dir, err := ProfileDir(browserDir, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("browser: profile %s does not exist yet", name)
	}
	return nil
}

// relaunchHeaded closes any live browser for the profile and relaunches it
// headed and focused — the takeover relaunch overrides the headless config
// for this launch only; the next idle respawn launches from the config again
// (design D8). The headed window lands on takeoverURL when the agent named
// one, else on the URL the session was browsing, so the person takes over
// the page in front of the agent. The returned session is handed out locked.
func (m *manager) relaunchHeaded(ctx context.Context, browserDir, name string, cfg Config, takeoverURL string) (*profileSession, func(), error) {
	m.mu.Lock()
	previous := ""
	if s, ok := m.sessions[name]; ok {
		delete(m.sessions, name)
		s.mu.Lock()
		m.mu.Unlock()
		previous = s.carryOverURL()
		s.saveCookieJar()
		_ = s.closeSession() // best-effort: the headed relaunch replaces it
		s.mu.Unlock()
	} else {
		m.mu.Unlock()
	}

	headed := cfg
	headed.Headless = false
	s, err := m.createSession(ctx, browserDir, name, headed)
	if err != nil {
		return nil, nil, err
	}
	s.idleTTL = idleTTL(headed)
	s.lastUsed = m.now()

	m.mu.Lock()
	if existing, ok := m.sessions[name]; ok {
		// Another goroutine launched the same profile meanwhile; discard the
		// loser and reuse the winner.
		m.mu.Unlock()
		_ = s.closeSession()
		existing.mu.Lock()
		return existing, existing.mu.Unlock, nil
	}
	m.sessions[name] = s
	m.mu.Unlock()

	target := takeoverURL
	if target == "" {
		target = previous
	}
	s.carryOver(target)
	s.focus()
	s.mu.Lock()
	return s, s.mu.Unlock, nil
}

// takeoverNavTimeout bounds the headed relaunch's carry-over navigation.
const takeoverNavTimeout = 15 * time.Second

// carryOverURL returns the live session's current page URL for the headed
// takeover relaunch to land on — empty when the session has no page (a test
// stub), is dead, or is still on the launch blank. Best-effort by contract:
// the person can always navigate by hand, so the takeover never fails over
// this. Caller holds s.mu.
func (s *profileSession) carryOverURL() string {
	if s.pageCtx == nil {
		return ""
	}
	url, _ := pageInfo(s.pageCtx)
	if url == "" || url == "about:blank" {
		return ""
	}
	return url
}

// carryOver navigates the fresh headed window to the URL the agent was
// browsing (design D8: the person takes over the page in front of them, not
// a blank tab). Best-effort: a failed or slow navigation leaves about:blank;
// it is never surfaced and never blocks the hold.
func (s *profileSession) carryOver(url string) {
	if url == "" || s.pageCtx == nil {
		return
	}
	runCtx, cancel := bindCall(s.pageCtx, s.pageCtx)
	defer cancel()
	navCtx, cancelNav := context.WithTimeout(runCtx, takeoverNavTimeout)
	defer cancelNav()
	_, _ = chromedp.Run(navCtx, chromedp.Navigate(url)) // best-effort: see above
}

// focus brings the browser window to the front, best-effort: the person must
// see the sign-in they are taking over. Test stubs without a page stay quiet.
func (s *profileSession) focus() {
	if s.pageCtx == nil {
		return
	}
	runCtx, cancel := bindCall(s.pageCtx, s.pageCtx)
	defer cancel()
	// Page.bringToFront activates the session's tab and raises its window in
	// a headed browser (design D8's focused takeover).
	_ = chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, page.BringToFront, cdp.Empty{})
		return err
	}))
}
