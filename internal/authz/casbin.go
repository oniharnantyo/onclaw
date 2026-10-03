package authz

import (
	"context"
	"fmt"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// casbinModel is the embedded RBAC-with-domains model (D2) with no grouping
// lines: the middleware resolves the concrete role per request, so
// user→role grouping would only duplicate the members table. keyMatch
// treats the stored policy permission as the pattern, so a synced
// "agents.*" line grants "agents.read"; catalog permissions contain no "*",
// so every built-in set matches exactly and every verdict is identical to
// domain.HasPermission (golden parity).
const casbinModel = `
[request_definition]
r = sub, dom, obj

[policy_definition]
p = sub, dom, obj

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && r.dom == p.dom && (r.obj == p.obj || keyMatch(r.obj, p.obj))
`

// CasbinAuthorizer is the Authorizer backed by casbin running the embedded
// model with an in-memory policy set — no casbin_rule table and no storage
// adapter (engine-only shape, D1). A plain enforcer guarded by an RWMutex
// keeps Enforce atomic against Sync/Reload under concurrent HTTP traffic.
type CasbinAuthorizer struct {
	mu       sync.RWMutex
	enforcer *casbin.Enforcer
	roles    RoleSource
}

var _ Authorizer = (*CasbinAuthorizer)(nil)

// NewCasbin builds the enforcer from the embedded model and loads every
// role in src into the in-memory policy set (the boot-time sync).
func NewCasbin(ctx context.Context, roles RoleSource) (*CasbinAuthorizer, error) {
	m, err := model.NewModelFromString(casbinModel)
	if err != nil {
		return nil, fmt.Errorf("authz: embedded model: %w", err)
	}
	// Model only, no adapter: the policy starts empty and stays in memory.
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("authz: casbin enforcer: %w", err)
	}
	a := &CasbinAuthorizer{enforcer: e, roles: roles}
	if err := a.Reload(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// Enforce reports whether the role holds the permission in the workspace.
// An unsynced role, or a permission absent from the role's synced set, is a
// false with a nil error.
func (a *CasbinAuthorizer) Enforce(ctx context.Context, roleID, workspaceID, permission string) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	allowed, err := a.enforcer.Enforce(roleID, workspaceID, permission)
	if err != nil {
		return false, fmt.Errorf("authz: enforce %q for role %q in workspace %q: %w", permission, roleID, workspaceID, err)
	}
	return allowed, nil
}

// Sync replaces every previously synced line for the role (across all
// workspaces) with its current permission set.
func (a *CasbinAuthorizer) Sync(ctx context.Context, role *domain.Role) error {
	lines := policyLines(role)
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.enforcer.RemoveFilteredPolicy(0, role.ID); err != nil {
		return fmt.Errorf("authz: drop old lines for role %q: %w", role.ID, err)
	}
	if _, err := a.enforcer.AddPolicies(lines); err != nil {
		return fmt.Errorf("authz: sync role %q: %w", role.ID, err)
	}
	return nil
}

// Reload drops and re-syncs every role from the role store. The store is
// read before any policy is dropped, so a store failure leaves the current
// policy serving.
func (a *CasbinAuthorizer) Reload(ctx context.Context) error {
	roles, err := a.roles.ListRoles(ctx)
	if err != nil {
		return fmt.Errorf("authz: list roles: %w", err)
	}
	lines := make([][]string, 0)
	for i := range roles {
		lines = append(lines, policyLines(&roles[i])...)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enforcer.ClearPolicy()
	if _, err := a.enforcer.AddPolicies(lines); err != nil {
		return fmt.Errorf("authz: load policies: %w", err)
	}
	return nil
}

// policyLines renders the role's permission set as `p, roleID, workspaceID,
// permission` lines, deduplicating while preserving order.
func policyLines(role *domain.Role) [][]string {
	seen := make(map[string]struct{}, len(role.Permissions))
	lines := make([][]string, 0, len(role.Permissions))
	for _, p := range role.Permissions {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		lines = append(lines, []string{role.ID, role.WorkspaceID, p})
	}
	return lines
}
