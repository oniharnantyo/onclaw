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

// adminWorkspaceHandlers handles instance-wide workspace management endpoints.
type adminWorkspaceHandlers struct {
	store   store.Store
	storage storage.Storage
}

// NewAdminWorkspaceHandlers creates a new adminWorkspaceHandlers instance with injected dependencies.
func NewAdminWorkspaceHandlers(st store.Store, strg ...storage.Storage) *adminWorkspaceHandlers {
	var s storage.Storage
	if len(strg) > 0 {
		s = strg[0]
	}
	return &adminWorkspaceHandlers{
		store:   st,
		storage: s,
	}
}

// AdminWorkspaceItem represents a workspace in the unscoped admin workspace listing.
type AdminWorkspaceItem struct {
	ID          string     `json:"id"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Timezone    string     `json:"timezone"`
	IsMaster    bool       `json:"is_master"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	MemberCount int        `json:"member_count"`
}

func (h *adminWorkspaceHandlers) findWorkspace(c *gin.Context) (*domain.Workspace, error) {
	wsParam := c.Param("ws")
	if wsParam == "" {
		wsParam = c.Param("id")
	}
	if wsParam == "" {
		return nil, domain.ErrNotFound
	}

	ws, err := h.store.Workspaces().BySlug(c.Request.Context(), wsParam)
	if err != nil && errors.Is(err, domain.ErrNotFound) {
		ws, err = h.store.Workspaces().ByID(c.Request.Context(), wsParam)
	}
	return ws, err
}

// AdminListWorkspaces lists all workspaces in the instance unscoped, including suspended ones,
// along with member counts.
func (h *adminWorkspaceHandlers) AdminListWorkspaces(c *gin.Context) {
	workspaces, err := h.store.Workspaces().ListAll(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}

	items := make([]AdminWorkspaceItem, 0, len(workspaces))
	for _, ws := range workspaces {
		members, err := h.store.Members().ListForWorkspace(c.Request.Context(), ws.ID)
		if err != nil {
			RespondError(c, err)
			return
		}

		items = append(items, AdminWorkspaceItem{
			ID:          ws.ID,
			Slug:        ws.Slug,
			Name:        ws.Name,
			Description: ws.Description,
			Timezone:    ws.Timezone,
			IsMaster:    ws.IsMaster,
			DisabledAt:  ws.DisabledAt,
			CreatedAt:   ws.CreatedAt,
			UpdatedAt:   ws.UpdatedAt,
			MemberCount: len(members),
		})
	}

	RespondOK(c, gin.H{"workspaces": items})
}

// AdminCreateWorkspaceRequest holds parameters for creating a new workspace as an admin.
type AdminCreateWorkspaceRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
	Timezone    string  `json:"timezone"`
	OwnerEmail  string  `json:"owner_email"`
}

// AdminCreateWorkspace creates a new workspace, seeds the three built-in roles,
// and assigns the designated user (by email) as the Owner. If the user does not exist,
// a passwordless account is auto-created.
func (h *adminWorkspaceHandlers) AdminCreateWorkspace(c *gin.Context) {
	var req AdminCreateWorkspaceRequest
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

	ownerEmail := domain.NormalizeEmail(req.OwnerEmail)
	if err := domain.ValidateEmail(ownerEmail); err != nil {
		RespondError(c, err)
		return
	}

	tz := strings.TrimSpace(req.Timezone)
	if tz == "" {
		tz = "UTC"
	}

	var createdWs *domain.Workspace
	var ownerRole *domain.Role
	var ownerMember *domain.Member
	var ownerUser *domain.User

	err := h.store.WithTx(c.Request.Context(), func(txStore store.Store) error {
		// Find or auto-provision owner user
		user, err := txStore.Users().ByEmail(c.Request.Context(), ownerEmail)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		if errors.Is(err, domain.ErrNotFound) || user == nil {
			userName := strings.Split(ownerEmail, "@")[0]
			user = &domain.User{
				Email: ownerEmail,
				Name:  userName,
			}
			if err := txStore.Users().Create(c.Request.Context(), user); err != nil {
				return err
			}
		}

		var desc string
		if req.Description != nil {
			desc = strings.TrimSpace(*req.Description)
		}

		ws := &domain.Workspace{
			Slug:        slug,
			Name:        name,
			Description: desc,
			Timezone:    tz,
			IsMaster:    false,
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

		// Assign owner
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
		ownerMember = member
		ownerUser = user
		return nil
	})

	if err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{
		"workspace": AdminWorkspaceItem{
			ID:          createdWs.ID,
			Slug:        createdWs.Slug,
			Name:        createdWs.Name,
			Description: createdWs.Description,
			Timezone:    createdWs.Timezone,
			IsMaster:    createdWs.IsMaster,
			DisabledAt:  createdWs.DisabledAt,
			CreatedAt:   createdWs.CreatedAt,
			UpdatedAt:   createdWs.UpdatedAt,
			MemberCount: 1,
		},
		"role":   ownerRole,
		"member": ownerMember,
		"user":   ownerUser,
	})
}

