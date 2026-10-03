package handlers_test

// Test-only adapter wiring the fake authorizer (authz.Fake) from the fake
// store: the authorizer boots from every role row (authz.RoleSource) while
// the store's RoleStore lists per workspace, so the adapter walks the
// workspace list — the same shape the composition root builds at boot
// (fix-role-permission-audit D1).

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/authz"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// allRolesSource adapts store.Store to authz.RoleSource.
type allRolesSource struct {
	st store.Store
}

func (s allRolesSource) ListRoles(ctx context.Context) ([]domain.Role, error) {
	wss, err := s.st.Workspaces().ListAll(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.Role
	for _, ws := range wss {
		rs, err := s.st.Roles().ListForWorkspace(ctx, ws.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}

// newTestAuthorizer builds the fake authorizer over the store's role rows.
// Handler tests evaluate permissions through the same port the router does.
func newTestAuthorizer(ctx context.Context, st store.Store) (*authz.Fake, error) {
	return authz.NewFake(ctx, allRolesSource{st: st})
}

// mustTestAuthorizer is newTestAuthorizer for tests without a natural error
// path (constructor smoke tests).
func mustTestAuthorizer(t *testing.T, st store.Store) *authz.Fake {
	t.Helper()
	az, err := newTestAuthorizer(context.Background(), st)
	if err != nil {
		t.Fatalf("build test authorizer: %v", err)
	}
	return az
}
