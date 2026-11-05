package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	cdpage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

// This file is the delivery side of the secure-submission handover (design
// D4): the internal __deliver_secret channel tool the parent host invokes
// with the person's value, the trusted-event fill, the submission plan, and
// the scrub sequence (navigation wait, field clearing, document scan, SPA-keep
// vs GET-reload). The value exists in exactly one place here — the fill frame
// of the __deliver_secret call — and is dropped from child memory when the
// call returns.

// scrubNavigationWait bounds the post-submission navigation wait (design D4:
// wait ≤5s; same-document means the scrub's in-page scan decides).
const scrubNavigationWait = 5 * time.Second

// deliverArgs is the internal __deliver_secret argument shape (design D7).
// Unlike the public submit_secret schema, this channel legitimately carries
// the value — from the parent host's memory, never from a model turn.
type deliverArgs struct {
	RequestID string `json:"requestID"`
	Value     string `json:"value"`
	// Identifier is the username-or-email the person typed on the handover
	// card, when the request declared an identifierTarget. It is not a
	// secret — it rides the same protected delivery for one-frame lifetime.
	Identifier string `json:"identifier,omitempty"`
}

// deliverSecretTool builds the internal __deliver_secret channel tool
// (design D7).
func deliverSecretTool() mcp.Tool {
	return mcp.NewTool("__deliver_secret",
		mcp.WithDescription("Internal buddies host channel: deliver the person's secret into the prepared "+
			"authentication fields. Inert without a live prepared request; invoked only by the buddies "+
			"host, never by the model."),
		mcp.WithString("requestID", mcp.Required(),
			mcp.Description("The requestID returned by the ready receipt.")),
		mcp.WithString("value", mcp.Required(),
			mcp.Description("The secret value to type.")),
	)
}

// handleDeliverSecret delivers the value into the parked request's fields.
// Inert without a live prepared request, and inert after the request's one
// delivery attempt — secrets are not retried. The freeze (strong or weak)
// holds until __resolve_attention, which stays resolvable for the entry.
func (b *browserTools) handleDeliverSecret(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args deliverArgs
	if err := req.BindArguments(&args); err != nil {
		return errResult(fmt.Errorf("browser: bad arguments: %w", err)), nil
	}
	if args.RequestID == "" || args.Value == "" {
		return errResult(errors.New("browser: requestID and value are required for __deliver_secret")), nil
	}
	entry := b.mgr.markAttentionDelivered(args.RequestID)
	if entry == nil {
		return errResult(errors.New(inertRefusal)), nil
	}

	res, err := b.withSessionInternal(ctx, entry.Profile, func(actx context.Context, s *profileSession) (any, error) {
		return s.deliverSecret(actx, entry.Secret, args.RequestID, args.Value, args.Identifier)
	})
	return renderResult(res, err)
}

