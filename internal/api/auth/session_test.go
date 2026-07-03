package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/api/auth"
	"github.com/oniharnantyo/onclaw/internal/api/service"
	"golang.org/x/crypto/bcrypt"
)

type mockKVStore struct {
	values map[string]string
	getErr error
}

func (m *mockKVStore) Get(ctx context.Context, key string) (string, error) {
	if m.getErr != nil {
		return "", m.getErr
	}
	v, ok := m.values[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (m *mockKVStore) Set(ctx context.Context, key, val string) error {
	m.values[key] = val
	return nil
}

func (m *mockKVStore) Delete(ctx context.Context, key string) error {
	delete(m.values, key)
	return nil
}

func TestSessionStore(t *testing.T) {
	store := auth.NewSessionStore()
	token, expiry, err := store.Create()
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
	if expiry.Before(time.Now()) {
		t.Error("expected expiry in the future")
	}

	valid, expired := store.Verify(token)
	if !valid || expired {
		t.Errorf("expected session to be valid and not expired, got valid=%v, expired=%v", valid, expired)
	}

	store.Delete(token)
	valid, expired = store.Verify(token)
	if valid || expired {
		t.Errorf("expected deleted session to be invalid, got valid=%v, expired=%v", valid, expired)
	}
}

func TestLoginAndLogout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := auth.NewSessionStore()

	// Hash password
	password := "my-secret-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	kv := &mockKVStore{values: map[string]string{"web_password_hash": string(hash)}}
	svc := service.New(nil, kv, nil, nil, nil, logger, nil, nil, nil, nil, nil, nil, nil)

	handler := auth.Login(store, svc, logger)

	// 1. Invalid payload
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", strings.NewReader("bad-json"))
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected BadRequest, got %d", w.Code)
	}

	// 2. Correct password
	w = httptest.NewRecorder()
	payload, _ := json.Marshal(map[string]string{"password": password})
	req = httptest.NewRequest("POST", "/login", bytes.NewReader(payload))
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected StatusOK, got %d", w.Code)
	}
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}

	// 3. Incorrect password
	w = httptest.NewRecorder()
	payload, _ = json.Marshal(map[string]string{"password": "wrong"})
	req = httptest.NewRequest("POST", "/login", bytes.NewReader(payload))
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected StatusUnauthorized, got %d", w.Code)
	}

	// 4. DB error during verification
	kv.getErr = errors.New("db error")
	w = httptest.NewRecorder()
	payload, _ = json.Marshal(map[string]string{"password": password})
	req = httptest.NewRequest("POST", "/login", bytes.NewReader(payload))
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected StatusInternalServerError, got %d", w.Code)
	}
	kv.getErr = nil

	// 5. Logout
	logoutHandler := auth.Logout(store)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(sessionCookie)
	logoutHandler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected StatusOK, got %d", w.Code)
	}
	// Verify session is deleted
	valid, _ := store.Verify(sessionCookie.Value)
	if valid {
		t.Error("expected session to be invalid after logout")
	}
}

func TestRequireAuthMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := auth.NewSessionStore()
	token, _, _ := store.Create()

	middleware := auth.RequireAuth(store, logger)
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	protected := middleware(nextHandler)

	// 1. Missing cookie
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/protected", nil)
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected StatusUnauthorized, got %d", w.Code)
	}

	// 2. Invalid cookie
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "invalid-token"})
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected StatusUnauthorized, got %d", w.Code)
	}

	// 3. Valid session GET request
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected StatusOK, got %d", w.Code)
	}

	// 4. Valid session POST request - CSRF Block (missing Origin/Referer)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected StatusForbidden due to CSRF, got %d", w.Code)
	}

	// 5. Valid session POST request - CSRF Pass (valid Origin/Referer matching Host)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/protected", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected StatusOK, got %d", w.Code)
	}

	// 6. Valid session POST request - CSRF Block (mismatched Origin)
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/protected", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://evil.com")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	protected.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected StatusForbidden, got %d", w.Code)
	}
}
