package domain

import "time"

// Member represents a user's membership in a workspace.
type Member struct {
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	RoleID      string    `json:"role_id"`
	Role        *Role     `json:"role,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// MemberView represents a joined view of a member with user, workspace, and role information.
type MemberView struct {
	WorkspaceID   string     `json:"workspace_id"`
	WorkspaceSlug string     `json:"workspace_slug,omitempty"`
	WorkspaceName string     `json:"workspace_name,omitempty"`
	UserID        string     `json:"user_id"`
	Email         string     `json:"email"`
	Name          string     `json:"name"`
	AvatarKey     *string    `json:"avatar_key,omitempty"`
	AvatarURL     *string    `json:"avatar_url,omitempty"`
	RoleID        string     `json:"role_id"`
	RoleName      string     `json:"role_name"`
	Role          *Role      `json:"role,omitempty"`
	Workspace     *Workspace `json:"workspace,omitempty"`
	Invited       bool       `json:"invited"`
	JoinedAt      time.Time  `json:"joined_at"`
}
