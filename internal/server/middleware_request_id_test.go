package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

var hex16Regex = regexp.MustCompile(`^[0-9a-f]{16}$`)

func TestRequestIDMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("header present on 200 OK and success body unchanged", func(t *testing.T) {
		r := gin.New()
		r.Use(RequestIDMiddleware())
		r.GET("/test-ok", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok", "count": 42})
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test-ok", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		reqID := w.Header().Get(HeaderRequestID)
		if reqID == "" {
			t.Fatal("expected X-Request-ID response header to be set")
		}
		if !hex16Regex.MatchString(reqID) {
			t.Fatalf("expected 16-char hex request id, got %q (len: %d)", reqID, len(reqID))
		}

		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if _, exists := body["request_id"]; exists {
			t.Errorf("expected success response body not to contain request_id, got %v", body["request_id"])
		}
		if body["status"] != "ok" {
			t.Errorf("expected status 'ok', got %v", body["status"])
		}
	})

	t.Run("header and envelope parity on 500 error", func(t *testing.T) {
		r := gin.New()
		r.Use(RequestIDMiddleware())
		r.GET("/test-500", func(c *gin.Context) {
			RespondError(c, errors.New("something went wrong internally"))
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test-500", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}

		headerID := w.Header().Get(HeaderRequestID)
		if headerID == "" {
			t.Fatal("expected X-Request-ID response header to be set")
		}
		if !hex16Regex.MatchString(headerID) {
			t.Fatalf("expected 16-char hex request id, got %q", headerID)
		}

		var env ErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("failed to decode error envelope: %v", err)
		}

		if env.Error.Code != CodeInternal {
			t.Errorf("expected code %q, got %q", CodeInternal, env.Error.Code)
		}
		if env.Error.RequestID == "" {
			t.Fatal("expected non-empty request_id in error envelope")
		}
		if env.Error.RequestID != headerID {
			t.Fatalf("expected envelope request_id %q to equal header %q", env.Error.RequestID, headerID)
		}
	})

	t.Run("header and envelope parity on 4xx errors", func(t *testing.T) {
		r := gin.New()
		r.Use(RequestIDMiddleware())
		r.GET("/400", func(c *gin.Context) {
			RespondError(c, domain.ErrInvalid)
		})
		r.GET("/401", func(c *gin.Context) {
			AbortUnauthenticated(c, "")
		})
		r.GET("/403", func(c *gin.Context) {
			AbortForbidden(c, "")
		})
		r.GET("/404", func(c *gin.Context) {
			AbortNotFound(c, "")
		})
		r.GET("/409", func(c *gin.Context) {
			RespondError(c, domain.ErrConflict)
		})

		endpoints := []struct {
			path           string
			expectedStatus int
			expectedCode   string
		}{
			{"/400", http.StatusBadRequest, CodeInvalidRequest},
			{"/401", http.StatusUnauthorized, CodeUnauthenticated},
			{"/403", http.StatusForbidden, CodeForbidden},
			{"/404", http.StatusNotFound, CodeNotFound},
			{"/409", http.StatusConflict, CodeConflict},
		}

		for _, ep := range endpoints {
			t.Run(ep.path, func(t *testing.T) {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, ep.path, nil)
				r.ServeHTTP(w, req)

				if w.Code != ep.expectedStatus {
					t.Fatalf("expected status %d, got %d", ep.expectedStatus, w.Code)
				}

				headerID := w.Header().Get(HeaderRequestID)
				if headerID == "" {
					t.Fatal("expected X-Request-ID response header")
				}

				var env ErrorEnvelope
				if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
					t.Fatalf("failed to decode error envelope: %v", err)
				}

				if env.Error.Code != ep.expectedCode {
					t.Errorf("expected code %q, got %q", ep.expectedCode, env.Error.Code)
				}
				if env.Error.RequestID != headerID {
					t.Errorf("expected envelope request_id %q to match header %q", env.Error.RequestID, headerID)
				}
			})
		}
	})

	t.Run("inbound valid X-Request-ID is echoed and preserved", func(t *testing.T) {
		validIDs := []string{
			"custom-trace-id-1234",
			"a",
			"a-b-c-1-2-3",
			"REQUEST-ID-UPPERCASE",
			"1234567890123456789012345678901234567890123456789012345678901234", // 64 chars
		}

		r := gin.New()
		r.Use(RequestIDMiddleware())
		r.GET("/echo", func(c *gin.Context) {
			id := CurrentRequestID(c)
			c.JSON(http.StatusOK, gin.H{"id_in_context": id})
		})
		r.GET("/echo-err", func(c *gin.Context) {
			RespondError(c, domain.ErrNotFound)
		})

		for _, inID := range validIDs {
			t.Run("valid id: "+inID, func(t *testing.T) {
				// 200 OK check
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/echo", nil)
				req.Header.Set(HeaderRequestID, inID)
				r.ServeHTTP(w, req)

				if w.Header().Get(HeaderRequestID) != inID {
					t.Errorf("expected header %q, got %q", inID, w.Header().Get(HeaderRequestID))
				}

				var body map[string]string
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if body["id_in_context"] != inID {
					t.Errorf("expected context id %q, got %q", inID, body["id_in_context"])
				}

				// Error envelope check
				wErr := httptest.NewRecorder()
				reqErr := httptest.NewRequest(http.MethodGet, "/echo-err", nil)
				reqErr.Header.Set(HeaderRequestID, inID)
				r.ServeHTTP(wErr, reqErr)

				var env ErrorEnvelope
				_ = json.Unmarshal(wErr.Body.Bytes(), &env)
				if env.Error.RequestID != inID {
					t.Errorf("expected envelope request_id %q, got %q", inID, env.Error.RequestID)
				}
			})
		}
	})

	t.Run("inbound invalid X-Request-ID is rejected and replaced by 16-hex id", func(t *testing.T) {
		invalidIDs := []struct {
			name string
			val  string
		}{
			{"empty", ""},
			{"65 chars", strings.Repeat("a", 65)},
			{"with underscore", "req_id_with_underscore"},
			{"with space", "req id with space"},
			{"with dots", "req.id.with.dots"},
			{"with special characters", "req!@#$%^&*()"},
			{"with html tag", "<script>alert(1)</script>"},
			{"with newline", "req-id\nnewline"},
		}

		r := gin.New()
		r.Use(RequestIDMiddleware())
		r.GET("/echo", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		for _, tc := range invalidIDs {
			t.Run(tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/echo", nil)
				if tc.val != "" {
					req.Header.Set(HeaderRequestID, tc.val)
				}
				r.ServeHTTP(w, req)

				gotHeader := w.Header().Get(HeaderRequestID)
				if gotHeader == tc.val && tc.val != "" {
					t.Errorf("expected invalid id %q to be rejected, but was echoed", tc.val)
				}
				if !hex16Regex.MatchString(gotHeader) {
					t.Errorf("expected 16-char hex generated id, got %q", gotHeader)
				}
			})
		}
	})

	t.Run("distinct requests receive distinct generated IDs", func(t *testing.T) {
		r := gin.New()
		r.Use(RequestIDMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		ids := make(map[string]bool)
		for i := 0; i < 50; i++ {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			r.ServeHTTP(w, req)

			id := w.Header().Get(HeaderRequestID)
			if !hex16Regex.MatchString(id) {
				t.Fatalf("expected 16-char hex id, got %q", id)
			}
			if ids[id] {
				t.Fatalf("duplicate request id generated: %s", id)
			}
			ids[id] = true
		}
	})

	t.Run("panic recovery preserves X-Request-ID response header", func(t *testing.T) {
		r := gin.New()
		r.Use(RequestIDMiddleware())
		r.Use(gin.Recovery())
		r.GET("/panic", func(c *gin.Context) {
			panic("unexpected crash in handler")
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 from recovery, got %d", w.Code)
		}
		headerID := w.Header().Get(HeaderRequestID)
		if headerID == "" {
			t.Fatal("expected X-Request-ID header to be retained after panic recovery")
		}
		if !hex16Regex.MatchString(headerID) {
			t.Errorf("expected 16-char hex request id, got %q", headerID)
		}
	})
}
