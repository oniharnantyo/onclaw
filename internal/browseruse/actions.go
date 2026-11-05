package browseruse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// scrollDelta is one scroll notch in pixels.
const scrollDelta = 600.0

// actionPollInterval is the tick between two element-state retries (rod's
// WaitInteractable/WaitEnabled sleeper carried the same shape).
const actionPollInterval = 100 * time.Millisecond

// evalUserGesture marks a Runtime.evaluate as user-initiated, matching the
// ByUser evaluations rod performed (programmatic navigation, focus, select).
func evalUserGesture(p *runtime.EvaluateParams) {
	t := true
	p.UserGesture = &t
}

// callFunctionOn runs a function bound to the element the object wraps —
// inside the call, `this` is the element and `document` its own document, so
// probes and mutations stay correct inside same-process frames.
func callFunctionOn(ctx context.Context, el *pageElement, fn string, out any) error {
	_, err := chromedp.Run(ctx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		return callFunctionOnSession(c, t, el.objectID, fn, out)
	}))
	return err
}

// callFunctionOnSession is callFunctionOn against an already-bound target
// session, for composing with other commands inside one action.
func callFunctionOnSession(ctx context.Context, t *chromedp.Target, objectID runtime.RemoteObjectID, fn string, out any) error {
	res, err := cdp.Call(ctx, t, runtime.CallFunctionOn, runtime.CallFunctionOnParams{
		ObjectID:            objectID,
		FunctionDeclaration: fn,
		ReturnByValue:       boolPtr(true),
		UserGesture:         boolPtr(true),
	})
	if err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		return errors.New("browser: page evaluation failed")
	}
	if out != nil && res.Result != nil && len(res.Result.Value) != 0 {
		if err := json.Unmarshal(res.Result.Value, out); err != nil {
			return fmt.Errorf("browser: decode page evaluation: %w", err)
		}
	}
	return nil
}

func boolPtr(v bool) *bool { return &v }

// clickTarget clicks the element addressed by ref or name and returns the
// slim result: ok, final URL and title, and the changed elements only. Name
// targeting takes a fresh snapshot (it becomes the latest) so one round trip
// needs no prior snapshot call.
// Caller holds s.mu.
func (s *profileSession) clickTarget(ctx context.Context, q TargetQuery) (SlimActionResult, error) {
	return s.runTargetAction(ctx, s.pageCtx, q, func(c context.Context, el *pageElement) error {
		if err := clickElement(c, el); err != nil {
			return fmt.Errorf("browser: click %s: %w", targetLabel(q), err)
		}
		return nil
	})
}

// typeTarget replaces the text of the element addressed by ref or name and
// returns the slim result.
// Caller holds s.mu.
func (s *profileSession) typeTarget(ctx context.Context, q TargetQuery, text string) (SlimActionResult, error) {
	return s.runTargetAction(ctx, s.pageCtx, q, func(c context.Context, el *pageElement) error {
		if err := focusElement(c, el); err != nil {
			return fmt.Errorf("browser: focus %s: %w", targetLabel(q), err)
		}
		if err := selectElementText(c, el); err != nil {
			return fmt.Errorf("browser: select text in %s: %w", targetLabel(q), err)
		}
		if err := typeElementText(c, el, text); err != nil {
			return fmt.Errorf("browser: type into %s: %w", targetLabel(q), err)
		}
		return nil
	})
}

// runTargetAction resolves the target, applies the element mutation, settles,
// and builds the slim diff result. The diff's baseline is the latest snapshot
// the session already holds — or, for name targeting, the fresh snapshot the
// resolution just took — so the model only ever sees changes against a page
// state it could have known about.
// Caller holds s.mu.
func (s *profileSession) runTargetAction(ctx context.Context, page context.Context, q TargetQuery, mutate func(context.Context, *pageElement) error) (SlimActionResult, error) {
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	pre := s.elements
	if q.Name != "" {
		// Fresh snapshot: the resolution basis and the diff baseline in one.
		fresh, err := s.snapshotPage(runCtx)
		if err != nil {
			return SlimActionResult{}, err
		}
		pre = fresh
		named, err := resolveTarget(fresh, q)
		if err != nil {
			return SlimActionResult{}, err
		}
		q = TargetQuery{Ref: named.Ref}
	}
	target, err := resolveTarget(pre, q)
	if err != nil {
		return SlimActionResult{}, err
	}
	el, err := s.resolveRef(runCtx, target.Ref)
	if err != nil {
		return SlimActionResult{}, err
	}
	if err := mutate(runCtx, el); err != nil {
		return SlimActionResult{}, err
	}
	s.settle(ctx)
	post, err := s.snapshotPage(runCtx)
	if err != nil {
		return SlimActionResult{}, err
	}
	return slimResult(runCtx, diffElements(pre, post)), nil
}

