// Package authz is the single evaluation point for "does role R in
// workspace W hold permission P" (fix-role-permission-audit D1). The roles
// table stays the source of truth; an Authorizer holds an in-memory policy
// synced from role rows at boot and at the workspace-creation seeding seam,
// so permission verdicts no longer require loading the role per request.
//
// The port is deliberately narrow so the engine stays swappable and
// non-HTTP consumers (e.g. the agent connection gate) share the same seam.
package authz

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Authorizer decides whether a role holds a permission within a workspace.
type Authorizer interface {
	// Enforce reports whether the role holds the permission in the workspace.
	// Error only on internal failure (nil/no synced policy is a false, not an error).
	Enforce(ctx context.Context, roleID, workspaceID, permission string) (bool, error)
	// Sync writes through one role's current permission set, replacing any
	// previously synced lines for that role. Called at boot for every role
	// row and at the workspace-creation seeding seam.
	Sync(ctx context.Context, role *domain.Role) error
	// Reload drops and re-syncs every role from the role store.
	Reload(ctx context.Context) error
}

// RoleSource enumerates every role row in the instance. It is the narrow
// seam the authorizer constructors load from; the composition root adapts
// it to the role store (workspaces × roles) at boot.
type RoleSource interface {
	ListRoles(ctx context.Context) ([]domain.Role, error)
}
