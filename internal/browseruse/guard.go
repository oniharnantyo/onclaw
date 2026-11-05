package browseruse

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/chromedp/chromedp"
)

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

// enforcePostLoadPolicy re-checks the final URL after redirects and resets
// the page when it landed somewhere the policy forbids.
func enforcePostLoadPolicy(ctx context.Context, page context.Context, finalURL string, allowPrivate bool) error {
	if _, err := validateNavigateURL(ctx, finalURL, allowPrivate); err == nil {
		return nil
	}
	runCtx, cancel := bindCall(page, ctx)
	defer cancel()
	_, _ = chromedp.Run(runCtx, chromedp.Navigate("about:blank")) // reset is best-effort; the refusal is the result
	return fmt.Errorf("browser: page landed on %s which violates the private-network policy; reset to about:blank", finalURL)
}

// isPublicIP reports whether ip is a public unicast address: not loopback,
// private, link-local, unspecified, or multicast.
func isPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast())
}