// deliverSecret runs the fill frame: fingerprint re-check, trusted-event
// fill, submission, and the scrub sequence. Every failure keeps the
// protection freeze for __resolve_attention to clear and answers a fixed
// reason code.
// Caller holds s.mu.
func (s *profileSession) deliverSecret(ctx context.Context, plan *parkedSecret, requestID, value, identifier string) (any, error) {
	if s.protection == nil || s.protection.requestID != requestID {
		// The freeze was resolved or lost before delivery: answer inertly
		// rather than typing into an unparked page.
		return channelFailedResult{Status: "failed", ReasonCode: reasonRequestGone}, nil
	}
	page, cancelPage := boundSessionPage(s, ctx)
	defer cancelPage()

	// The value lives only inside this call frame; whatever happens, it is
	// out of child memory when the frame returns.
	s.sealProtectionValue(value)
	defer s.dropProtectionValue()
	// The engine's error logging could echo CDP payloads; the fill frame must
	// never let the value reach even the stderr excerpts the parent surfaces.
	restoreLogs := s.quietLogs()
	defer restoreLogs()

	// Fingerprint re-check, immediately before entry (design D2) — the
	// identifier field alongside the secret targets.
	planFields := plan.targets
	if plan.identifier != nil {
		planFields = append(append([]secretField(nil), plan.targets...), *plan.identifier)
	}
	for _, f := range planFields {
		probe, err := s.probeSecretTarget(ctx, page, f.ref)
		if err != nil || probe.Framed || probe.FP != f.fp {
			return channelFailedResult{Status: "failed", ReasonCode: reasonTargetChanged}, nil
		}
	}

	// Arm the navigation watcher before submission: click or Enter may
	// navigate within milliseconds.
	watch := s.watchNavigation(ctx, page)
	defer watch.stop()

	// Fill: DOM.focus + select() + Input.insertText per target — trusted
	// events that cross open shadow roots (design D4). The identifier
	// (username/email) goes first — login forms order identifier before
	// secret, and Enter submission focus-checks the last filled field.
	filled := make([]string, 0, len(plan.targets)+1)
	if plan.identifier != nil && identifier != "" {
		if err := s.fillSecretField(ctx, page, plan.identifier.ref, identifier); err != nil {
			s.clearFilledQuietly(ctx, page, filled)
			return channelFailedResult{Status: "failed", ReasonCode: reasonFillFailed}, nil
		}
		filled = append(filled, plan.identifier.ref)
	}
	for _, f := range plan.targets {
		if err := s.fillSecretField(ctx, page, f.ref, value); err != nil {
			s.clearFilledQuietly(ctx, page, filled)
			return channelFailedResult{Status: "failed", ReasonCode: reasonFillFailed}, nil
		}
		filled = append(filled, f.ref)
	}

	// Submission per the parked plan.
	switch plan.submission {
	case "click":
		if err := s.clickSubmitTarget(ctx, page, plan.submitTarget); err != nil {
			s.clearFilledQuietly(ctx, page, filled)
			return channelFailedResult{Status: "failed", ReasonCode: reasonSubmitFailed}, nil
		}
	case "enter":
		if err := s.pressEnterOn(ctx, page, filled[len(filled)-1]); err != nil {
			s.clearFilledQuietly(ctx, page, filled)
			return channelFailedResult{Status: "failed", ReasonCode: reasonSubmitFailed}, nil
		}
	}

	// Scrub: wait ≤5s for navigation; a same-document submit goes through the
	// in-page scan (design D4).
	if watch.wait(ctx, scrubNavigationWait) {
		// A real main-frame navigation also ends the protection arc entirely.
		s.releaseProtection()
		return deliverCompletedResult{Status: "completed"}, nil
	}
	if err := s.clearFilledFields(ctx, page, filled); err != nil {
		return channelFailedResult{Status: "failed", ReasonCode: reasonScrubFailed}, nil
	}
	echo, err := s.scanForValue(ctx, page, value)
	if err != nil {
		return channelFailedResult{Status: "failed", ReasonCode: reasonScrubFailed}, nil
	}
	if echo {
		// The value is findable somewhere in the document: replace the
		// document with a GET reload, never replaying a form POST (design D4).
		if err := s.reloadDocument(ctx, page); err != nil {
			return channelFailedResult{Status: "failed", ReasonCode: reasonScrubFailed}, nil
		}
		s.releaseProtection()
		return deliverCompletedResult{Status: "completed"}, nil
	}
	// Clean same-document page: SPA-keep. The freeze releases to the weaker
	// state — evaluate stays blocked until the next real main-frame
	// navigation, which clears history.
	s.weakenProtection()
	return deliverCompletedResult{Status: "completed"}, nil
}

// quietLogs silences the engine's logging for the duration of the fill frame,
// so nothing can carry the value to the stderr excerpts the parent surfaces.
// chromedp keeps no per-browser logger (its own logging never echoes command
// payloads); its default logf writes through the process-wide standard
// logger, so that is what this swaps for the frame. The restore puts the
// original writer back exactly as it was.
func (s *profileSession) quietLogs() func() {
	std := log.Default()
	orig := std.Writer()
	std.SetOutput(io.Discard)
	return func() { std.SetOutput(orig) }
}

// clearFilledQuietly is the failure-path field clear: best-effort, because
// the failure receipt must not depend on the clear succeeding.
func (s *profileSession) clearFilledQuietly(ctx context.Context, page context.Context, refs []string) {
	_ = s.clearFilledFields(ctx, page, refs)
}

// Delivery seams (nil = CDP default), mirroring the settle seams.

// fillSecretField types one field through the fillFn seam (default:
// defaultFillSecretField).
// Caller holds s.mu.
func (s *profileSession) fillSecretField(ctx context.Context, page context.Context, ref, text string) error {
	if s.fillFn != nil {
		return s.fillFn(ctx, page, ref, text)
	}
	return defaultFillSecretField(ctx, page, ref, text)
}

