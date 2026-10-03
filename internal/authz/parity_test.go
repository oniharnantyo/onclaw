package authz

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Every scenario below runs against both Authorizer implementations; the
// golden parity test derives BOTH sides from the domain package at test
// time (role sets from the built-in set variables, verdicts from
// domain.HasPermission), so the suite stays valid through catalog surgery
// (e.g. roles.write removal, channels.* additions) without edits.

type implFactory struct {
	name  string
	build func(ctx context.Context, src RoleSource) (Authorizer, error)
}

var factories = []implFactory{
	{name: "casbin", build: func(ctx context.Context, src RoleSource) (Authorizer, error) {
		return NewCasbin(ctx, src)
	}},
	{name: "fake", build: func(ctx context.Context, src RoleSource) (Authorizer, error) {
		return NewFake(ctx, src)
	}},
}

// stubSource is a mutable RoleSource for driving construction and Reload.
type stubSource struct {
	mu    sync.Mutex
	roles map[string]domain.Role
	fail  error
}

func newStubSource(roles ...domain.Role) *stubSource {
	s := &stubSource{roles: make(map[string]domain.Role, len(roles))}
	s.SetRoles(roles...)
	return s
}

func (s *stubSource) ListRoles(ctx context.Context) ([]domain.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	out := make([]domain.Role, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, r)
	}
	return out, nil
}

// SetRoles replaces or adds roles by ID.
func (s *stubSource) SetRoles(roles ...domain.Role) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range roles {
		s.roles[r.ID] = r
	}
}

func (s *stubSource) setFail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

// builtinRoles derives one role row per built-in domain permission set, all
// in workspace ws-1.
func builtinRoles() []domain.Role {
	sets := []struct {
		name string
		perm []string
	}{
		{domain.RoleOwner, domain.OwnerPermissions},
		{domain.RoleAdmin, domain.AdminPermissions},
		{domain.RoleMember, domain.MemberPermissions},
		{domain.RoleSuperadmin, domain.SuperadminPermissions},
	}
	roles := make([]domain.Role, 0, len(sets))
	for _, s := range sets {
		roles = append(roles, domain.Role{
			ID:          "role-" + strings.ToLower(s.name),
			WorkspaceID: "ws-1",
			Name:        s.name,
			Permissions: s.perm,
			BuiltIn:     true,
		})
	}
	return roles
}

// enforceWant asserts one verdict, requiring a nil error in all cases: an
// unsynced role or missing permission must be (false, nil), never an error.
func enforceWant(t *testing.T, a Authorizer, roleID, workspaceID, permission string, want bool) {
	t.Helper()
	got, err := a.Enforce(context.Background(), roleID, workspaceID, permission)
	if err != nil {
		t.Fatalf("Enforce(%q, %q, %q) returned error: %v", roleID, workspaceID, permission, err)
	}
	if got != want {
		t.Fatalf("Enforce(%q, %q, %q) = %t, want %t", roleID, workspaceID, permission, got, want)
	}
}

// TestGoldenParity syncs the four built-in roles' permission sets from the
// domain package, then for every role x every catalog permission asserts
// Enforce == domain.HasPermission. It also proves workspace isolation: the
// same roles synced in ws-1 must enforce nothing in ws-2.
func TestGoldenParity(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			roles := builtinRoles()
			src := newStubSource(roles...)
			auth, err := factory.build(ctx, src)
			if err != nil {
				t.Fatalf("build authorizer: %v", err)
			}

			for _, role := range roles {
				for _, perm := range domain.AllPermissions() {
					want := domain.HasPermission(role.Permissions, perm)
					enforceWant(t, auth, role.ID, role.WorkspaceID, perm, want)
					// Workspace isolation: policy lines are domain-scoped, so
					// a role from ws-1 grants nothing in ws-2 regardless of
					// the permission.
					enforceWant(t, auth, role.ID, "ws-2", perm, false)
				}
			}
		})
	}
}

