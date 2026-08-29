package domain

import "time"

// Built-in role name constants.
const (
	RoleOwner      = "Owner"
	RoleAdmin      = "Admin"
	RoleMember     = "Member"
	RoleSuperadmin = "Superadmin"
)

// Role represents a configurable or built-in role within a workspace.
type Role struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	IsOwner     bool      `json:"is_owner"`
	Permissions []string  `json:"permissions"`
	BuiltIn     bool      `json:"built_in"`
	CreatedAt   time.Time `json:"created_at"`
}
