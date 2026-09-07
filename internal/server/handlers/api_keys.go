package handlers

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
)

// APIKeyResponse is the display representation of a workspace API key.
// The plaintext key and its stored hash never appear in responses.
type APIKeyResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	KeyPrefix string     `json:"key_prefix"`
	KeySuffix string     `json:"key_suffix"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func toAPIKeyResponse(k *domain.WorkspaceAPIKey) APIKeyResponse {
	return APIKeyResponse{
		ID:        k.ID,
		Name:      k.Name,
		KeyPrefix: k.KeyPrefix,
		KeySuffix: k.KeySuffix,
		CreatedBy: k.CreatedBy,
		CreatedAt: k.CreatedAt,
		RevokedAt: k.RevokedAt,
	}
}

// apiKeysHandlers handles workspace API key management endpoints.
type apiKeysHandlers struct {
	keys *services.APIKeyService
}

// NewAPIKeysHandlers creates a new apiKeysHandlers instance with injected dependencies.
func NewAPIKeysHandlers(keys *services.APIKeyService) *apiKeysHandlers {
	return &apiKeysHandlers{
		keys: keys,
	}
}

// ListAPIKeys lists the workspace's API keys (display fields only, no secrets).
func (h *apiKeysHandlers) ListAPIKeys(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	keys, err := h.keys.List(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	items := make([]APIKeyResponse, 0, len(keys))
	for i := range keys {
		items = append(items, toAPIKeyResponse(&keys[i]))
	}

	RespondOK(c, gin.H{"api_keys": items})
}

// CreateAPIKeyRequest holds parameters for creating a workspace API key.
type CreateAPIKeyRequest struct {
	Name string `json:"name"`
}

// CreateAPIKey mints a new workspace API key. The plaintext key is returned
// exactly once in the "key" field and is never retrievable afterwards.
func (h *apiKeysHandlers) CreateAPIKey(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	plaintext, key, err := h.keys.Create(c.Request.Context(), ws.ID, req.Name, user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{
		"key":     plaintext,
		"api_key": toAPIKeyResponse(key),
	})
}

// ExchangeAPIKey mints a workspace-scoped API key for the authenticated
// caller. Membership (any role) is the only authorization — chat is a
// Member-level activity, so no workspace.write is required. The plaintext
// key is returned exactly once in the "key" field, mirroring CreateAPIKey.
func (h *apiKeysHandlers) ExchangeAPIKey(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	name := "exchanged key"
	var req CreateAPIKeyRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondError(c, domain.ErrInvalid)
			return
		}
		if req.Name != "" {
			name = req.Name
		}
	}

	plaintext, key, err := h.keys.Create(c.Request.Context(), ws.ID, name, user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{
		"key":     plaintext,
		"api_key": toAPIKeyResponse(key),
	})
}

// RevokeAPIKey revokes a workspace API key; revoked keys stop authenticating
// /v1 requests immediately.
func (h *apiKeysHandlers) RevokeAPIKey(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	id := c.Param("id")
	if id == "" {
		RespondError(c, domain.ErrNotFound)
		return
	}

	if err := h.keys.Revoke(c.Request.Context(), ws.ID, id); err != nil {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}