// clickSubmitTarget clicks the submit button through the submitClickFn seam
// (default: defaultClickSubmitTarget).
// Caller holds s.mu.
func (s *profileSession) clickSubmitTarget(ctx context.Context, page context.Context, ref string) error {
	if s.submitClickFn != nil {
		return s.submitClickFn(ctx, page, ref)
	}
	return defaultClickSubmitTarget(ctx, page, ref)
}

// pressEnterOn refocuses the last filled field and presses Enter through the
// submitEnterFn seam (default: defaultPressEnterOn).
// Caller holds s.mu.
func (s *profileSession) pressEnterOn(ctx context.Context, page context.Context, ref string) error {
	if s.submitEnterFn != nil {
		return s.submitEnterFn(ctx, page, ref)
	}
	return defaultPressEnterOn(ctx, page, ref)
}

// watchNavigation arms the navigation watcher through the watchNavFn seam
// (default: defaultWatchNavigation).
// Caller holds s.mu.
func (s *profileSession) watchNavigation(ctx context.Context, page context.Context) *navWatch {
	if s.watchNavFn != nil {
		return s.watchNavFn(ctx, page)
	}
	return defaultWatchNavigation(ctx, page)
}

// clearFilledFields clears the filled fields through the clearFieldsFn seam
// (default: defaultClearFilledFields).
// Caller holds s.mu.
func (s *profileSession) clearFilledFields(ctx context.Context, page context.Context, refs []string) error {
	if s.clearFieldsFn != nil {
		return s.clearFieldsFn(ctx, page, refs)
	}
	return defaultClearFilledFields(ctx, page, refs)
}

// scanForValue scans the live document through the scanFn seam (default:
// defaultScanForValue).
// Caller holds s.mu.
func (s *profileSession) scanForValue(ctx context.Context, page context.Context, value string) (bool, error) {
	if s.scanFn != nil {
		return s.scanFn(ctx, page, value)
	}
	return defaultScanForValue(ctx, page, value)
}

// reloadDocument replaces the document with a GET reload through the
// reloadFn seam (default: defaultReloadDocument).
// Caller holds s.mu.
func (s *profileSession) reloadDocument(ctx context.Context, page context.Context) error {
	if s.reloadFn != nil {
		return s.reloadFn(ctx, page)
	}
	return defaultReloadDocument(ctx, page)
}

// navWatch is the armed navigation watcher produced by watchNavFn.
type navWatch struct {
	// fired closes when a real main-frame navigation happened (history API
	// pushState/replaceState does not count).
	fired chan struct{}
	// stop releases the watcher's resources.
	stop func()
}

// boundSessionPage returns the session's page handle bound to the action
// context: the chromedp context of the session's single tab, carrying ctx's
// deadline and cancellation. Test stubs run without a live page: the nil
// result is only ever handed to seam overrides, never to a CDP default.
// Callers defer the returned cancel to release the binding.
func boundSessionPage(s *profileSession, ctx context.Context) (context.Context, context.CancelFunc) {
	if s.pageCtx == nil {
		return nil, func() {}
	}
	return bindCall(s.pageCtx, ctx)
}

// wait reports whether the watched navigation happened within d.
func (w *navWatch) wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-w.fired:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// resolveRefObject resolves a snapshot ref to the CDP remote object of the
// node, so probes and input events can bind to it even inside open shadow
// roots (backend node ids are session-global). It runs against the session's
// target t from inside a chromedp action.
func resolveRefObject(ctx context.Context, t *chromedp.Target, ref string) (*runtime.RemoteObject, error) {
	var id int64
	if _, err := fmt.Sscanf(ref, "e%d", &id); err != nil {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot", ref)
	}
	res, err := cdp.Call(ctx, t, dom.ResolveNode, dom.ResolveNodeParams{BackendNodeID: cdp.BackendNodeID(id)})
	if err != nil {
		return nil, fmt.Errorf("browser: ref %s is stale; take a new snapshot: %w", ref, err)
	}
	return res.Object, nil
}