// clickState is what the in-page click probe reports: the element's center in
// viewport coordinates plus whether it is enabled, or a reason it is not
// clickable yet.
type clickState struct {
	OK      bool    `json:"ok"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Enabled bool    `json:"enabled"`
}

// clickProbeJS reports the element's click point and enabled state: width,
// height, pointer-events, and a coverage check mirroring rod's
// WaitInteractable (visible shape, interactive, not covered at the center).
const clickProbeJS = `function() {
	const r = this.getBoundingClientRect();
	if (r.width <= 0 || r.height <= 0) return { ok: false };
	const x = r.left + r.width / 2, y = r.top + r.height / 2;
	if (getComputedStyle(this).pointerEvents === 'none') return { ok: false };
	const at = document.elementFromPoint(x, y);
	if (!at || (at !== this && !this.contains(at))) return { ok: false };
	return { ok: true, x: x, y: y, enabled: !this.disabled };
}`

// clickElement clicks the element with trusted input events, mirroring rod's
// element click: scroll into view, wait interactable (retrying the probe),
// hover the center, wait enabled, then press and release the left button.
func clickElement(ctx context.Context, el *pageElement) error {
	for {
		var st clickState
		if _, err := chromedp.Run(ctx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
			if _, err := cdp.Call(c, t, dom.ScrollIntoViewIfNeeded, dom.ScrollIntoViewIfNeededParams{
				ObjectID: el.objectID,
			}); err != nil {
				return err
			}
			return callFunctionOnSession(c, t, el.objectID, clickProbeJS, &st)
		})); err != nil {
			return err
		}
		if st.OK && st.Enabled {
			return dispatchClick(ctx, st.X, st.Y)
		}
		if err := sleepCDP(ctx, actionPollInterval); err != nil {
			// The deadline arrived first: report it the way rod's
			// bounded waits did, through the caller's wrapper.
			return ctx.Err()
		}
	}
}

// dispatchClick sends the trusted mouse sequence at the given point: move,
// press, release — the events rod's Mouse.Click produced.
func dispatchClick(ctx context.Context, x, y float64) error {
	_, err := chromedp.Run(ctx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		steps := []input.DispatchMouseEventParams{
			{Type: input.DispatchMouseEventTypeMouseMoved, X: x, Y: y},
			{Type: input.DispatchMouseEventTypeMousePressed, X: x, Y: y,
				Button: input.MouseButtonLeft, Buttons: 1, ClickCount: 1},
			{Type: input.DispatchMouseEventTypeMouseReleased, X: x, Y: y,
				Button: input.MouseButtonLeft, ClickCount: 1},
		}
		for _, p := range steps {
			if _, err := cdp.Call(c, t, input.DispatchMouseEvent, p); err != nil {
				return err
			}
		}
		return nil
	}))
	return err
}

// focusElement scrolls the element into view and focuses it, matching rod's
// element focus.
func focusElement(ctx context.Context, el *pageElement) error {
	_, err := chromedp.Run(ctx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		if _, err := cdp.Call(c, t, dom.ScrollIntoViewIfNeeded, dom.ScrollIntoViewIfNeededParams{
			ObjectID: el.objectID,
		}); err != nil {
			return err
		}
		return callFunctionOnSession(c, t, el.objectID, `function() { this.focus(); }`, nil)
	}))
	return err
}

// selectElementText selects the element's whole content, matching rod's
// SelectAllText (an element without select fails the type here, as it did
// through rod's evaluation exception).
func selectElementText(ctx context.Context, el *pageElement) error {
	return callFunctionOn(ctx, el, `function() { this.select(); }`, nil)
}

// enabledProbeJS reports whether the element accepts input, matching rod's
// WaitEnabled.
const enabledProbeJS = `function() { return !this.disabled; }`

// inputEventJS notifies the page's framework listeners of the typed value,
// matching rod's InputEvent evaluation.
const inputEventJS = `function() { this.dispatchEvent(new Event('input', { bubbles: true })); this.dispatchEvent(new Event('change', { bubbles: true })); }`

// typeElementText waits for the element to accept input, inserts the text
// with Input.insertText (composition-grade trusted input), and notifies the
// page's input/change listeners — the sequence rod's Input performed.
func typeElementText(ctx context.Context, el *pageElement, text string) error {
	var enabled bool
	for {
		if err := callFunctionOn(ctx, el, enabledProbeJS, &enabled); err != nil {
			return err
		}
		if enabled {
			break
		}
		if err := sleepCDP(ctx, actionPollInterval); err != nil {
			return ctx.Err()
		}
	}
	_, err := chromedp.Run(ctx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		if _, err := cdp.Call(c, t, input.InsertText, input.InsertTextParams{Text: text}); err != nil {
			return err
		}
		return callFunctionOnSession(c, t, el.objectID, inputEventJS, nil)
	}))
	return err
}

// slimResult builds the slim action payload from the session page and the
// changed-element delta.
func slimResult(page context.Context, changed []SnapshotElement) SlimActionResult {
	pageURL, title := pageInfo(page)
	if changed == nil {
		changed = []SnapshotElement{} // JSON: an empty delta, never null
	}
	return SlimActionResult{OK: true, URL: pageURL, Title: title, Changed: changed}
}

// targetLabel renders the target for error messages.
func targetLabel(q TargetQuery) string {
	if q.Ref != "" {
		return q.Ref
	}
	return fmt.Sprintf("named element %q", q.Name)
}

// pressKey presses a resolved key on the page and returns the slim result.
// The caller resolves the key name before acquiring the session.
// Caller holds s.mu.
func (s *profileSession) pressKey(ctx context.Context, key rune, keyName string) (SlimActionResult, error) {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	_, err := chromedp.Run(runCtx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		// The kb table is the playwright-derived key/code/virtual-key-code
		// mapping rod's input package carried; each rune encodes to the
		// trusted down/char/up sequence the page expects.
		for _, ev := range kb.Encode(key) {
			if _, err := cdp.Call(c, t, input.DispatchKeyEvent, *ev); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return SlimActionResult{}, fmt.Errorf("browser: press %s: %w", keyName, err)
	}
	s.settle(ctx)
	return s.slimAfterAction(runCtx)
}

// scroll scrolls the page one notch up or down (empty direction = down) and
// returns the slim result.
// Caller holds s.mu.
func (s *profileSession) scroll(ctx context.Context, direction string) (SlimActionResult, error) {
	if direction == "" {
		direction = "down"
	}
	var delta float64
	switch direction {
	case "down":
		delta = scrollDelta
	case "up":
		delta = -scrollDelta
	default:
		return SlimActionResult{}, fmt.Errorf("browser: direction must be up or down, got %q", direction)
	}
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	_, err := chromedp.Run(runCtx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(c, t, input.DispatchMouseEvent, input.DispatchMouseEventParams{
			Type:   input.DispatchMouseEventTypeMouseWheel,
			DeltaY: delta,
		})
		return err
	}))
	if err != nil {
		// Wheel input is occasionally rejected (e.g. no viewport focus);
		// window scrolling is equivalent for the snapshot.
		if _, evalErr := chromedp.Run(runCtx, chromedp.Evaluate[chromedp.Void](
			fmt.Sprintf("window.scrollBy(0, %v)", delta))); evalErr != nil {
			return SlimActionResult{}, fmt.Errorf("browser: scroll: %w", err)
		}
	}
	s.settle(ctx)
	return s.slimAfterAction(runCtx)
}

// back navigates one step back in the session history and returns the slim
// result. Refs reset like any navigation.
// Caller holds s.mu.
func (s *profileSession) back(ctx context.Context) (SlimActionResult, error) {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	// history.back() through the page, as rod did: it never fails on an
	// empty history, and the settle below does the waiting.
	if _, err := chromedp.Run(runCtx, chromedp.Evaluate[chromedp.Void](`history.back()`, evalUserGesture)); err != nil {
		return SlimActionResult{}, fmt.Errorf("browser: back: %w", err)
	}
	pre := s.elements // the model's view of the page being left
	s.settle(ctx)
	s.elements = nil // refs from the previous page are invalid after navigation
	post, err := s.snapshotPage(runCtx)
	if err != nil {
		return SlimActionResult{}, err
	}
	return slimResult(runCtx, diffElements(pre, post)), nil
}

// slimAfterAction snapshots the page and builds the slim diff result against
// the pre-action snapshot held on the session.
// Caller holds s.mu.
func (s *profileSession) slimAfterAction(page context.Context) (SlimActionResult, error) {
	pre := s.elements
	post, err := s.snapshotPage(page)
	if err != nil {
		return SlimActionResult{}, err
	}
	return slimResult(page, diffElements(pre, post)), nil
}

// screenshot captures the page as a PNG under dir — the resolved screenshots
// directory for this call: the calling session's workspace screenshots
// directory when the host names one through the call's _meta, otherwise the
// browser dir's own screenshots/ — and reports the file's path and pixel
// dimensions. The "%d.png" unix-millis filename is load-bearing: the chat
// screenshot read serves exactly files matching ^\d+\.png$ from the
// directory it resolves for the calling session.
// Caller holds s.mu.
func (s *profileSession) screenshot(ctx context.Context, dir string) (ScreenshotResult, error) {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	// Full-page capture beyond the viewport, PNG — the full-page semantics
	// rod's Screenshot(true, PageCaptureScreenshot) provided.
	data, err := chromedp.Run(runCtx, chromedp.FullScreenshot(100))
	if err != nil {
		return ScreenshotResult{}, fmt.Errorf("browser: screenshot: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ScreenshotResult{}, fmt.Errorf("browser: create screenshot dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.png", time.Now().UnixMilli()))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ScreenshotResult{}, fmt.Errorf("browser: write screenshot: %w", err)
	}
	dim, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ScreenshotResult{}, fmt.Errorf("browser: decode screenshot: %w", err)
	}
	return ScreenshotResult{Path: path, Width: dim.Width, Height: dim.Height}, nil
}

// readPage extracts the page's text content bounded to maxLength (the
// requested length is resolved by boundReadLength) and reports the truncation
// indication. Length never fails the call.
// Caller holds s.mu.
func (s *profileSession) readPage(ctx context.Context, maxLength int) (ReadResult, error) {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	text, err := chromedp.Run(runCtx, chromedp.Evaluate[string](`document.body ? document.body.innerText : ""`))
	if err != nil {
		return ReadResult{}, fmt.Errorf("browser: read page text: %w", err)
	}
	text, truncated := boundReadText(text, boundReadLength(maxLength))
	pageURL, title := pageInfo(runCtx)
	return ReadResult{
		OK:        true,
		URL:       pageURL,
		Title:     title,
		Text:      text,
		Truncated: truncated,
		Length:    len([]rune(text)),
	}, nil
}

// boundReadLength resolves the read tool's length bound: absent or
// non-positive means the default, larger requests are clamped to the cap.
func boundReadLength(requested int) int {
	if requested <= 0 {
		return DefaultReadChars
	}
	if requested > MaxReadChars {
		return MaxReadChars
	}
	return requested
}

// boundReadText cuts text to at most max runes, marking the cut with a
// truncation indicator. Length never errors: truncating mid-page is the
// contract.
func boundReadText(text string, max int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= max {
		return text, false
	}
	return string(runes[:max]) + "\n…[truncated]", true
}

// browserKey maps the press_key tool's key names to keyboard runes: the
// named special keys and any single printable ASCII character. The runes are
// the kb table's DOM key symbols (rod's input.Key values, same playwright
// table), which kb.Encode turns into the matching trusted key events.
func browserKey(name string) (rune, bool) {
	switch name {
	case "Enter":
		return []rune(kb.Enter)[0], true
	case "Tab":
		return []rune(kb.Tab)[0], true
	case "Escape":
		return []rune(kb.Escape)[0], true
	case "Backspace":
		return []rune(kb.Backspace)[0], true
	case "Delete":
		return []rune(kb.Delete)[0], true
	case "Space":
		return ' ', true
	case "ArrowUp":
		return []rune(kb.ArrowUp)[0], true
	case "ArrowDown":
		return []rune(kb.ArrowDown)[0], true
	case "ArrowLeft":
		return []rune(kb.ArrowLeft)[0], true
	case "ArrowRight":
		return []rune(kb.ArrowRight)[0], true
	case "Home":
		return []rune(kb.Home)[0], true
	case "End":
		return []rune(kb.End)[0], true
	case "PageUp":
		return []rune(kb.PageUp)[0], true
	case "PageDown":
		return []rune(kb.PageDown)[0], true
	}
	if len(name) == 1 {
		r := []rune(name)[0]
		if r >= 0x20 && r <= 0x7E {
			return r, true
		}
	}
	return 0, false
}
