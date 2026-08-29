package domain

import "time"

// MasterWorkspaceSlug is the reserved slug for the master tenant.
const MasterWorkspaceSlug = "master"

// Workspace represents a tenant in OnClaw.
type Workspace struct {
	ID         string     `json:"id"`
	Slug       string     `json:"slug"`
	Name       string     `json:"name"`
	Timezone   string     `json:"timezone"`
	IsMaster   bool       `json:"is_master"`
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// IsSuspended reports whether the workspace is suspended (disabled).
func (w *Workspace) IsSuspended() bool {
	return w != nil && w.DisabledAt != nil
}
