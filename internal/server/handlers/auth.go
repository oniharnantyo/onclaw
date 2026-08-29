package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
)

// authHandlers handles authentication and current user profile endpoints.
type authHandlers struct {
	auth    auth.Service
	storage storage.Storage
}

// NewAuthHandlers creates a new authHandlers instance with injected dependencies.
func NewAuthHandlers(authService auth.Service, strg storage.Storage) *authHandlers {
	return &authHandlers{
		auth:    authService,
		storage: strg,
	}
}

// LoginRequestBody supports both flat credentials and nested credentials payload.
type LoginRequestBody struct {
	Provider    string            `json:"provider"`
	Email       string            `json:"email"`
	Password    string            `json:"password"`
	Credentials map[string]string `json:"credentials"`
}

// Login handles user authentication via the configured auth provider and issues a session token.
func (h *authHandlers) Login(c *gin.Context) {
	var req LoginRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	creds := req.Credentials
	if creds == nil {
		creds = make(map[string]string)
	}
	if req.Email != "" && creds["email"] == "" {
		creds["email"] = req.Email
	}
	if req.Password != "" && creds["password"] == "" {
		creds["password"] = req.Password
	}

	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = auth.ProviderPassword
	}

	if h == nil || h.auth == nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	result, err := h.auth.Login(c.Request.Context(), auth.LoginRequest{
		Provider:    provider,
		Credentials: creds,
	})
	if err != nil {
		RespondError(c, err)
		return
	}

	if h.storage != nil && result.User != nil && result.User.AvatarKey != nil && *result.User.AvatarKey != "" {
		url := h.storage.URL(*result.User.AvatarKey)
		result.User.AvatarURL = &url
	}

	RespondOK(c, result)
}

// Logout terminates the current session (placeholder for JWT discard).
func (h *authHandlers) Logout(c *gin.Context) {
	RespondNoContent(c)
}

// Me returns the current authenticated user profile and all workspace memberships.
func (h *authHandlers) Me(c *gin.Context) {
	user := MustCurrentUser(c)

	if h == nil || h.auth == nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	result, err := h.auth.Me(c.Request.Context(), user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	if h.storage != nil {
		if result.User != nil && result.User.AvatarKey != nil && *result.User.AvatarKey != "" {
			url := h.storage.URL(*result.User.AvatarKey)
			result.User.AvatarURL = &url
		}
		for i := range result.Memberships {
			if result.Memberships[i].AvatarKey != nil && *result.Memberships[i].AvatarKey != "" {
				url := h.storage.URL(*result.Memberships[i].AvatarKey)
				result.Memberships[i].AvatarURL = &url
			}
		}
	}

	RespondOK(c, result)
}
