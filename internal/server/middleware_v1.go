package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// v1Middlewares holds dependencies for /v1 (OpenResponses) middleware.
type v1Middlewares struct {
	apiKeys store.WorkspaceAPIKeyStore
}

// NewV1Middlewares creates a new v1Middlewares instance with the given granular store.
func NewV1Middlewares(apiKeys store.WorkspaceAPIKeyStore) *v1Middlewares {
	return &v1Middlewares{apiKeys: apiKeys}
}

// APIKeyAuthRequired authenticates the request with a workspace API key:
// `Authorization: Bearer oc_ws_...` → SHA-256 → LookupByHash. On success the
// key (carrying the workspace tenant scope and creator user) is bound to the
// context. Session JWTs do not authenticate this surface; every failure is a
// 401 OpenResponses envelope with code invalid_api_key, indistinguishable
// between unknown, malformed, and revoked keys.
func (m *v1Middlewares) APIKeyAuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			abortV1InvalidAPIKey(c)
			return
		}
		plaintext := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		if !strings.HasPrefix(plaintext, services.APIKeyPrefix) {
			abortV1InvalidAPIKey(c)
			return
		}

		key, err := m.apiKeys.LookupByHash(c.Request.Context(), domain.HashAPIKey(plaintext))
		if err != nil || key.IsRevoked() {
			abortV1InvalidAPIKey(c)
			return
		}

		c.Set(handlers.APIKeyContextKey, key)
		c.Next()
	}
}

// abortV1InvalidAPIKey responds with the auth-failure envelope. All failures
// share one message so workspace/key existence is not leaked.
func abortV1InvalidAPIKey(c *gin.Context) {
	handlers.RespondV1Error(c, http.StatusUnauthorized, handlers.V1ErrorTypeInvalidRequest, "", handlers.CodeInvalidAPIKey, "invalid API key")
}
