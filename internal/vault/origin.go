package vault

import (
	"net"
	"net/url"
	"strings"
)

// OriginMatches reports whether a login stored under pattern may be used
// on the page origin (add-browser-login-handover design D5, ported from
// OpenBot's websiteMatchesOrigin): both sides reduce to hosts, only
// HTTPS origins qualify — a side carrying an explicit http scheme never
// matches — and a match is the exact host or the same registrable domain,
// approximated as the last two labels: "example.com" matches every
// subdomain, "accounts.example.com" matches "login.accounts.example.com".
// IP hosts have no domain structure and must match exactly; ports are
// ignored; empty or malformed input never matches.
func OriginMatches(pattern, origin string) bool {
	patternHost, ok := originHost(pattern)
	if !ok {
		return false
	}
	pageHost, ok := originHost(origin)
	if !ok {
		return false
	}
	if net.ParseIP(patternHost) != nil || net.ParseIP(pageHost) != nil {
		return patternHost == pageHost
	}
	return registrableDomain(patternHost) == registrableDomain(pageHost)
}

// NormalizeOrigin reduces an origin to its canonical stored form: the
// lowercased scheme and host (port kept), any path, query, and trailing
// slash dropped. Input that does not parse as a URL with a host is
// lowercased verbatim.
func NormalizeOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return strings.ToLower(origin)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// originHost reduces one side of the comparison to its lowercase host,
// enforcing the HTTPS-only rule: an explicit non-https scheme never
// matches, a bare host ("example.com") is scheme-less and therefore
// https-eligible. ok is false for empty or malformed input.
func originHost(side string) (string, bool) {
	side = strings.TrimSpace(side)
	if side == "" {
		return "", false
	}
	u, err := url.Parse(side)
	if err != nil || u.Host == "" {
		// No host parsed: read the input as a bare host (possibly with a
		// path or port attached).
		u, err = url.Parse("https://" + side)
		if err != nil {
			return "", false
		}
	}
	if u.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	return host, true
}

// registrableDomain approximates the eTLD+1 as the last two dot-separated
// labels ("a.b.example.com" → "example.com"); single- and two-label hosts
// return unchanged. Deliberately without the public-suffix list, matching
// OpenBot's simple semantics.
func registrableDomain(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) <= 2 {
		return host
	}
	return labels[len(labels)-2] + "." + labels[len(labels)-1]
}