// TestUnsyncedRoleDenies proves a role that was never synced enforces
// (false, nil) — absence of policy is a denial, not an internal failure.
func TestUnsyncedRoleDenies(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			auth, err := factory.build(ctx, newStubSource())
			if err != nil {
				t.Fatalf("build authorizer: %v", err)
			}
			perm := domain.AllPermissions()[0]
			enforceWant(t, auth, "role-never-synced", "ws-1", perm, false)
		})
	}
}

// TestSyncReplaces proves Sync's write-through semantics: re-syncing a role
// with a smaller set drops the permissions that left the set, and
// re-syncing with an empty set drops everything.
func TestSyncReplaces(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			src := newStubSource()
			auth, err := factory.build(ctx, src)
			if err != nil {
				t.Fatalf("build authorizer: %v", err)
			}

			full := domain.OwnerPermissions
			role := &domain.Role{ID: "role-r1", WorkspaceID: "ws-1", Name: domain.RoleOwner, Permissions: full}
			if err := auth.Sync(ctx, role); err != nil {
				t.Fatalf("Sync full set: %v", err)
			}
			for _, perm := range domain.AllPermissions() {
				enforceWant(t, auth, role.ID, role.WorkspaceID, perm, domain.HasPermission(full, perm))
			}

			smaller := full[:len(full)/2]
			role.Permissions = smaller
			if err := auth.Sync(ctx, role); err != nil {
				t.Fatalf("Sync smaller set: %v", err)
			}
			for _, perm := range domain.AllPermissions() {
				enforceWant(t, auth, role.ID, role.WorkspaceID, perm, domain.HasPermission(smaller, perm))
			}

			role.Permissions = nil
			if err := auth.Sync(ctx, role); err != nil {
				t.Fatalf("Sync empty set: %v", err)
			}
			for _, perm := range domain.AllPermissions() {
				enforceWant(t, auth, role.ID, role.WorkspaceID, perm, false)
			}
		})
	}
}

// TestReloadRereadsStore proves Reload drops and re-syncs every role from
// the role store, and that a store failure leaves the current policy
// serving.
func TestReloadRereadsStore(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			original := domain.Role{ID: "role-r2", WorkspaceID: "ws-1", Name: domain.RoleMember, Permissions: domain.MemberPermissions}
			src := newStubSource(original)
			auth, err := factory.build(ctx, src)
			if err != nil {
				t.Fatalf("build authorizer: %v", err)
			}
			for _, perm := range domain.AllPermissions() {
				enforceWant(t, auth, original.ID, original.WorkspaceID, perm, domain.HasPermission(domain.MemberPermissions, perm))
			}

			// The store's row changes; until Reload the enforcer keeps the
			// old verdicts, after Reload it reflects the new set.
			updated := original
			updated.Permissions = domain.AdminPermissions
			src.SetRoles(updated)

			boom := errors.New("store unavailable")
			src.setFail(boom)
			if err := auth.Reload(ctx); !errors.Is(err, boom) {
				t.Fatalf("Reload with failing store: err = %v, want %v", err, boom)
			}
			for _, perm := range domain.AllPermissions() {
				enforceWant(t, auth, original.ID, original.WorkspaceID, perm, domain.HasPermission(domain.MemberPermissions, perm))
			}

			src.setFail(nil)
			if err := auth.Reload(ctx); err != nil {
				t.Fatalf("Reload: %v", err)
			}
			for _, perm := range domain.AllPermissions() {
				enforceWant(t, auth, updated.ID, updated.WorkspaceID, perm, domain.HasPermission(domain.AdminPermissions, perm))
			}
		})
	}
}

// TestWildcardPatternGrants pins the keyMatch semantics both
// implementations must share: a stored "agents.*" policy line grants every
// "agents."-prefixed permission and nothing outside the prefix.
func TestWildcardPatternGrants(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			auth, err := factory.build(ctx, newStubSource())
			if err != nil {
				t.Fatalf("build authorizer: %v", err)
			}
			role := &domain.Role{ID: "role-wild", WorkspaceID: "ws-1", Permissions: []string{"agents.*"}}
			if err := auth.Sync(ctx, role); err != nil {
				t.Fatalf("Sync: %v", err)
			}
			enforceWant(t, auth, role.ID, "ws-1", "agents.read", true)
			enforceWant(t, auth, role.ID, "ws-1", "agents.write", true)
			enforceWant(t, auth, role.ID, "ws-1", "agents", false)
			enforceWant(t, auth, role.ID, "ws-1", "workspace.read", false)
			enforceWant(t, auth, role.ID, "ws-2", "agents.read", false)
		})
	}
}

