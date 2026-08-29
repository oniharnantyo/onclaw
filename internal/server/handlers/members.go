package handlers

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// memberHandlers handles workspace membership endpoints.
type memberHandlers struct {
	store   store.Store
	storage storage.Storage
}

// NewMemberHandlers creates a new memberHandlers instance with injected dependencies.
func NewMemberHandlers(st store.Store, strg storage.Storage) *memberHandlers {
	return &memberHandlers{
		store:   st,
		storage: strg,
	}
}

// MemberItemResponse represents a member item in workspace member listings.
type MemberItemResponse struct {
	UserID    string       `json:"user_id"`
	Email     string       `json:"email"`
	Name      string       `json:"name"`
	AvatarKey *string      `json:"avatar_key,omitempty"`
	AvatarURL *string      `json:"avatar_url,omitempty"`
	RoleID    string       `json:"role_id"`
	RoleName  string       `json:"role_name"`
	Role      *domain.Role `json:"role,omitempty"`
	Invited   bool         `json:"invited"`
	JoinedAt  time.Time    `json:"joined_at"`
}

// ListMembers lists all members of the current workspace with user details and roles.
func (h *memberHandlers) ListMembers(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	members, err := h.store.Members().ListForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	// TODO(perf): batch user and role lookups when workspaces grow
	items := make([]MemberItemResponse, 0, len(members))
	for _, m := range members {
		u, err := h.store.Users().ByID(c.Request.Context(), m.UserID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			RespondError(c, err)
			return
		}

		role := m.Role
		if role == nil {
			role, _ = h.store.Roles().ByID(c.Request.Context(), m.RoleID)
		}

		roleName := ""
		if role != nil {
			roleName = role.Name
		}

		item := MemberItemResponse{
			UserID:   m.UserID,
			RoleID:   m.RoleID,
			RoleName: roleName,
			Role:     role,
			JoinedAt: m.CreatedAt,
		}

		if u != nil {
			item.Email = u.Email
			item.Name = u.Name
			item.AvatarKey = u.AvatarKey
			item.AvatarURL = u.AvatarURL
			item.Invited = u.PasswordHash == nil || *u.PasswordHash == ""
			if h.storage != nil && u.AvatarKey != nil && *u.AvatarKey != "" {
				url := h.storage.URL(*u.AvatarKey)
				item.AvatarURL = &url
			}
		}

		items = append(items, item)
	}

	RespondOK(c, gin.H{"members": items})
}

// AddMemberRequest holds parameters for adding a new member to a workspace.
type AddMemberRequest struct {
	Email  string `json:"email"`
	RoleID string `json:"role_id"`
}

// AddMember adds a user to the workspace by email, auto-creating a passwordless user if necessary.
func (h *memberHandlers) AddMember(c *gin.Context) {
	actorRole := MustCurrentRole(c)
	ws := MustCurrentWorkspace(c)

	var req AddMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	email := domain.NormalizeEmail(req.Email)
	if err := domain.ValidateEmail(email); err != nil {
		RespondError(c, err)
		return
	}

	roleID := strings.TrimSpace(req.RoleID)
	if roleID == "" {
		RespondError(c, fmt.Errorf("%w: role_id is required", domain.ErrInvalid))
		return
	}

	targetRole, err := h.store.Roles().ByID(c.Request.Context(), roleID)
	if err != nil || targetRole.WorkspaceID != ws.ID {
		RespondError(c, fmt.Errorf("%w: role not found in workspace", domain.ErrNotFound))
		return
	}

	// Permission algebra guard: canAssign (role ⊆ actor)
	if !domain.CanAssign(actorRole.Permissions, targetRole.Permissions) {
		AbortForbidden(c, "insufficient permissions to assign this role")
		return
	}

	var createdMember *domain.Member
	var targetUser *domain.User

	err = h.store.WithTx(c.Request.Context(), func(txStore store.Store) error {
		// Look up user or auto-provision
		u, err := txStore.Users().ByEmail(c.Request.Context(), email)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		if errors.Is(err, domain.ErrNotFound) || u == nil {
			name := strings.Split(email, "@")[0]
			u = &domain.User{
				Email: email,
				Name:  name,
			}
			if err := txStore.Users().Create(c.Request.Context(), u); err != nil {
				return err
			}
		}

		// Check if already a member
		existing, err := txStore.Members().Get(c.Request.Context(), ws.ID, u.ID)
		if err == nil && existing != nil {
			return fmt.Errorf("%w: user is already a member of this workspace", domain.ErrConflict)
		}

		m := &domain.Member{
			WorkspaceID: ws.ID,
			UserID:      u.ID,
			RoleID:      targetRole.ID,
			Role:        targetRole,
		}
		if err := txStore.Members().Add(c.Request.Context(), m); err != nil {
			return err
		}

		targetUser = u
		createdMember = m
		return nil
	})

	if err != nil {
		RespondError(c, err)
		return
	}

	if h.storage != nil && targetUser.AvatarKey != nil && *targetUser.AvatarKey != "" {
		url := h.storage.URL(*targetUser.AvatarKey)
		targetUser.AvatarURL = &url
	}

	RespondCreated(c, gin.H{
		"member": createdMember,
		"user":   targetUser,
		"role":   targetRole,
	})
}

