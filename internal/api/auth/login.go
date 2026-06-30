package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

type loginRequest struct {
	Password string `json:"password"`
}

// Login returns an HTTP handler for handling user authentication and session creation.
func Login(store *SessionStore, svc *service.Service, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		ok, err := svc.VerifyPassword(r.Context(), req.Password)
		if err != nil {
			log.Error("Failed to verify password", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "Auth system misconfigured")
			return
		}

		if !ok {
			log.Warn("Failed login attempt")
			httpx.Error(w, http.StatusUnauthorized, "Invalid password")
			return
		}

		token, expiry, err := store.Create()
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "Session generation failed")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    token,
			Path:     "/",
			Expires:  expiry,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Secure:   false,
		})

		httpx.JSON(w, http.StatusOK, map[string]string{"status": "logged_in"})
	}
}

// Logout returns an HTTP handler for logging out and destroying the session.
func Logout(store *SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err == nil {
			store.Delete(cookie.Value)
		}

		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})

		httpx.JSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
	}
}