// AdminPatchWorkspaceRequest holds parameters for updating a workspace's name, description, and timezone by an admin.
type AdminPatchWorkspaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Timezone    *string `json:"timezone"`
}

// AdminPatchWorkspace renames a workspace and/or updates its description and timezone with creation-grade validation.
// The route is superadmin-gated, so the master workspace may be renamed and re-zoned here;
// its slug remains immutable.
func (h *adminWorkspaceHandlers) AdminPatchWorkspace(c *gin.Context) {
	ws, err := h.findWorkspace(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	var req AdminPatchWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if req.Name == nil && req.Description == nil && req.Timezone == nil {
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

	if req.Description != nil {
		ws.Description = strings.TrimSpace(*req.Description)
	}

	if req.Timezone != nil {
		trimmed := strings.TrimSpace(*req.Timezone)
		if err := domain.ValidateTimezone(trimmed); err != nil {
			RespondError(c, err)
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

// AdminTransferWorkspaceOwnerRequest holds parameters for transferring ownership of a workspace.
type AdminTransferWorkspaceOwnerRequest struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

// AdminTransferWorkspaceOwner performs an atomic ownership transfer for a workspace.
// The target user is assigned the built-in Owner role (and auto-added if not already a member).
// All other current owners in the workspace are demoted to the built-in Admin role.
// Master workspace ownership transfer is rejected.
func (h *adminWorkspaceHandlers) AdminTransferWorkspaceOwner(c *gin.Context) {
	ws, err := h.findWorkspace(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	if ws.IsMaster || ws.Slug == domain.MasterWorkspaceSlug {
		RespondError(c, fmt.Errorf("%w: cannot transfer ownership of master workspace", domain.ErrInvalid))
		return
	}

	var req AdminTransferWorkspaceOwnerRequest
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
	if reqUserID != "" {
		targetUser, err = h.store.Users().ByID(c.Request.Context(), reqUserID)
	} else {
		targetUser, err = h.store.Users().ByEmail(c.Request.Context(), reqEmail)
	}
	if err != nil {
		RespondError(c, err)
		return
	}

	var ownerRole *domain.Role
	err = h.store.WithTx(c.Request.Context(), func(txStore store.Store) error {
		oRole, err := txStore.Roles().FindByName(c.Request.Context(), ws.ID, domain.RoleOwner)
		if err != nil {
			return fmt.Errorf("owner role not found in workspace: %w", err)
		}
		ownerRole = oRole

		adminRole, err := txStore.Roles().FindByName(c.Request.Context(), ws.ID, domain.RoleAdmin)
		if err != nil {
			return fmt.Errorf("admin role not found in workspace: %w", err)
		}

		members, err := txStore.Members().ListForWorkspace(c.Request.Context(), ws.ID)
		if err != nil {
			return err
		}

		// Demote all other owners to Admin
		for _, m := range members {
			r := m.Role
			if r == nil {
				r, _ = txStore.Roles().ByID(c.Request.Context(), m.RoleID)
			}
			if r != nil && r.IsOwner {
				if m.UserID != targetUser.ID {
					if err := txStore.Members().UpdateRole(c.Request.Context(), ws.ID, m.UserID, adminRole.ID); err != nil {
						return err
					}
				}
			}
		}

		// Assign target user as Owner (auto-add if not a member)
		targetMember, err := txStore.Members().Get(c.Request.Context(), ws.ID, targetUser.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		if errors.Is(err, domain.ErrNotFound) || targetMember == nil {
			newMember := &domain.Member{
				WorkspaceID: ws.ID,
				UserID:      targetUser.ID,
				RoleID:      ownerRole.ID,
				Role:        ownerRole,
			}
			if err := txStore.Members().Add(c.Request.Context(), newMember); err != nil {
				return err
			}
		} else {
			if targetMember.RoleID != ownerRole.ID {
				if err := txStore.Members().UpdateRole(c.Request.Context(), ws.ID, targetUser.ID, ownerRole.ID); err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{
		"workspace": ws,
		"owner":     targetUser,
		"role":      ownerRole,
	})
}

// AdminListWorkspaceMembers lists all members of any specified workspace with user details and roles.
func (h *adminWorkspaceHandlers) AdminListWorkspaceMembers(c *gin.Context) {
	ws, err := h.findWorkspace(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	members, err := h.store.Members().ListForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

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

// AdminAddWorkspaceMemberRequest holds parameters for adding an existing user to a workspace as an admin.
type AdminAddWorkspaceMemberRequest struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	RoleID   string `json:"role_id"`
	RoleName string `json:"role_name"`
}

// AdminAddWorkspaceMember adds an existing user to any workspace with Admin or Member role.
// Owner role cannot be assigned through this endpoint (use owner transfer instead).
// Unknown users return 404. Already-member users return 409.
func (h *adminWorkspaceHandlers) AdminAddWorkspaceMember(c *gin.Context) {
	ws, err := h.findWorkspace(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	var req AdminAddWorkspaceMemberRequest
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
	if reqUserID != "" {
		targetUser, err = h.store.Users().ByID(c.Request.Context(), reqUserID)
	} else {
		targetUser, err = h.store.Users().ByEmail(c.Request.Context(), reqEmail)
	}
	if err != nil {
		RespondError(c, err)
		return
	}

	reqRoleID := strings.TrimSpace(req.RoleID)
	reqRoleName := strings.TrimSpace(req.RoleName)
	if reqRoleID == "" && reqRoleName == "" {
		RespondError(c, fmt.Errorf("%w: role_id or role_name is required", domain.ErrInvalid))
		return
	}

	var targetRole *domain.Role
	if reqRoleID != "" {
		targetRole, err = h.store.Roles().ByID(c.Request.Context(), reqRoleID)
	} else {
		targetRole, err = h.store.Roles().FindByName(c.Request.Context(), ws.ID, reqRoleName)
	}
	if err != nil || targetRole == nil || targetRole.WorkspaceID != ws.ID {
		RespondError(c, fmt.Errorf("%w: role not found in workspace", domain.ErrInvalid))
		return
	}

	if targetRole.IsOwner || targetRole.Name == domain.RoleOwner {
		RespondError(c, fmt.Errorf("%w: cannot assign owner role via member management; use owner transfer", domain.ErrInvalid))
		return
	}

	if targetRole.Name != domain.RoleAdmin && targetRole.Name != domain.RoleMember {
		RespondError(c, fmt.Errorf("%w: only admin and member roles can be assigned via member management", domain.ErrInvalid))
		return
	}

	var createdMember *domain.Member
	err = h.store.WithTx(c.Request.Context(), func(txStore store.Store) error {
		existing, err := txStore.Members().Get(c.Request.Context(), ws.ID, targetUser.ID)
		if err == nil && existing != nil {
			return fmt.Errorf("%w: user is already a member of this workspace", domain.ErrConflict)
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		m := &domain.Member{
			WorkspaceID: ws.ID,
			UserID:      targetUser.ID,
			RoleID:      targetRole.ID,
			Role:        targetRole,
		}
		if err := txStore.Members().Add(c.Request.Context(), m); err != nil {
			return err
		}
		createdMember = m
		return nil
	})

	if err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{
		"member": createdMember,
		"user":   targetUser,
		"role":   targetRole,
	})
}

// AdminDisableWorkspace suspends a workspace by slug or ID. Master workspace is protected from suspension.
func (h *adminWorkspaceHandlers) AdminDisableWorkspace(c *gin.Context) {
	ws, err := h.findWorkspace(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	if ws.IsMaster || ws.Slug == domain.MasterWorkspaceSlug {
		RespondError(c, fmt.Errorf("%w: cannot disable master workspace", domain.ErrInvalid))
		return
	}

	now := time.Now().UTC()
	ws.DisabledAt = &now

	if err := h.store.Workspaces().Update(c.Request.Context(), ws); err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"workspace": ws})
}

// AdminEnableWorkspace restores / enables a suspended workspace by slug or ID.
func (h *adminWorkspaceHandlers) AdminEnableWorkspace(c *gin.Context) {
	ws, err := h.findWorkspace(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	ws.DisabledAt = nil

	if err := h.store.Workspaces().Update(c.Request.Context(), ws); err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"workspace": ws})
}