// PatchMemberRequest holds parameters for updating a member's role.
type PatchMemberRequest struct {
	RoleID string `json:"role_id"`
}

// PatchMember updates a member's role, enforcing permission algebra guards and last owner protection.
func (h *memberHandlers) PatchMember(c *gin.Context) {
	actorRole := MustCurrentRole(c)
	ws := MustCurrentWorkspace(c)
	targetUID := c.Param("uid")
	if targetUID == "" {
		AbortNotFound(c, "member not found")
		return
	}

	var req PatchMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	roleID := strings.TrimSpace(req.RoleID)
	if roleID == "" {
		RespondError(c, fmt.Errorf("%w: role_id is required", domain.ErrInvalid))
		return
	}

	targetMember, err := h.store.Members().Get(c.Request.Context(), ws.ID, targetUID)
	if err != nil {
		RespondError(c, err)
		return
	}

	targetRole := targetMember.Role
	if targetRole == nil {
		targetRole, err = h.store.Roles().ByID(c.Request.Context(), targetMember.RoleID)
		if err != nil {
			RespondError(c, err)
			return
		}
	}

	newRole, err := h.store.Roles().ByID(c.Request.Context(), roleID)
	if err != nil || newRole.WorkspaceID != ws.ID {
		RespondError(c, fmt.Errorf("%w: role not found in workspace", domain.ErrNotFound))
		return
	}

	// Last owner protection guard: demoting an owner when no other owner exists
	if targetRole.IsOwner && !newRole.IsOwner {
		allMembers, err := h.store.Members().ListForWorkspace(c.Request.Context(), ws.ID)
		if err != nil {
			RespondError(c, err)
			return
		}

		ownerCount := 0
		for _, m := range allMembers {
			r := m.Role
			if r == nil {
				r, _ = h.store.Roles().ByID(c.Request.Context(), m.RoleID)
			}
			if r != nil && r.IsOwner {
				ownerCount++
			}
		}

		if err := domain.ValidateLastOwner(true, ownerCount); err != nil {
			RespondError(c, err)
			return
		}
	}

	// Guard: canAssign (role ⊆ actor)
	if !domain.CanAssign(actorRole.Permissions, newRole.Permissions) {
		AbortForbidden(c, "insufficient permissions to assign this role")
		return
	}

	// Owner bypass of CanEdit: Owners can modify roles of any member (including peer owners
	// for ownership transfer and management), honoring the explicit `is_owner` special case from the design.
	// Non-owners must satisfy the strict permission algebra guard: canEdit (target ⊊ actor).
	if !actorRole.IsOwner && !domain.CanEdit(actorRole.Permissions, targetRole.Permissions) {
		AbortForbidden(c, "cannot modify member with equal or higher permissions")
		return
	}

	if err := h.store.Members().UpdateRole(c.Request.Context(), ws.ID, targetUID, newRole.ID); err != nil {
		RespondError(c, err)
		return
	}

	targetMember.RoleID = newRole.ID
	targetMember.Role = newRole

	RespondOK(c, gin.H{
		"member": targetMember,
		"role":   newRole,
	})
}

// DeleteMember removes a member from a workspace, supporting both self-removal (leave) and administrative removal.
func (h *memberHandlers) DeleteMember(c *gin.Context) {
	actor := MustCurrentUser(c)
	actorRole := MustCurrentRole(c)
	ws := MustCurrentWorkspace(c)
	targetUID := c.Param("uid")
	if targetUID == "" {
		AbortNotFound(c, "member not found")
		return
	}

	targetMember, err := h.store.Members().Get(c.Request.Context(), ws.ID, targetUID)
	if err != nil {
		RespondError(c, err)
		return
	}

	targetRole := targetMember.Role
	if targetRole == nil {
		targetRole, err = h.store.Roles().ByID(c.Request.Context(), targetMember.RoleID)
		if err != nil {
			RespondError(c, err)
			return
		}
	}

	// Last owner protection guard
	if targetRole.IsOwner {
		allMembers, err := h.store.Members().ListForWorkspace(c.Request.Context(), ws.ID)
		if err != nil {
			RespondError(c, err)
			return
		}

		ownerCount := 0
		for _, m := range allMembers {
			r := m.Role
			if r == nil {
				r, _ = h.store.Roles().ByID(c.Request.Context(), m.RoleID)
			}
			if r != nil && r.IsOwner {
				ownerCount++
			}
		}

		if err := domain.ValidateLastOwner(true, ownerCount); err != nil {
			RespondError(c, err)
			return
		}
	}

	// If removing someone else, verify permissions and edit algebra
	if actor.ID != targetUID {
		if !domain.HasPermission(actorRole.Permissions, domain.MembersRemove) {
			AbortForbidden(c, "insufficient permissions to remove members")
			return
		}

		// Owner bypass of CanEdit: Owners can remove members (including peer owners for owner
		// transfer and management, as long as last-owner guard passes), honoring the explicit `is_owner`
		// special case from the design. Non-owners must satisfy the strict permission algebra guard: canEdit (target ⊊ actor).
		if !actorRole.IsOwner && !domain.CanEdit(actorRole.Permissions, targetRole.Permissions) {
			AbortForbidden(c, "cannot remove member with equal or higher permissions")
			return
		}
	}

	if err := h.store.Members().Remove(c.Request.Context(), ws.ID, targetUID); err != nil {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}
