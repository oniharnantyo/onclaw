package browseruse

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdpage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// navigate loads target on the session page, waits for it to settle, and
// re-checks where the load actually ended up (redirects must not smuggle the
// tailInfoTimeout bounds the detached re-read when the caller's context was
// cancelled after the navigation landed.
const tailInfoTimeout = 2 * time.Second

// browser to a private target). Refs from the previous page are invalid
// after navigation.
// Caller holds s.mu.
func (s *profileSession) navigate(ctx context.Context, target string, allowPrivate bool) (string, string, error) {
	runCtx, cancel := bindCall(s.pageCtx, ctx)
	defer cancel()
	_, err := chromedp.Run(runCtx, chromedp.Func(func(c context.Context, t *chromedp.Target) error {
		// Stop any load in flight first, then navigate: rod's Navigate did
		// both and reported the browser's own net error text on failure.
		_, _ = cdp.Call(c, t, cdpage.StopLoading, cdp.Empty{})
		res, err := cdp.Call(c, t, cdpage.Navigate, cdpage.NavigateParams{URL: target})
		if err != nil {
			return err
		}
		if res.ErrorText != "" {
			return errors.New(res.ErrorText)
		}
		return nil
	}))
	if err != nil {
		return "", "", fmt.Errorf("browser: navigate: %w", err)
	}
	s.settle(ctx)
	s.elements = nil // refs from the previous page are invalid after navigation
	info, err := targetInfo(runCtx)
	if err != nil && ctx.Err() != nil {
		// The navigation itself already landed: a cancellation racing the
		// tail read (the caller's turn stopped, the request was torn down)
		// must not turn a successful navigation into a failed tool call —
		// the model would retry a navigation that happened. Re-read on a
		// detached short deadline straight from the page context; if the
		// browser is genuinely gone this errors as before.
		dctx, dcancel := context.WithTimeout(context.WithoutCancel(s.pageCtx), tailInfoTimeout)
		info, err = targetInfo(dctx)
		dcancel()
	}
	if err != nil {
		return "", "", fmt.Errorf("browser: page info: %w", err)
	}
	if err := enforcePostLoadPolicy(ctx, s.pageCtx, info.URL, allowPrivate); err != nil {
		return "", "", err
	}
	return info.URL, info.Title, nil
}
