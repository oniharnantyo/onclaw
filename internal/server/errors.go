package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Standard API error codes.
const (
	CodeInvalidRequest     = "invalid_request"
	CodeUnauthenticated    = "unauthenticated"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodeLastOwnerProtected = "last_owner_protected"
	CodePayloadTooLarge    = "payload_too_large"
	CodeUndecryptable      = "undecryptable"
	CodeInternal           = "internal"
)

// ErrorDetail represents a field-level error detail.
type ErrorDetail struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message,omitempty"`
}

// APIError represents the error object in the API response.
type APIError struct {
	Code      string        `json:"code"`
	Message   string        `json:"message"`
	RequestID string        `json:"request_id,omitempty"`
	Details   []ErrorDetail `json:"details,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}

// ErrorEnvelope is the standard JSON envelope for all API error responses.
type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

// NewErrorEnvelope creates a new ErrorEnvelope.
func NewErrorEnvelope(code, message, requestID string, details ...ErrorDetail) ErrorEnvelope {
	return ErrorEnvelope{
		Error: APIError{
			Code:      code,
			Message:   message,
			RequestID: requestID,
			Details:   details,
		},
	}
}

// ErrorToStatus maps domain sentinel errors to HTTP status code, API error code, and client-safe message.
func ErrorToStatus(err error) (int, string, string) {
	if err == nil {
		return http.StatusOK, "", ""
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		status := CodeToStatus(apiErr.Code)
		return status, apiErr.Code, apiErr.Message
	}

	switch {
	case errors.Is(err, domain.ErrLastOwnerProtected):
		return http.StatusConflict, CodeLastOwnerProtected, err.Error()
	case errors.Is(err, domain.ErrUndecryptable):
		return http.StatusBadRequest, CodeUndecryptable, err.Error()
	case errors.Is(err, domain.ErrInvalid):
		return http.StatusBadRequest, CodeInvalidRequest, err.Error()
	case errors.Is(err, domain.ErrUnauthenticated):
		return http.StatusUnauthorized, CodeUnauthenticated, err.Error()
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, CodeForbidden, err.Error()
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, CodeNotFound, err.Error()
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, CodeConflict, err.Error()
	case errors.Is(err, domain.ErrPayloadTooLarge):
		return http.StatusRequestEntityTooLarge, CodePayloadTooLarge, err.Error()
	default:
		return http.StatusInternalServerError, CodeInternal, "internal server error"
	}
}

// CodeToStatus maps a string error code to an HTTP status code.
func CodeToStatus(code string) int {
	switch code {
	case CodeInvalidRequest, CodeUndecryptable:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict, CodeLastOwnerProtected:
		return http.StatusConflict
	case CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}

// RespondError maps an error to the standard error envelope and writes the response.
func RespondError(c *gin.Context, err error) {
	status, code, message := ErrorToStatus(err)
	var details []ErrorDetail
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		details = apiErr.Details
	}
	reqID := CurrentRequestID(c)
	if reqID == "" && apiErr != nil && apiErr.RequestID != "" {
		reqID = apiErr.RequestID
	}
	if status >= 500 {
		slog.Error("internal server error", "error", err, "request_id", reqID)
	}
	c.AbortWithStatusJSON(status, NewErrorEnvelope(code, message, reqID, details...))
}

// AbortWithError sends a specific error envelope and aborts the Gin handler chain.
func AbortWithError(c *gin.Context, status int, code, message string, details ...ErrorDetail) {
	reqID := CurrentRequestID(c)
	if status >= 500 {
		slog.Error("internal server error", "code", code, "message", message, "request_id", reqID)
	}
	c.AbortWithStatusJSON(status, NewErrorEnvelope(code, message, reqID, details...))
}

// AbortNotFound responds with a standard 404 not_found error.
func AbortNotFound(c *gin.Context, message string) {
	if message == "" {
		message = "not found"
	}
	AbortWithError(c, http.StatusNotFound, CodeNotFound, message)
}

// AbortUnauthenticated responds with a standard 401 unauthenticated error.
func AbortUnauthenticated(c *gin.Context, message string) {
	if message == "" {
		message = "unauthenticated"
	}
	AbortWithError(c, http.StatusUnauthorized, CodeUnauthenticated, message)
}

// AbortForbidden responds with a standard 403 forbidden error.
func AbortForbidden(c *gin.Context, message string) {
	if message == "" {
		message = "forbidden"
	}
	AbortWithError(c, http.StatusForbidden, CodeForbidden, message)
}
