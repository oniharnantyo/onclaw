package fake_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestUserStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	// 1. Create user
	u := &domain.User{
		Email: "Alice@Example.com  ",
		Name:  "Alice",
	}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("unexpected create error: %v", err)
	}
	if u.ID == "" {
		t.Fatal("expected user ID to be assigned")
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("expected email to be normalized, got %q", u.Email)
	}
	if u.CreatedAt.IsZero() || u.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	// 2. Duplicate email rejected
	err := s.Users().Create(ctx, &domain.User{
		Email: "alice@example.com",
		Name:  "Alice Dup",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate email, got %v", err)
	}

	// 3. Invalid email rejected
	err = s.Users().Create(ctx, &domain.User{
		Email: "invalid-email",
		Name:  "Bad",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad email, got %v", err)
	}

	// 4. ByEmail lookup
	byEmail, err := s.Users().ByEmail(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatalf("unexpected ByEmail error: %v", err)
	}
	if byEmail.ID != u.ID || byEmail.Name != "Alice" {
		t.Fatalf("unexpected user retrieved: %+v", byEmail)
	}

	// 5. ByID lookup
	byID, err := s.Users().ByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("unexpected ByID error: %v", err)
	}
	if byID.Email != "alice@example.com" {
		t.Fatalf("unexpected email: %q", byID.Email)
	}

	// 6. SetPasswordHash
	if err := s.Users().SetPasswordHash(ctx, u.ID, "argon2id$hashed"); err != nil {
		t.Fatalf("unexpected SetPasswordHash error: %v", err)
	}
	updatedUser, _ := s.Users().ByID(ctx, u.ID)
	if updatedUser.PasswordHash == nil || *updatedUser.PasswordHash != "argon2id$hashed" {
		t.Fatalf("expected password hash to be set")
	}

	// 7. SetDisabled
	now := time.Now().UTC()
	if err := s.Users().SetDisabled(ctx, u.ID, &now); err != nil {
		t.Fatalf("unexpected SetDisabled error: %v", err)
	}
	disabledUser, _ := s.Users().ByID(ctx, u.ID)
	if !disabledUser.IsDisabled() {
		t.Fatal("expected user to be disabled")
	}

	// Enable again
	if err := s.Users().SetDisabled(ctx, u.ID, nil); err != nil {
		t.Fatalf("unexpected SetDisabled(nil) error: %v", err)
	}
	enabledUser, _ := s.Users().ByID(ctx, u.ID)
	if enabledUser.IsDisabled() {
		t.Fatal("expected user to be enabled")
	}

	// 8. Update
	avatarKey := "avatars/12345"
	enabledUser.Name = "Alice In Wonderland"
	enabledUser.AvatarKey = &avatarKey
	if err := s.Users().Update(ctx, enabledUser); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}
	reloaded, _ := s.Users().ByID(ctx, u.ID)
	if reloaded.Name != "Alice In Wonderland" || reloaded.AvatarKey == nil || *reloaded.AvatarKey != avatarKey {
		t.Fatalf("unexpected user after update: %+v", reloaded)
	}

	// 9. List
	u2 := &domain.User{Email: "bob@example.com", Name: "Bob"}
	_ = s.Users().Create(ctx, u2)
	list, err := s.Users().List(ctx)
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 users, got %d", len(list))
	}
}

func TestWorkspaceStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	// 1. Create workspace
	ws := &domain.Workspace{
		Slug: "acme-corp",
		Name: "Acme Corporation",
	}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected Create error: %v", err)
	}
	if ws.ID == "" {
		t.Fatal("expected workspace ID")
	}
	if ws.Timezone != "UTC" {
		t.Fatalf("expected default UTC timezone, got %q", ws.Timezone)
	}

	// 2. Duplicate slug rejected
	err := s.Workspaces().Create(ctx, &domain.Workspace{
		Slug: "acme-corp",
		Name: "Acme 2",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate slug, got %v", err)
	}

	// 3. Reserved slug rejected (for non-master)
	err = s.Workspaces().Create(ctx, &domain.Workspace{
		Slug: "master",
		Name: "Master Fake",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid on reserved slug, got %v", err)
	}

	// 4. Master workspace creation allowed with IsMaster=true
	masterWS := &domain.Workspace{
		Slug:     domain.MasterWorkspaceSlug,
		Name:     "Master Tenant",
		IsMaster: true,
	}
	if err := s.Workspaces().Create(ctx, masterWS); err != nil {
		t.Fatalf("expected master workspace creation to succeed, got %v", err)
	}

	// 5. BySlug lookup
	bySlug, err := s.Workspaces().BySlug(ctx, "acme-corp")
	if err != nil {
		t.Fatalf("unexpected BySlug error: %v", err)
	}
	if bySlug.ID != ws.ID || bySlug.Name != "Acme Corporation" {
		t.Fatalf("unexpected workspace: %+v", bySlug)
	}

	// 6. Update
	bySlug.Name = "Acme Corp International"
	bySlug.Timezone = "America/New_York"
	if err := s.Workspaces().Update(ctx, bySlug); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}
	reloaded, _ := s.Workspaces().ByID(ctx, ws.ID)
	if reloaded.Name != "Acme Corp International" || reloaded.Timezone != "America/New_York" {
		t.Fatalf("unexpected updated workspace: %+v", reloaded)
	}

	// 7. ListAll
	all, err := s.Workspaces().ListAll(ctx)
	if err != nil {
		t.Fatalf("unexpected ListAll error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(all))
	}
}

func TestRoleAndMemberStore(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	// Seed user and workspace
	u := &domain.User{Email: "charlie@example.com", Name: "Charlie"}
	_ = s.Users().Create(ctx, u)

	ws := &domain.Workspace{Slug: "startup", Name: "Startup Inc"}
	_ = s.Workspaces().Create(ctx, ws)

	// 1. Create Roles
	ownerRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        domain.RoleOwner,
		IsOwner:     true,
		Permissions: domain.OwnerPermissions,
		BuiltIn:     true,
	}
	if err := s.Roles().Create(ctx, ownerRole); err != nil {
		t.Fatalf("unexpected Create role error: %v", err)
	}

	adminRole := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        domain.RoleAdmin,
		IsOwner:     false,
		Permissions: domain.AdminPermissions,
		BuiltIn:     true,
	}
	if err := s.Roles().Create(ctx, adminRole); err != nil {
		t.Fatalf("unexpected Create role error: %v", err)
	}

	// Duplicate role name in same workspace
	err := s.Roles().Create(ctx, &domain.Role{
		WorkspaceID: ws.ID,
		Name:        domain.RoleOwner,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate role name, got %v", err)
	}

	// Role ByID and FindByName
	foundRole, err := s.Roles().FindByName(ctx, ws.ID, domain.RoleOwner)
	if err != nil || foundRole.ID != ownerRole.ID {
		t.Fatalf("unexpected FindByName result: %+v, err: %v", foundRole, err)
	}

	rolesList, err := s.Roles().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(rolesList) != 2 {
		t.Fatalf("expected 2 roles, got %d, err: %v", len(rolesList), err)
	}

	// 2. MemberStore Add
	m := &domain.Member{
		WorkspaceID: ws.ID,
		UserID:      u.ID,
		RoleID:      ownerRole.ID,
	}
	if err := s.Members().Add(ctx, m); err != nil {
		t.Fatalf("unexpected Add member error: %v", err)
	}

	// Duplicate member add
	err = s.Members().Add(ctx, &domain.Member{
		WorkspaceID: ws.ID,
		UserID:      u.ID,
		RoleID:      adminRole.ID,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate member, got %v", err)
	}

	// CountMembers
	count, err := s.Roles().CountMembers(ctx, ownerRole.ID)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 owner member, got %d, err: %v", count, err)
	}

	// Member Get with Role joined
	gotMember, err := s.Members().Get(ctx, ws.ID, u.ID)
	if err != nil {
		t.Fatalf("unexpected Get member error: %v", err)
	}
	if gotMember.Role == nil || gotMember.Role.Name != domain.RoleOwner {
		t.Fatalf("expected joined role Owner, got %+v", gotMember.Role)
	}

	// Member ListForWorkspace
	wsMembers, err := s.Members().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(wsMembers) != 1 {
		t.Fatalf("expected 1 member in workspace, got %d, err: %v", len(wsMembers), err)
	}
	if wsMembers[0].Role == nil || wsMembers[0].Role.Name != domain.RoleOwner {
		t.Fatalf("expected joined role in ListForWorkspace")
	}

	// Member ListForUser (MemberView)
	userViews, err := s.Members().ListForUser(ctx, u.ID)
	if err != nil || len(userViews) != 1 {
		t.Fatalf("expected 1 member view, got %d, err: %v", len(userViews), err)
	}
	if userViews[0].WorkspaceSlug != ws.Slug || userViews[0].Email != u.Email || userViews[0].RoleName != domain.RoleOwner {
		t.Fatalf("unexpected member view: %+v", userViews[0])
	}

	// UpdateRole
	if err := s.Members().UpdateRole(ctx, ws.ID, u.ID, adminRole.ID); err != nil {
		t.Fatalf("unexpected UpdateRole error: %v", err)
	}
	gotMemberAfterUpdate, _ := s.Members().Get(ctx, ws.ID, u.ID)
	if gotMemberAfterUpdate.RoleID != adminRole.ID || gotMemberAfterUpdate.Role.Name != domain.RoleAdmin {
		t.Fatalf("expected role Admin after update, got %+v", gotMemberAfterUpdate.Role)
	}

	// Remove member
	if err := s.Members().Remove(ctx, ws.ID, u.ID); err != nil {
		t.Fatalf("unexpected Remove member error: %v", err)
	}
	_, err = s.Members().Get(ctx, ws.ID, u.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Remove, got %v", err)
	}
}

