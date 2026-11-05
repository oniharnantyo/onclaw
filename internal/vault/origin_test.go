package vault

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOriginMatches ports OpenBot's websiteMatchesOrigin semantics as a
// table: HTTPS-only origins, exact host or same registrable domain (last
// two labels), IP hosts exact-match, ports ignored, empty never matches.
func TestOriginMatches(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		origin  string
		want    bool
	}{
		{name: "exact host", pattern: "https://example.com", origin: "https://example.com/login", want: true},
		{name: "exact host with port ignored", pattern: "https://example.com:8443", origin: "https://example.com", want: true},
		{name: "subdomain of pattern", pattern: "https://example.com", origin: "https://app.example.com", want: true},
		{name: "deep subdomain shares registrable domain", pattern: "https://accounts.example.com", origin: "https://login.accounts.example.com/auth", want: true},
		{name: "bare host pattern", pattern: "example.com", origin: "https://www.example.com/x?q=1", want: true},
		{name: "case-insensitive host", pattern: "https://Example.com", origin: "https://EXAMPLE.com", want: true},
		{name: "cross domain", pattern: "https://example.com", origin: "https://evil.com", want: false},
		{name: "lookalike domain", pattern: "https://example.com", origin: "https://notexample.com", want: false},
		{name: "sibling subdomains of different sites", pattern: "https://accounts.example.com", origin: "https://evil-other.example.net", want: false},
		{name: "http origin never matches", pattern: "https://example.com", origin: "http://example.com", want: false},
		{name: "http pattern never matches", pattern: "http://example.com", origin: "https://example.com", want: false},
		{name: "empty pattern", pattern: "", origin: "https://example.com", want: false},
		{name: "empty origin", pattern: "https://example.com", origin: "", want: false},
		{name: "malformed origin", pattern: "https://example.com", origin: "://nope", want: false},
		{name: "ip host exact match", pattern: "https://127.0.0.1:3000", origin: "https://127.0.0.1:8080/app", want: true},
		{name: "ip never matches by label suffix", pattern: "https://1.2.3.4", origin: "https://5.3.4", want: false},
		{name: "ip vs domain", pattern: "https://1.2.3.4", origin: "https://example.com", want: false},
		{name: "different tld", pattern: "https://example.com", origin: "https://example.org", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, OriginMatches(tt.pattern, tt.origin))
		})
	}
}

// TestNormalizeOrigin pins the canonical stored form: lowercased scheme
// and host, path/query/trailing slash dropped, bare input lowercased.
func TestNormalizeOrigin(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://example.com", want: "https://example.com"},
		{in: "https://example.com/", want: "https://example.com"},
		{in: "HTTPS://Example.COM/login?next=/x", want: "https://example.com"},
		{in: "https://example.com:8443/path", want: "https://example.com:8443"},
		{in: "  https://example.com  ", want: "https://example.com"},
		{in: "example.com/path", want: "example.com/path"},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, NormalizeOrigin(tt.in))
	}
}
