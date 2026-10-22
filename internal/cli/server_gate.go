package cli

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BearerGate returns middleware enforcing an optional bearer token (design
// D7, spec: optional server bearer token). When token is empty the returned
// middleware is a passthrough: the local single-user assumption means no
// authentication is required. When a token is configured, every request —
// plain HTTP and WebSocket upgrades alike — must present it.
//
// Token delivery story (one mechanism covers HTTP and both WebSocket
// endpoints /wails/events and /wails/stream/ws, which sit in front of the
// Wails handler):
//
//   - "Authorization: Bearer <token>" header — for non-browser clients and
//     for the browser's normal fetch/HTTP traffic;
//   - a single "?token=<token>" query parameter fallback — because browsers
//     cannot set custom headers on `new WebSocket(...)`. The parameter is
//     stripped from the request before it is forwarded downstream.
//
// Anything else is rejected with 401 Unauthorized.
func BearerGate(token string) func(http.Handler) http.Handler {
	if token == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented, viaQuery := requestToken(r)
			if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="buddies"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if viaQuery {
				// Keep the token out of downstream logs and redirects.
				r2 := r.Clone(r.Context())
				q := r2.URL.Query()
				q.Del("token")
				r2.URL.RawQuery = q.Encode()
				r = r2
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestToken extracts the presented token: the Authorization bearer header
// first, the ?token= query parameter second. The boolean reports that the
// token came from the query string.
func requestToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:]), false
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t, true
	}
	return "", false
}