// TestConcurrentEnforceAndSync exercises Enforce against Sync/Reload from
// multiple goroutines (run with -race in CI) — the enforcer must stay
// consistent and never surface torn verdicts or spurious errors.
func TestConcurrentEnforceAndSync(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			roles := builtinRoles()
			src := newStubSource(roles...)
			auth, err := factory.build(ctx, src)
			if err != nil {
				t.Fatalf("build authorizer: %v", err)
			}

			perms := domain.AllPermissions()
			var wg sync.WaitGroup
			const readers = 4
			stop := make(chan struct{})
			for i := 0; i < readers; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					for n := 0; ; n++ {
						select {
						case <-stop:
							return
						default:
						}
						role := roles[n%len(roles)]
						perm := perms[n%len(perms)]
						if _, err := auth.Enforce(ctx, role.ID, role.WorkspaceID, perm); err != nil {
							t.Errorf("Enforce error under concurrency: %v", err)
							return
						}
					}
				}(i)
			}

			for n := 0; n < 50; n++ {
				var set []string
				if n%2 == 0 {
					set = domain.AdminPermissions
				} else {
					set = domain.MemberPermissions
				}
				role := &domain.Role{ID: roles[0].ID, WorkspaceID: "ws-1", Permissions: set}
				if err := auth.Sync(ctx, role); err != nil {
					t.Fatalf("Sync under concurrency: %v", err)
				}
				if err := auth.Reload(ctx); err != nil {
					t.Fatalf("Reload under concurrency: %v", err)
				}
			}
			close(stop)
			wg.Wait()
		})
	}
}

// TestConstructorLoadsRoles proves the constructors' boot-time sync: roles
// present in the source at build time are enforceable without an explicit
// Sync call, keyed on the role's ID and workspace.
func TestConstructorLoadsRoles(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			ctx := context.Background()
			roles := builtinRoles()
			auth, err := factory.build(ctx, newStubSource(roles...))
			if err != nil {
				t.Fatalf("build authorizer: %v", err)
			}
			for _, role := range roles {
				for _, perm := range domain.AllPermissions() {
					enforceWant(t, auth, role.ID, role.WorkspaceID, perm, domain.HasPermission(role.Permissions, perm))
				}
			}
		})
	}
}

// TestFakeMatchesCasbinVerdicts cross-checks the two implementations
// directly: over every built-in role x every catalog permission both must
// return identical verdicts (already covered via the shared scenarios, but
// this documents the pairing contract explicitly).
func TestFakeMatchesCasbinVerdicts(t *testing.T) {
	ctx := context.Background()
	var impls []Authorizer
	for _, factory := range factories {
		auth, err := factory.build(ctx, newStubSource(builtinRoles()...))
		if err != nil {
			t.Fatalf("build %s: %v", factory.name, err)
		}
		impls = append(impls, auth)
	}
	for _, role := range builtinRoles() {
		for _, perm := range domain.AllPermissions() {
			want, err := impls[0].Enforce(ctx, role.ID, role.WorkspaceID, perm)
			if err != nil {
				t.Fatalf("casbin Enforce: %v", err)
			}
			got, err := impls[1].Enforce(ctx, role.ID, role.WorkspaceID, perm)
			if err != nil {
				t.Fatalf("fake Enforce: %v", err)
			}
			if got != want {
				t.Fatalf("fake and casbin disagree for %s x %s: fake=%t casbin=%t", role.ID, perm, got, want)
			}
			if want != domain.HasPermission(role.Permissions, perm) {
				t.Fatalf("both implementations drifted from domain.HasPermission for %s x %s", role.ID, perm)
			}
		}
	}
}
