package handlers

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// adminUserHandlers handles instance-wide user management endpoints.
type adminUserHandlers struct {
	store  store.Store
	hasher auth.PasswordHasher
}

// NewAdminUserHandlers creates a new adminUserHandlers instance with injected dependencies.
func NewAdminUserHandlers(st store.Store) *adminUserHandlers {
	return &adminUserHandlers{
		store:  st,
		hasher: auth.NewPasswordHasher(),
	}
}

// AdminListUsers lists all users across the instance, including their superadmin status.
func (h *adminUserHandlers) AdminListUsers(c *gin.Context) {
	users, err := h.store.Users().List(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}

	master, err := h.store.Workspaces().BySlug(c.Request.Context(), domain.MasterWorkspaceSlug)
	if err == nil && master != nil {
		masterMembers, err := h.store.Members().ListForWorkspace(c.Request.Context(), master.ID)
		if err == nil {
			superadminUserIDs := make(map[string]bool)
			for _, m := range masterMembers {
				r := m.Role
				if r == nil {
					r, _ = h.store.Roles().ByID(c.Request.Context(), m.RoleID)
				}
				if r != nil && (r.IsOwner || r.Name == domain.RoleSuperadmin) {
					superadminUserIDs[m.UserID] = true
				}
			}
			for i := range users {
				if superadminUserIDs[users[i].ID] {
					users[i].IsSuperadmin = true
				}
			}
		}
	}

	RespondOK(c, gin.H{"users": users})
}

// AdminCreateUserRequest holds parameters for creating a new user by an admin.
type AdminCreateUserRequest struct {
	Email    string  `json:"email"`
	Name     string  `json:"name"`
	Password *string `json:"password,omitempty"`
}

// AdminCreateUser creates a new user identity (with optional password).
func (h *adminUserHandlers) AdminCreateUser(c *gin.Context) {
	var req AdminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	email := domain.NormalizeEmail(req.Email)
	if err := domain.ValidateEmail(email); err != nil {
		RespondError(c, err)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	var hash *string
	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		hPass, err := h.hasher.Hash(*req.Password)
		if err != nil {
			RespondError(c, fmt.Errorf("%w: failed to hash password: %v", domain.ErrInvalid, err))
			return
		}
		hash = &hPass
	}

	user := &domain.User{
		Email:        email,
		Name:         name,
		PasswordHash: hash,
	}

	if err := h.store.Users().Create(c.Request.Context(), user); err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{"user": user})
}

// AdminDisableUser globally disables a user account by ID.
func (h *adminUserHandlers) AdminDisableUser(c *gin.Context) {
	uid := c.Param("uid")
	if uid == "" {
		AbortNotFound(c, "user not found")
		return
	}

	user, err := h.store.Users().ByID(c.Request.Context(), uid)
	if err != nil {
		RespondError(c, err)
		return
	}

	now := time.Now().UTC()
	if err := h.store.Users().SetDisabled(c.Request.Context(), user.ID, &now); err != nil {
		RespondError(c, err)
		return
	}

	user.DisabledAt = &now
	RespondOK(c, gin.H{"user": user})
}

// AdminEnableUser globally enables a disabled user account by ID.
func (h *adminUserHandlers) AdminEnableUser(c *gin.Context) {
	uid := c.Param("uid")
	if uid == "" {
		AbortNotFound(c, "user not found")
		return
	}

	user, err := h.store.Users().ByID(c.Request.Context(), uid)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			AbortNotFound(c, "user not found")
			return
		}
		RespondError(c, err)
		return
	}

	if err := h.store.Users().SetDisabled(c.Request.Context(), user.ID, nil); err != nil {
		RespondError(c, err)
		return
	}

	user.DisabledAt = nil
	RespondOK(c, gin.H{"user": user})
}
