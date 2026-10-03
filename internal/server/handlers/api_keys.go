package handlers

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/authz"
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
	// authz is the permission authorizer (fix-role-permission-audit D7):
	// listing and revocation are creator-symmetric — the creating member
	// manages their own keys without workspace.write.
	authz authz.Authorizer
}

// NewAPIKeysHandlers creates a new apiKeysHandlers instance with injected dependencies.
func NewAPIKeysHandlers(keys *services.APIKeyService, authz authz.Authorizer) *apiKeysHandlers {
	return &apiKeysHandlers{
		keys:  keys,
		authz: authz,
	}
}

// callerHolds reports whether the request's resolved role holds the given
// workspace permission through the authorizer port (fix-role-permission-audit
// D1: in-handler checks ride the same evaluation point as the middleware).
func (h *apiKeysHandlers) callerHolds(c *gin.Context, permission string) (bool, error) {
	ws := MustCurrentWorkspace(c)
	role := MustCurrentRole(c)
	return h.authz.Enforce(c.Request.Context(), role.ID, ws.ID, permission)
}

// ListAPIKeys lists the workspace's API keys (display fields only, no
// secrets). Creator-symmetric (fix-role-permission-audit D7): a caller whose
// role lacks workspace.write sees only the keys they created; workspace.write
// holders see every workspace key.
func (h *apiKeysHandlers) ListAPIKeys(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	keys, err := h.keys.List(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	seeAll, err := h.callerHolds(c, domain.WorkspaceWrite)
	if err != nil {
		RespondError(c, err)
		return
	}

	items := make([]APIKeyResponse, 0, len(keys))
	for i := range keys {
		if !seeAll && keys[i].CreatedBy != user.ID {
			continue
		}
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
// /v1 requests immediately. Creator-symmetric (fix-role-permission-audit D7):
// the key's creator may revoke their own key without workspace.write;
// revoking another member's key requires workspace.write — anyone else gets
// 403. Unknown or foreign-workspace keys ride the service's not-found.
func (h *apiKeysHandlers) RevokeAPIKey(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	id := c.Param("id")
	if id == "" {
		RespondError(c, domain.ErrNotFound)
		return
	}

	// The key store has no by-ID read; the workspace's key list is the
	// lookup (short rows, rare path). An unknown id fails not-found exactly
	// as the revoke would.
	keys, err := h.keys.List(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	var target *domain.WorkspaceAPIKey
	for i := range keys {
		if keys[i].ID == id {
			target = &keys[i]
			break
		}
	}
	if target == nil {
		RespondError(c, domain.ErrNotFound)
		return
	}

	if target.CreatedBy != user.ID {
		allowed, err := h.callerHolds(c, domain.WorkspaceWrite)
		if err != nil {
			RespondError(c, err)
			return
		}
		if !allowed {
			RespondError(c, fmt.Errorf("%w: revoking another member's API key requires workspace.write", domain.ErrForbidden))
			return
		}
	}

	if err := h.keys.Revoke(c.Request.Context(), ws.ID, id); err != nil {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}
