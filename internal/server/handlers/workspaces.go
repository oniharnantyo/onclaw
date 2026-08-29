package handlers

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceHandlers handles workspace lifecycle and retrieval endpoints.
type workspaceHandlers struct {
	store store.Store
}

// NewWorkspaceHandlers creates a new workspaceHandlers instance with injected dependencies.
func NewWorkspaceHandlers(st store.Store) *workspaceHandlers {
	return &workspaceHandlers{
		store: st,
	}
}

// ListWorkspaces lists all workspaces and roles for the authenticated user.
func (h *workspaceHandlers) ListWorkspaces(c *gin.Context) {
	user := MustCurrentUser(c)

	memberships, err := h.store.Members().ListForUser(c.Request.Context(), user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"workspaces": memberships})
}

// CreateWorkspaceRequest holds the payload for creating a new workspace.
type CreateWorkspaceRequest struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Timezone string `json:"timezone"`
}

// CreateWorkspace creates a new workspace, seeds the three built-in roles,
// and assigns the authenticated creator as the Owner within a single transaction.
func (h *workspaceHandlers) CreateWorkspace(c *gin.Context) {
	user := MustCurrentUser(c)

	var req CreateWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		RespondError(c, fmt.Errorf("%w: workspace name cannot be empty", domain.ErrInvalid))
		return
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		RespondError(c, fmt.Errorf("%w: workspace slug cannot be empty", domain.ErrInvalid))
		return
	}

	if err := domain.ValidateSlug(slug); err != nil {
		RespondError(c, err)
		return
	}

	tz := strings.TrimSpace(req.Timezone)
	if tz == "" {
		tz = "UTC"
	}

	var createdWs *domain.Workspace
	var ownerRole *domain.Role
	var creatorMember *domain.Member

	err := h.store.WithTx(c.Request.Context(), func(txStore store.Store) error {
		ws := &domain.Workspace{
			Slug:     slug,
			Name:     name,
			Timezone: tz,
			IsMaster: false,
		}
		if err := txStore.Workspaces().Create(c.Request.Context(), ws); err != nil {
			return err
		}

		// Built-in Owner role
		oRole := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        domain.RoleOwner,
			IsOwner:     true,
			Permissions: domain.OwnerPermissions,
			BuiltIn:     true,
		}
		if err := txStore.Roles().Create(c.Request.Context(), oRole); err != nil {
			return err
		}

		// Built-in Admin role
		aRole := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        domain.RoleAdmin,
			IsOwner:     false,
			Permissions: domain.AdminPermissions,
			BuiltIn:     true,
		}
		if err := txStore.Roles().Create(c.Request.Context(), aRole); err != nil {
			return err
		}

		// Built-in Member role
		mRole := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        domain.RoleMember,
			IsOwner:     false,
			Permissions: domain.MemberPermissions,
			BuiltIn:     true,
		}
		if err := txStore.Roles().Create(c.Request.Context(), mRole); err != nil {
			return err
		}

		// Assign creator as Owner
		member := &domain.Member{
			WorkspaceID: ws.ID,
			UserID:      user.ID,
			RoleID:      oRole.ID,
		}
		if err := txStore.Members().Add(c.Request.Context(), member); err != nil {
			return err
		}
		member.Role = oRole

		createdWs = ws
		ownerRole = oRole
		creatorMember = member
		return nil
	})

	if err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{
		"workspace": createdWs,
		"role":      ownerRole,
		"member":    creatorMember,
	})
}

// GetWorkspace returns details of the currently resolved workspace and current caller's role.
func (h *workspaceHandlers) GetWorkspace(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	role := MustCurrentRole(c)
	member := MustCurrentMember(c)

	RespondOK(c, gin.H{
		"workspace": ws,
		"role":      role,
		"my_role":   role,
		"member":    member,
	})
}

// PatchWorkspaceRequest holds editable fields for a workspace.
type PatchWorkspaceRequest struct {
	Name     *string `json:"name"`
	Timezone *string `json:"timezone"`
}

// PatchWorkspace updates name and timezone of the current workspace. Slug remains immutable.
// The master workspace is editable only by superadmins: the master tenant has no role with
// workspace.write other than Superadmin, so the middleware permission guard enforces that rule.
func (h *workspaceHandlers) PatchWorkspace(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req PatchWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if req.Name == nil && req.Timezone == nil {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: workspace name cannot be empty", domain.ErrInvalid))
			return
		}
		ws.Name = trimmed
	}

	if req.Timezone != nil {
		trimmed := strings.TrimSpace(*req.Timezone)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: timezone cannot be empty", domain.ErrInvalid))
			return
		}
		ws.Timezone = trimmed
	}

	if err := h.store.Workspaces().Update(c.Request.Context(), ws); err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"workspace": ws})
}
