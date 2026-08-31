package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestErrorToStatus(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
		expectedMsg    string
	}{
		{
			name:           "nil error",
			err:            nil,
			expectedStatus: http.StatusOK,
			expectedCode:   "",
			expectedMsg:    "",
		},
		{
			name:           "ErrInvalid",
			err:            domain.ErrInvalid,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   CodeInvalidRequest,
			expectedMsg:    "invalid request",
		},
		{
			name:           "ErrUnauthenticated",
			err:            domain.ErrUnauthenticated,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   CodeUnauthenticated,
			expectedMsg:    "unauthenticated",
		},
		{
			name:           "ErrForbidden",
			err:            domain.ErrForbidden,
			expectedStatus: http.StatusForbidden,
			expectedCode:   CodeForbidden,
			expectedMsg:    "forbidden",
		},
		{
			name:           "ErrNotFound",
			err:            domain.ErrNotFound,
			expectedStatus: http.StatusNotFound,
			expectedCode:   CodeNotFound,
			expectedMsg:    "not found",
		},
		{
			name:           "ErrConflict",
			err:            domain.ErrConflict,
			expectedStatus: http.StatusConflict,
			expectedCode:   CodeConflict,
			expectedMsg:    "conflict",
		},
		{
			name:           "ErrLastOwnerProtected",
			err:            domain.ErrLastOwnerProtected,
			expectedStatus: http.StatusConflict,
			expectedCode:   CodeLastOwnerProtected,
			expectedMsg:    "cannot demote or remove the last owner",
		},
		{
			name:           "ErrPayloadTooLarge",
			err:            domain.ErrPayloadTooLarge,
			expectedStatus: http.StatusRequestEntityTooLarge,
			expectedCode:   CodePayloadTooLarge,
			expectedMsg:    "payload too large",
		},
		{
			name:           "ErrUndecryptable",
			err:            domain.ErrUndecryptable,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   CodeUndecryptable,
			expectedMsg:    "undecryptable",
		},
		{
			name:           "unknown/internal error",
			err:            errors.New("db connection failure"),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   CodeInternal,
			expectedMsg:    "internal server error",
		},
		{
			name: "custom APIError",
			err: &APIError{
				Code:    CodeForbidden,
				Message: "custom forbidden message",
			},
			expectedStatus: http.StatusForbidden,
			expectedCode:   CodeForbidden,
			expectedMsg:    "custom forbidden message",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, code, msg := ErrorToStatus(tc.err)
			if status != tc.expectedStatus {
				t.Errorf("status = %d, want %d", status, tc.expectedStatus)
			}
			if code != tc.expectedCode {
				t.Errorf("code = %q, want %q", code, tc.expectedCode)
			}
			if msg != tc.expectedMsg {
				t.Errorf("msg = %q, want %q", msg, tc.expectedMsg)
			}
		})
	}
}

func TestAPIErrorJSON(t *testing.T) {
	t.Run("serializes request_id when present", func(t *testing.T) {
		env := NewErrorEnvelope(CodeNotFound, "resource not found", "req-12345-abc", ErrorDetail{Field: "id", Message: "must be valid"})
		data, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("failed to marshal ErrorEnvelope: %v", err)
		}

		var raw map[string]map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		errObj := raw["error"]
		if errObj["code"] != CodeNotFound {
			t.Errorf("expected code %q, got %v", CodeNotFound, errObj["code"])
		}
		if errObj["message"] != "resource not found" {
			t.Errorf("expected message %q, got %v", "resource not found", errObj["message"])
		}
		if errObj["request_id"] != "req-12345-abc" {
			t.Errorf("expected request_id %q, got %v", "req-12345-abc", errObj["request_id"])
		}
	})

	t.Run("omits request_id when empty", func(t *testing.T) {
		env := NewErrorEnvelope(CodeInvalidRequest, "invalid", "")
		data, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("failed to marshal ErrorEnvelope: %v", err)
		}

		var raw map[string]map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		errObj := raw["error"]
		if _, exists := errObj["request_id"]; exists {
			t.Errorf("expected request_id to be omitted when empty, got %v", errObj["request_id"])
		}
	})
}

func TestRespondError(t *testing.T) {
	t.Run("sets code, message and request_id from context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(RequestIDContextKey, "ctx-req-id-999")

		RespondError(c, domain.ErrInvalid)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", w.Code)
		}

		var env ErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if env.Error.Code != CodeInvalidRequest {
			t.Errorf("expected code %q, got %q", CodeInvalidRequest, env.Error.Code)
		}
		if env.Error.Message != "invalid request" {
			t.Errorf("expected message %q, got %q", "invalid request", env.Error.Message)
		}
		if env.Error.RequestID != "ctx-req-id-999" {
			t.Errorf("expected request_id %q, got %q", "ctx-req-id-999", env.Error.RequestID)
		}
	})
}

func TestAbortHelpers(t *testing.T) {
	t.Run("AbortNotFound", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(RequestIDContextKey, "req-404-test")
		AbortNotFound(c, "workspace not found")

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", w.Code)
		}
		var env ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if env.Error.Code != CodeNotFound || env.Error.Message != "workspace not found" {
			t.Errorf("unexpected body: %+v", env)
		}
		if env.Error.RequestID != "req-404-test" {
			t.Errorf("expected request_id %q, got %q", "req-404-test", env.Error.RequestID)
		}
	})

	t.Run("AbortUnauthenticated", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(RequestIDContextKey, "req-401-test")
		AbortUnauthenticated(c, "")

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", w.Code)
		}
		var env ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if env.Error.Code != CodeUnauthenticated || env.Error.Message != "unauthenticated" {
			t.Errorf("unexpected body: %+v", env)
		}
		if env.Error.RequestID != "req-401-test" {
			t.Errorf("expected request_id %q, got %q", "req-401-test", env.Error.RequestID)
		}
	})

	t.Run("AbortForbidden", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(RequestIDContextKey, "req-403-test")
		AbortForbidden(c, "")

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d", w.Code)
		}
		var env ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if env.Error.Code != CodeForbidden || env.Error.Message != "forbidden" {
			t.Errorf("unexpected body: %+v", env)
		}
		if env.Error.RequestID != "req-403-test" {
			t.Errorf("expected request_id %q, got %q", "req-403-test", env.Error.RequestID)
		}
	})
}

func TestErrorLoggingRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	oldLogger := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldLogger)

	t.Run("RespondError logs 500 error with request_id", func(t *testing.T) {
		buf.Reset()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(RequestIDContextKey, "req-slog-500-1")

		RespondError(c, errors.New("database connection failure"))

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", w.Code)
		}

		logOutput := buf.String()
		if !strings.Contains(logOutput, `"request_id":"req-slog-500-1"`) {
			t.Errorf("expected slog output to contain request_id, got: %s", logOutput)
		}
		if !strings.Contains(logOutput, "internal server error") {
			t.Errorf("expected slog output to contain 'internal server error', got: %s", logOutput)
		}
	})

	t.Run("AbortWithError logs 500 error with request_id", func(t *testing.T) {
		buf.Reset()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(RequestIDContextKey, "req-slog-500-2")

		AbortWithError(c, http.StatusInternalServerError, CodeInternal, "custom internal failure")

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", w.Code)
		}

		logOutput := buf.String()
		if !strings.Contains(logOutput, `"request_id":"req-slog-500-2"`) {
			t.Errorf("expected slog output to contain request_id, got: %s", logOutput)
		}
	})
}
