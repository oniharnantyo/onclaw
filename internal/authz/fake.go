package authz

import (
	"context"
	"strings"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Fake is the in-memory Authorizer for fake-store unit tests elsewhere. It
// mirrors CasbinAuthorizer's model exactly — roleID → workspaceID → synced
// permission set, exact-or-prefix permission matching — so tests written
// against the fake exercise the same semantics as production; the shared
// parity test pins the two implementations together.
type Fake struct {
	mu     sync.RWMutex
	roles  RoleSource
	policy map[string]map[string]map[string]struct{} // roleID → workspaceID → permissions
}

var _ Authorizer = (*Fake)(nil)

// NewFake builds the fake and loads every role in src (mirroring
// NewCasbin's boot-time sync).
func NewFake(ctx context.Context, roles RoleSource) (*Fake, error) {
	f := &Fake{roles: roles, policy: map[string]map[string]map[string]struct{}{}}
	if err := f.Reload(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

// Enforce reports whether the role holds the permission in the workspace.
// An unsynced role, or a permission absent from the role's synced set, is a
// false with a nil error.
func (f *Fake) Enforce(ctx context.Context, roleID, workspaceID, permission string) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	perms, ok := f.policy[roleID][workspaceID]
	if !ok {
		return false, nil
	}
	for stored := range perms {
		if matchesPermission(stored, permission) {
			return true, nil
		}
	}
	return false, nil
}

// Sync replaces every previously synced lines for the role (across all
// workspaces) with its current permission set.
func (f *Fake) Sync(ctx context.Context, role *domain.Role) error {
	perms := make(map[string]struct{}, len(role.Permissions))
	for _, p := range role.Permissions {
		perms[p] = struct{}{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.policy, role.ID)
	f.policy[role.ID] = map[string]map[string]struct{}{
		role.WorkspaceID: perms,
	}
	return nil
}

// Reload drops and re-syncs every role from the role source. The source is
// read before any policy is dropped, so a source failure leaves the current
// policy serving.
func (f *Fake) Reload(ctx context.Context) error {
	roles, err := f.roles.ListRoles(ctx)
	if err != nil {
		return err
	}
	policy := make(map[string]map[string]map[string]struct{}, len(roles))
	for i := range roles {
		perms := make(map[string]struct{}, len(roles[i].Permissions))
		for _, p := range roles[i].Permissions {
			perms[p] = struct{}{}
		}
		ws, ok := policy[roles[i].ID]
		if !ok {
			ws = map[string]map[string]struct{}{}
			policy[roles[i].ID] = ws
		}
		ws[roles[i].WorkspaceID] = perms
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.policy = policy
	return nil
}

// matchesPermission reports whether a stored policy permission grants the
// required permission: exact equality, or prefix match when the stored side
// carries a "*". It mirrors casbin's util.KeyMatch with the stored policy
// permission as the pattern (second argument), keeping the fake and
// CasbinAuthorizer in lockstep.
func matchesPermission(stored, required string) bool {
	if i := strings.Index(stored, "*"); i >= 0 {
		if len(required) > i {
			return required[:i] == stored[:i]
		}
		return required == stored[:i]
	}
	return stored == required
}

// StaticRoleSource is a RoleSource over a fixed role slice — a convenient
// seam for tests and simple compositions.
type StaticRoleSource struct {
	Roles []domain.Role
}

// ListRoles returns the fixed role slice.
func (s *StaticRoleSource) ListRoles(ctx context.Context) ([]domain.Role, error) {
	return s.Roles, nil
}