// defaultFillSecretField fills one field with trusted events: CDP DOM.focus,
// a JS select() to replace any existing content, then Input.insertText —
// composition-grade input the page cannot distinguish from a keyboard.
func defaultFillSecretField(ctx context.Context, page context.Context, ref, text string) error {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	return chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		obj, err := resolveRefObject(ctx, t, ref)
		if err != nil {
			return err
		}
		if _, err := cdp.Call(ctx, t, dom.Focus, dom.FocusParams{ObjectID: obj.ObjectID}); err != nil {
			return fmt.Errorf("browser: focus authentication field: %w", err)
		}
		if _, err := cdp.Call(ctx, t, runtime.CallFunctionOn, runtime.CallFunctionOnParams{
			ObjectID:            obj.ObjectID,
			FunctionDeclaration: `function() { if (typeof this.select === 'function') this.select(); }`,
			ReturnByValue:       boolPtr(true),
		}); err != nil {
			return fmt.Errorf("browser: select authentication field: %w", err)
		}
		if _, err := cdp.Call(ctx, t, input.InsertText, input.InsertTextParams{Text: text}); err != nil {
			return fmt.Errorf("browser: insert authentication value: %w", err)
		}
		return nil
	}))
}

// defaultClickSubmitTarget waits for the submit button to become enabled
// (in-page MutationObserver poll) and clicks it with trusted mouse events:
// scroll into view, re-check interactability the way rod's element click did
// (pointer-events, shape, occlusion — retried while a transient overlay
// clears), then move the mouse and dispatch a real press and release
// (design D8's trusted-event parity). A button that never becomes clickable
// fails the delivery; the click never lands on whatever happens to sit at
// those coordinates.
func defaultClickSubmitTarget(ctx context.Context, page context.Context, ref string) error {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	return chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		obj, err := resolveRefObject(ctx, t, ref)
		if err != nil {
			return err
		}
		res, err := cdp.Call(ctx, t, runtime.CallFunctionOn, runtime.CallFunctionOnParams{
			ObjectID:            obj.ObjectID,
			FunctionDeclaration: `function() { const btn = this; const enabled = () => !btn.disabled && btn.getAttribute('aria-disabled') !== 'true'; if (enabled()) return true; return new Promise((resolve) => { const obs = new MutationObserver(() => { if (enabled()) { obs.disconnect(); resolve(true); } }); obs.observe(btn, { attributes: true, attributeFilter: ['disabled', 'aria-disabled'], childList: true, subtree: true }); setTimeout(() => { obs.disconnect(); resolve(false); }, 10000); }); }`,
			ReturnByValue:       boolPtr(true),
			AwaitPromise:        boolPtr(true),
		})
		if err != nil {
			return fmt.Errorf("browser: await submit button: %w", err)
		}
		if res.ExceptionDetails != nil {
			return errors.New("browser: await submit button failed")
		}
		deadline := time.Now().Add(submitClickWait)
		var x, y float64
		for {
			if _, err := cdp.Call(ctx, t, dom.ScrollIntoViewIfNeeded, dom.ScrollIntoViewIfNeededParams{ObjectID: obj.ObjectID}); err != nil {
				return fmt.Errorf("browser: scroll submit button: %w", err)
			}
			clickable, px, py, err := elementPoint(ctx, t, obj.ObjectID)
			if err != nil {
				return fmt.Errorf("browser: resolve submit button: %w", err)
			}
			if clickable {
				x, y = px, py
				break
			}
			if time.Now().After(deadline) {
				return errors.New("browser: submit button is covered or not clickable")
			}
			if err := sleepCDP(ctx, actionPollInterval); err != nil {
				return err
			}
		}
		if _, err := cdp.Call(ctx, t, input.DispatchMouseEvent, input.DispatchMouseEventParams{
			Type: input.DispatchMouseEventTypeMouseMoved,
			X:    x,
			Y:    y,
		}); err != nil {
			return fmt.Errorf("browser: move to submit button: %w", err)
		}
		press := input.DispatchMouseEventParams{
			Type:       input.DispatchMouseEventTypeMousePressed,
			X:          x,
			Y:          y,
			Button:     input.MouseButtonLeft,
			Buttons:    1,
			ClickCount: 1,
		}
		if _, err := cdp.Call(ctx, t, input.DispatchMouseEvent, press); err != nil {
			return fmt.Errorf("browser: click submit button: %w", err)
		}
		release := press
		release.Type = input.DispatchMouseEventTypeMouseReleased
		release.Buttons = 0
		if _, err := cdp.Call(ctx, t, input.DispatchMouseEvent, release); err != nil {
			return fmt.Errorf("browser: click submit button: %w", err)
		}
		return nil
	}))
}

