package auth

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
)

// RequireAuth returns a middleware that enforces CSRF and session verification.
func RequireAuth(store *SessionStore, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// CSRF verification for state-changing requests
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
				origin := r.Header.Get("Origin")
				if origin == "" {
					origin = r.Header.Get("Referer")
				}
				if origin == "" {
					log.Warn("CSRF block: missing Origin and Referer headers on state-changing request")
					httpx.Error(w, http.StatusForbidden, "CSRF verification failed: missing origin/referer")
					return
				}
				u, err := url.Parse(origin)
				if err != nil {
					log.Warn("CSRF block: failed to parse origin URL", "origin", origin, "error", err)
					httpx.Error(w, http.StatusForbidden, "CSRF verification failed")
					return
				}
				originHost := u.Hostname()
				reqHost, _, err := net.SplitHostPort(r.Host)
				if err != nil {
					reqHost = r.Host
				}
				if originHost != reqHost {
					log.Warn("CSRF block: origin hostname does not match request Hostname", "origin_host", originHost, "request_host", reqHost)
					httpx.Error(w, http.StatusForbidden, "CSRF verification failed")
					return
				}
			}

			// Session verification
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "Authentication required")
				return
			}

			valid, _ := store.Verify(cookie.Value)
			if !valid {
				httpx.Error(w, http.StatusUnauthorized, "Session expired or invalid")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
