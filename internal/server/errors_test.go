package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestRespondError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

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
}

func TestAbortHelpers(t *testing.T) {
	t.Run("AbortNotFound", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		AbortNotFound(c, "workspace not found")

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", w.Code)
		}
		var env ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if env.Error.Code != CodeNotFound || env.Error.Message != "workspace not found" {
			t.Errorf("unexpected body: %+v", env)
		}
	})

	t.Run("AbortUnauthenticated", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		AbortUnauthenticated(c, "")

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", w.Code)
		}
		var env ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if env.Error.Code != CodeUnauthenticated || env.Error.Message != "unauthenticated" {
			t.Errorf("unexpected body: %+v", env)
		}
	})

	t.Run("AbortForbidden", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		AbortForbidden(c, "")

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d", w.Code)
		}
		var env ErrorEnvelope
		_ = json.Unmarshal(w.Body.Bytes(), &env)
		if env.Error.Code != CodeForbidden || env.Error.Message != "forbidden" {
			t.Errorf("unexpected body: %+v", env)
		}
	})
}