// submitClickWait bounds the interactability retry of the submit click — the
// same budget the enable wait above carries.
const submitClickWait = 10 * time.Second

// elementPoint reports whether the element is clickable with a cursor, and
// its viewport center in CSS pixels when it is: a non-zero bounding rect,
// no pointer-events:none, and the topmost element at the center point being
// the element itself or its light-DOM descendant — the checks rod's
// Interactable performed. Input event coordinates are viewport-relative, and
// the rect re-reads the position after the scroll.
func elementPoint(ctx context.Context, t *chromedp.Target, objectID runtime.RemoteObjectID) (bool, float64, float64, error) {
	res, err := cdp.Call(ctx, t, runtime.CallFunctionOn, runtime.CallFunctionOnParams{
		ObjectID:            objectID,
		FunctionDeclaration: `function() { const r = this.getBoundingClientRect(); if (r.width <= 0 || r.height <= 0) return { ok: false }; const x = r.left + r.width / 2, y = r.top + r.height / 2; if (getComputedStyle(this).pointerEvents === 'none') return { ok: false }; const at = document.elementFromPoint(x, y); return { ok: !!at && (at === this || this.contains(at)), x: x, y: y }; }`,
		ReturnByValue:       boolPtr(true),
	})
	if err != nil {
		return false, 0, 0, err
	}
	if res.ExceptionDetails != nil || res.Result == nil {
		return false, 0, 0, errors.New("browser: element point failed")
	}
	var pt struct {
		OK bool    `json:"ok"`
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
	}
	if err := json.Unmarshal(res.Result.Value, &pt); err != nil {
		return false, 0, 0, errors.New("browser: element point failed")
	}
	return pt.OK, pt.X, pt.Y, nil
}

// defaultPressEnterOn refocuses the last filled field and presses Enter with
// trusted key events — the exact event sequence rod's keyboard produced:
// keyDown then keyUp, key "\r", code "Enter", virtual key code 13.
func defaultPressEnterOn(ctx context.Context, page context.Context, ref string) error {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	return chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		obj, err := resolveRefObject(ctx, t, ref)
		if err != nil {
			return err
		}
		if _, err := cdp.Call(ctx, t, dom.Focus, dom.FocusParams{ObjectID: obj.ObjectID}); err != nil {
			return fmt.Errorf("browser: refocus authentication field: %w", err)
		}
		down := input.DispatchKeyEventParams{
			Type:                  input.DispatchKeyEventTypeKeyDown,
			Code:                  "Enter",
			Key:                   "\r",
			Text:                  "\r",
			UnmodifiedText:        "\r",
			WindowsVirtualKeyCode: 13,
		}
		if _, err := cdp.Call(ctx, t, input.DispatchKeyEvent, down); err != nil {
			return fmt.Errorf("browser: press enter: %w", err)
		}
		up := down
		up.Type = input.DispatchKeyEventTypeKeyUp
		if _, err := cdp.Call(ctx, t, input.DispatchKeyEvent, up); err != nil {
			return fmt.Errorf("browser: press enter: %w", err)
		}
		return nil
	}))
}

// defaultWatchNavigation arms a main-frame navigation watcher: same-document
// history navigations fire Page.navigatedWithinDocument (not this event) and
// sub-iframe navigations carry a parent frame — neither counts. The
// subscription runs on its own derived context and ends when that context
// ends, so stop() cancels it without blocking.
func defaultWatchNavigation(ctx context.Context, page context.Context) *navWatch {
	if page == nil {
		// Stub sessions: no live page, no navigation can happen.
		return &navWatch{fired: make(chan struct{}), stop: func() {}}
	}
	runCtx, cancel := bindCall(page, ctx)
	fired := make(chan struct{})
	var once sync.Once
	signal := func() {
		once.Do(func() { close(fired) })
	}
	err := chromedp.Listen(runCtx, func(ev any) {
		if e, ok := ev.(*cdpage.EventFrameNavigated); ok {
			if e.Frame == nil || e.Frame.ParentID != "" {
				return // sub-iframe navigation: not the main frame
			}
			signal()
		}
	})
	if err != nil {
		// The browser is gone (or the context is not a live page): no
		// navigation can arrive, and the watcher reports the timeout instead.
		cancel()
		return &navWatch{fired: make(chan struct{}), stop: func() {}}
	}
	// End the subscription as soon as the watch fired or the context ended —
	// the same early release rod's wait loop performed.
	go func() {
		select {
		case <-fired:
		case <-runCtx.Done():
		}
		cancel()
	}()
	return &navWatch{fired: fired, stop: cancel}
}

