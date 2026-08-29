package handlers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// adminSuperadminHandlers handles instance superadmin role grant and revoke endpoints.
type adminSuperadminHandlers struct {
	store store.Store
}

// NewAdminSuperadminHandlers creates a new adminSuperadminHandlers instance with injected dependencies.
func NewAdminSuperadminHandlers(st store.Store) *adminSuperadminHandlers {
	return &adminSuperadminHandlers{
		store: st,
	}
}

// AdminGrantSuperadminRequest holds parameters for granting the Superadmin role to a user.
type AdminGrantSuperadminRequest struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

// AdminGrantSuperadmin grants the Superadmin role in the master tenant to the designated user.
func (h *adminSuperadminHandlers) AdminGrantSuperadmin(c *gin.Context) {
	var req AdminGrantSuperadminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	reqUserID := strings.TrimSpace(req.UserID)
	reqEmail := domain.NormalizeEmail(req.Email)

	if reqUserID == "" && reqEmail == "" {
		RespondError(c, fmt.Errorf("%w: user_id or email is required", domain.ErrInvalid))
		return
	}

	var targetUser *domain.User
	var err error

	if reqUserID != "" {
		targetUser, err = h.store.Users().ByID(c.Request.Context(), reqUserID)
	} else {
		targetUser, err = h.store.Users().ByEmail(c.Request.Context(), reqEmail)
	}

	if err != nil {
		RespondError(c, err)
		return
	}

	var superadminRole *domain.Role
	var grantedMember *domain.Member

	err = h.store.WithTx(c.Request.Context(), func(txStore store.Store) error {
		master, err := txStore.Workspaces().BySlug(c.Request.Context(), domain.MasterWorkspaceSlug)
		if err != nil {
			return err
		}

		sRole, err := txStore.Roles().FindByName(c.Request.Context(), master.ID, domain.RoleSuperadmin)
		if err != nil {
			return fmt.Errorf("superadmin role not found in master tenant: %w", err)
		}
		superadminRole = sRole

		existingMember, err := txStore.Members().Get(c.Request.Context(), master.ID, targetUser.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		if errors.Is(err, domain.ErrNotFound) || existingMember == nil {
			newMember := &domain.Member{
				WorkspaceID: master.ID,
				UserID:      targetUser.ID,
				RoleID:      sRole.ID,
				Role:        sRole,
			}
			if err := txStore.Members().Add(c.Request.Context(), newMember); err != nil {
				return err
			}
			grantedMember = newMember
		} else {
			if existingMember.RoleID == sRole.ID {
				return fmt.Errorf("%w: user is already a superadmin", domain.ErrConflict)
			}

			if err := txStore.Members().UpdateRole(c.Request.Context(), master.ID, targetUser.ID, sRole.ID); err != nil {
				return err
			}
			existingMember.RoleID = sRole.ID
			existingMember.Role = sRole
			grantedMember = existingMember
		}

		return nil
	})

	if err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{
		"member": grantedMember,
		"user":   targetUser,
		"role":   superadminRole,
	})
}

// AdminRevokeSuperadmin revokes the Superadmin role from a user in the master tenant.
// Enforces the last-admin guard: removing or demoting the last remaining superadmin is rejected.
func (h *adminSuperadminHandlers) AdminRevokeSuperadmin(c *gin.Context) {
	targetUID := c.Param("uid")
	if targetUID == "" {
		AbortNotFound(c, "user not found")
		return
	}

	err := h.store.WithTx(c.Request.Context(), func(txStore store.Store) error {
		master, err := txStore.Workspaces().BySlug(c.Request.Context(), domain.MasterWorkspaceSlug)
		if err != nil {
			return err
		}

		superadminRole, err := txStore.Roles().FindByName(c.Request.Context(), master.ID, domain.RoleSuperadmin)
		if err != nil {
			return fmt.Errorf("superadmin role not found in master tenant: %w", err)
		}

		member, err := txStore.Members().Get(c.Request.Context(), master.ID, targetUID)
		if err != nil {
			return err
		}

		if member.RoleID != superadminRole.ID {
			return fmt.Errorf("%w: user is not a superadmin", domain.ErrInvalid)
		}

		// Last-admin guard
		count, err := txStore.Roles().CountMembers(c.Request.Context(), superadminRole.ID)
		if err != nil {
			return err
		}

		if count <= 1 {
			return domain.ErrLastOwnerProtected
		}

		return txStore.Members().Remove(c.Request.Context(), master.ID, targetUID)
	})

	if err != nil {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}