func TestWithTx_Commit(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	err := s.WithTx(ctx, func(txStore store.Store) error {
		u := &domain.User{Email: "tx-commit@example.com", Name: "Tx User"}
		if err := txStore.Users().Create(ctx, u); err != nil {
			return err
		}
		ws := &domain.Workspace{Slug: "tx-ws", Name: "Tx Workspace"}
		if err := txStore.Workspaces().Create(ctx, ws); err != nil {
			return err
		}
		r := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        domain.RoleOwner,
			IsOwner:     true,
			Permissions: domain.OwnerPermissions,
			BuiltIn:     true,
		}
		if err := txStore.Roles().Create(ctx, r); err != nil {
			return err
		}
		m := &domain.Member{WorkspaceID: ws.ID, UserID: u.ID, RoleID: r.ID}
		return txStore.Members().Add(ctx, m)
	})
	if err != nil {
		t.Fatalf("unexpected WithTx error: %v", err)
	}

	// Verify all mutations persisted to parent store
	u, err := s.Users().ByEmail(ctx, "tx-commit@example.com")
	if err != nil || u == nil {
		t.Fatalf("expected user to exist after commit, err: %v", err)
	}

	ws, err := s.Workspaces().BySlug(ctx, "tx-ws")
	if err != nil || ws == nil {
		t.Fatalf("expected workspace to exist after commit, err: %v", err)
	}

	m, err := s.Members().Get(ctx, ws.ID, u.ID)
	if err != nil || m == nil || m.Role == nil {
		t.Fatalf("expected membership to exist with role after commit, err: %v", err)
	}
}

func TestWithTx_Rollback(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	// Initial user
	u0 := &domain.User{Email: "existing@example.com", Name: "Existing"}
	_ = s.Users().Create(ctx, u0)

	expectedErr := errors.New("something went wrong")

	err := s.WithTx(ctx, func(txStore store.Store) error {
		u := &domain.User{Email: "rollback@example.com", Name: "Rollback User"}
		if err := txStore.Users().Create(ctx, u); err != nil {
			return err
		}
		ws := &domain.Workspace{Slug: "rollback-ws", Name: "Rollback Workspace"}
		if err := txStore.Workspaces().Create(ctx, ws); err != nil {
			return err
		}

		// Mutate existing user inside tx
		if err := txStore.Users().SetDisabled(ctx, u0.ID, &u0.CreatedAt); err != nil {
			return err
		}

		// Fail the transaction
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

	// Verify rollback - none of the tx changes should exist in parent store
	_, err = s.Users().ByEmail(ctx, "rollback@example.com")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for rolled back user, got %v", err)
	}

	_, err = s.Workspaces().BySlug(ctx, "rollback-ws")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for rolled back workspace, got %v", err)
	}

	// Existing user should NOT be disabled in parent store
	reloadedU0, _ := s.Users().ByID(ctx, u0.ID)
	if reloadedU0.IsDisabled() {
		t.Fatal("expected existing user not to be modified after rollback")
	}
}

func TestWithTx_Nested(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	err := s.WithTx(ctx, func(outerTx store.Store) error {
		u1 := &domain.User{Email: "outer@example.com", Name: "Outer"}
		if err := outerTx.Users().Create(ctx, u1); err != nil {
			return err
		}

		// Nested tx that succeeds
		innerErr := outerTx.WithTx(ctx, func(innerTx store.Store) error {
			u2 := &domain.User{Email: "inner@example.com", Name: "Inner"}
			return innerTx.Users().Create(ctx, u2)
		})
		if innerErr != nil {
			return innerErr
		}

		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both should be committed
	if _, err := s.Users().ByEmail(ctx, "outer@example.com"); err != nil {
		t.Fatalf("expected outer user to exist: %v", err)
	}
	if _, err := s.Users().ByEmail(ctx, "inner@example.com"); err != nil {
		t.Fatalf("expected inner user to exist: %v", err)
	}
}