// defaultClearFilledFields empties the filled fields and notifies the page's
// framework listeners, so a SPA cannot keep the value in its state.
func defaultClearFilledFields(ctx context.Context, page context.Context, refs []string) error {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	return chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		for _, ref := range refs {
			obj, err := resolveRefObject(ctx, t, ref)
			if err != nil {
				return err
			}
			if _, err := cdp.Call(ctx, t, runtime.CallFunctionOn, runtime.CallFunctionOnParams{
				ObjectID:            obj.ObjectID,
				FunctionDeclaration: `function() { try { this.value = ''; } catch (e) {} this.dispatchEvent(new Event('input', { bubbles: true })); this.dispatchEvent(new Event('change', { bubbles: true })); }`,
				ReturnByValue:       boolPtr(true),
			}); err != nil {
				return fmt.Errorf("browser: clear authentication field: %w", err)
			}
		}
		return nil
	}))
}

// scanOnceJS is the in-page scrub scan body (design D4): document.title,
// location.href, every text node, element values, every attribute, and all
// open shadow roots. It answers a single boolean — never where the value
// was seen.
func scanOnceJS(needleLiteral string) string {
	return `(() => {
	const needle = ` + needleLiteral + `;
	const hits = (text) => typeof text === 'string' && text.indexOf(needle) !== -1;
	const visit = (root, isDoc) => {
		if (isDoc && hits(document.title)) return true;
		if (isDoc && hits(String(location.href))) return true;
		const walker = (root.ownerDocument || root).createTreeWalker(root, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
		let node;
		while ((node = walker.nextNode())) {
			if (node.nodeType === Node.TEXT_NODE) {
				if (hits(node.nodeValue)) return true;
				continue;
			}
			const el = node;
			if (typeof el.value === 'string' && el.value !== '' && hits(el.value)) return true;
			if (el.attributes) {
				for (const attr of el.attributes) {
					if (hits(String(attr.value))) return true;
				}
			}
			if (el.shadowRoot && visit(el.shadowRoot, false)) return true;
		}
		return false;
	};
	return visit(document, true);
})()`
}

// defaultScanForValue runs the in-page scrub scan for the value.
func defaultScanForValue(ctx context.Context, page context.Context, value string) (bool, error) {
	literal, err := json.Marshal(value) // a JSON string is a valid JS string literal
	if err != nil {
		return false, fmt.Errorf("browser: encode scan needle: %w", err)
	}
	var found bool
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	err = chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, runtime.Evaluate, runtime.EvaluateParams{
			Expression:    scanOnceJS(string(literal)),
			ReturnByValue: boolPtr(true),
		})
		if err != nil {
			return fmt.Errorf("browser: scrub scan: %w", err)
		}
		if res.ExceptionDetails != nil || res.Result == nil {
			return errors.New("browser: scrub scan failed")
		}
		return json.Unmarshal(res.Result.Value, &found)
	}))
	if err != nil {
		return false, err
	}
	return found, nil
}

// defaultReloadDocument replaces the document with a fresh GET navigation to
// the current URL: CDP navigation never replays a form POST. The page settles
// so the next parent action sees the replaced document.
func defaultReloadDocument(ctx context.Context, page context.Context) error {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	return chromedp.Do(runCtx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		res, err := cdp.Call(ctx, t, target.GetTargetInfo, target.GetTargetInfoParams{TargetID: t.TargetID})
		if err != nil {
			return fmt.Errorf("browser: page info: %w", err)
		}
		if res.TargetInfo == nil {
			return errors.New("browser: page info: no live tab")
		}
		if _, err := cdp.Call(ctx, t, cdpage.Navigate, cdpage.NavigateParams{URL: res.TargetInfo.URL}); err != nil {
			return fmt.Errorf("browser: reload: %w", err)
		}
		return nil
	}))
}
