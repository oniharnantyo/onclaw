package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// OpenResponses (/v1) error types per the wire contract.
const (
	V1ErrorTypeInvalidRequest = "invalid_request_error"
	V1ErrorTypeNotFound       = "not_found_error"
	V1ErrorTypeRateLimit      = "rate_limit_error"
	V1ErrorTypeModel          = "model_error"
	V1ErrorTypeServer         = "server_error"
)

// V1Error is the error object of the OpenResponses envelope:
// {error: {message, type, param, code}}.
type V1Error struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    *string `json:"code"`
}

// V1ErrorEnvelope is the standard JSON envelope for all /v1 error responses.
type V1ErrorEnvelope struct {
	Error V1Error `json:"error"`
}

// NewV1ErrorEnvelope builds an OpenResponses error envelope.
func NewV1ErrorEnvelope(errType, param, code, message string) V1ErrorEnvelope {
	v1Err := V1Error{Message: message, Type: errType}
	if param != "" {
		v1Err.Param = &param
	}
	if code != "" {
		v1Err.Code = &code
	}
	return V1ErrorEnvelope{Error: v1Err}
}

// RespondV1Error writes an OpenResponses error envelope with the given status
// and aborts the handler chain.
func RespondV1Error(c *gin.Context, status int, errType, param, code, message string) {
	if message == "" {
		message = http.StatusText(status)
	}
	c.AbortWithStatusJSON(status, NewV1ErrorEnvelope(errType, param, code, message))
}

// RespondV1InvalidRequest writes a 400 invalid_request_error naming the
// offending parameter.
func RespondV1InvalidRequest(c *gin.Context, param, message string) {
	RespondV1Error(c, http.StatusBadRequest, V1ErrorTypeInvalidRequest, param, "", message)
}

// RespondV1NotFound writes a 404 not_found_error. Messages must not leak
// workspace existence: callers pass generic wording ("not found").
func RespondV1NotFound(c *gin.Context, param, message string) {
	RespondV1Error(c, http.StatusNotFound, V1ErrorTypeNotFound, param, "", message)
}

// RespondV1ModelError writes a 500 model_error for upstream model failures.
func RespondV1ModelError(c *gin.Context, message string) {
	RespondV1Error(c, http.StatusInternalServerError, V1ErrorTypeModel, "", "", message)
}

// RespondV1ModelErrorBody renders a model_error envelope for mid-stream
// response.failed events (no HTTP status involved).
func RespondV1ModelErrorBody(message string) V1ErrorEnvelope {
	return NewV1ErrorEnvelope(V1ErrorTypeModel, "", "", message)
}

// RespondV1ServerError writes a 500 server_error.
func RespondV1ServerError(c *gin.Context, message string) {
	RespondV1Error(c, http.StatusInternalServerError, V1ErrorTypeServer, "", "", message)
}

// V1ErrorToStatus maps a domain error to the OpenResponses status and error
// type per design D6: ErrInvalid → 400 invalid_request_error,
// ErrNotFound → 404 not_found_error, conflict → 409 invalid_request_error,
// authentication failures → 401 invalid_request_error (invalid_api_key code).
func V1ErrorToStatus(err error) (int, string, string, string) {
	if err == nil {
		return http.StatusOK, "", "", ""
	}
	switch {
	case errors.Is(err, domain.ErrInvalid):
		return http.StatusBadRequest, V1ErrorTypeInvalidRequest, "", ""
	case errors.Is(err, domain.ErrUnauthenticated):
		return http.StatusUnauthorized, V1ErrorTypeInvalidRequest, "", CodeInvalidAPIKey
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, V1ErrorTypeInvalidRequest, "", ""
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, V1ErrorTypeNotFound, "", ""
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrLastOwnerProtected):
		return http.StatusConflict, V1ErrorTypeInvalidRequest, "", CodeConflict
	case errors.Is(err, domain.ErrPayloadTooLarge):
		return http.StatusBadRequest, V1ErrorTypeInvalidRequest, "", ""
	default:
		return http.StatusInternalServerError, V1ErrorTypeServer, "", ""
	}
}

// RespondV1DomainError maps a domain error onto the OpenResponses envelope.
func RespondV1DomainError(c *gin.Context, err error) {
	status, errType, param, code := V1ErrorToStatus(err)
	RespondV1Error(c, status, errType, param, code, err.Error())
}

const (
	// APIKeyContextKey is the gin context key for the authenticated workspace
	// API key (*domain.WorkspaceAPIKey).
	APIKeyContextKey = "current_api_key"

	// CodeInvalidAPIKey is the OpenResponses error code for all API key
	// authentication failures. Auth failures never distinguish unknown,
	// malformed, and revoked keys.
	CodeInvalidAPIKey = "invalid_api_key"
)

// CurrentAPIKey retrieves the authenticated *domain.WorkspaceAPIKey from the Gin context.
func CurrentAPIKey(c *gin.Context) (*domain.WorkspaceAPIKey, bool) {
	val, exists := c.Get(APIKeyContextKey)
	if !exists {
		return nil, false
	}
	k, ok := val.(*domain.WorkspaceAPIKey)
	return k, ok && k != nil
}

// MustCurrentAPIKey retrieves the authenticated *domain.WorkspaceAPIKey from
// the Gin context or panics.
func MustCurrentAPIKey(c *gin.Context) *domain.WorkspaceAPIKey {
	k, ok := CurrentAPIKey(c)
	if !ok {
		panic("current api key not found in context")
	}
	return k
}
