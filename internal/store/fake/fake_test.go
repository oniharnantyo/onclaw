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

func TestProviderStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws1 := &domain.Workspace{Slug: "ws-1", Name: "Workspace 1"}
	_ = s.Workspaces().Create(ctx, ws1)
	ws2 := &domain.Workspace{Slug: "ws-2", Name: "Workspace 2"}
	_ = s.Workspaces().Create(ctx, ws2)

	// 1. Create provider
	p1 := &domain.ProviderConfig{
		WorkspaceID:   ws1.ID,
		Type:          "openai",
		Name:          "OpenAI Prod",
		BaseURL:       "https://api.openai.com",
		KeyCiphertext: "v1:nonce1:cipher1",
		KeyHint:       "1234",
		Enabled:       true,
	}
	if err := s.Providers().Create(ctx, p1); err != nil {
		t.Fatalf("unexpected create provider error: %v", err)
	}
	if p1.ID == "" {
		t.Fatal("expected provider ID to be generated")
	}
	if p1.CreatedAt.IsZero() || p1.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	// 2. Validation on create
	if err := s.Providers().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil, got %v", err)
	}
	if err := s.Providers().Create(ctx, &domain.ProviderConfig{WorkspaceID: "", Type: "openai", Name: "No WS"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing workspace_id, got %v", err)
	}
	if err := s.Providers().Create(ctx, &domain.ProviderConfig{WorkspaceID: ws1.ID, Type: "", Name: "No Type"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing type, got %v", err)
	}
	if err := s.Providers().Create(ctx, &domain.ProviderConfig{WorkspaceID: ws1.ID, Type: "openai", Name: ""}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing name, got %v", err)
	}

	// Duplicate ID rejected
	pDup := &domain.ProviderConfig{
		ID:          p1.ID,
		WorkspaceID: ws1.ID,
		Type:        "openai",
		Name:        "Duplicate ID",
	}
	if err := s.Providers().Create(ctx, pDup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate provider ID, got %v", err)
	}

	// 3. ByID lookup
	found, err := s.Providers().ByID(ctx, ws1.ID, p1.ID)
	if err != nil {
		t.Fatalf("unexpected ByID error: %v", err)
	}
	if found.ID != p1.ID || found.Name != "OpenAI Prod" || found.KeyCiphertext != "v1:nonce1:cipher1" || found.KeyHint != "1234" {
		t.Fatalf("unexpected provider found: %+v", found)
	}

	// ByID cross-tenant lookup returns ErrNotFound
	_, err = s.Providers().ByID(ctx, ws2.ID, p1.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant ByID, got %v", err)
	}

	// ByID empty / nonexistent
	_, err = s.Providers().ByID(ctx, "", p1.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty workspaceID, got %v", err)
	}
	_, err = s.Providers().ByID(ctx, ws1.ID, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent ID, got %v", err)
	}

	// 4. ListForWorkspace
	p2 := &domain.ProviderConfig{
		WorkspaceID: ws1.ID,
		Type:        "openai",
		Name:        "OpenAI Sandbox",
		Enabled:     true,
	}
	_ = s.Providers().Create(ctx, p2)

	p3 := &domain.ProviderConfig{
		WorkspaceID: ws2.ID,
		Type:        "anthropic",
		Name:        "Anthropic Main",
		Enabled:     true,
	}
	_ = s.Providers().Create(ctx, p3)

	listWS1, err := s.Providers().ListForWorkspace(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(listWS1) != 2 {
		t.Fatalf("expected 2 providers for ws1, got %d", len(listWS1))
	}

	listWS2, err := s.Providers().ListForWorkspace(ctx, ws2.ID)
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(listWS2) != 1 {
		t.Fatalf("expected 1 provider for ws2, got %d", len(listWS2))
	}

	listEmpty, err := s.Providers().ListForWorkspace(ctx, "nonexistent-ws")
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(listEmpty) != 0 {
		t.Fatalf("expected 0 providers for nonexistent workspace, got %d", len(listEmpty))
	}

	// 5. Update
	p1.Name = "OpenAI Prod V2"
	p1.BaseURL = "https://custom.openai.proxy"
	p1.KeyCiphertext = "v1:nonce2:cipher2"
	p1.KeyHint = "5678"
	p1.Enabled = false
	if err := s.Providers().Update(ctx, p1); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}

	reloaded, err := s.Providers().ByID(ctx, ws1.ID, p1.ID)
	if err != nil {
		t.Fatalf("unexpected ByID after update: %v", err)
	}
	if reloaded.Name != "OpenAI Prod V2" || reloaded.BaseURL != "https://custom.openai.proxy" ||
		reloaded.KeyCiphertext != "v1:nonce2:cipher2" || reloaded.KeyHint != "5678" || reloaded.Enabled {
		t.Fatalf("unexpected provider after update: %+v", reloaded)
	}

	// Update validation
	if err := s.Providers().Update(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil update, got %v", err)
	}
	if err := s.Providers().Update(ctx, &domain.ProviderConfig{ID: "", WorkspaceID: ws1.ID}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for update without ID, got %v", err)
	}
	if err := s.Providers().Update(ctx, &domain.ProviderConfig{ID: p1.ID, WorkspaceID: ""}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for update without WorkspaceID, got %v", err)
	}
	// Update cross-tenant
	if err := s.Providers().Update(ctx, &domain.ProviderConfig{ID: p1.ID, WorkspaceID: ws2.ID, Name: "Hack"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant update, got %v", err)
	}

	// 6. Delete
	// Cross-tenant delete returns ErrNotFound
	if err := s.Providers().Delete(ctx, ws2.ID, p1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant delete, got %v", err)
	}
	// Correct delete
	if err := s.Providers().Delete(ctx, ws1.ID, p1.ID); err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}
	// Subsequent ByID returns ErrNotFound
	if _, err := s.Providers().ByID(ctx, ws1.ID, p1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	// Subsequent Delete returns ErrNotFound
	if err := s.Providers().Delete(ctx, ws1.ID, p1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for repeated delete, got %v", err)
	}
}

func TestWithTx_ProviderStore(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws := &domain.Workspace{Slug: "tx-prov-ws", Name: "Tx Providers WS"}
	_ = s.Workspaces().Create(ctx, ws)

	// Commit test
	err := s.WithTx(ctx, func(txStore store.Store) error {
		p := &domain.ProviderConfig{
			WorkspaceID:   ws.ID,
			Type:          "openai",
			Name:          "Committed Provider",
			KeyCiphertext: "v1:nonce:key",
			KeyHint:       "9999",
			Enabled:       true,
		}
		return txStore.Providers().Create(ctx, p)
	})
	if err != nil {
		t.Fatalf("unexpected WithTx error: %v", err)
	}

	list, err := s.Providers().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 provider after commit, got %d (err: %v)", len(list), err)
	}
	if list[0].Name != "Committed Provider" {
		t.Fatalf("unexpected provider: %+v", list[0])
	}

	// Rollback test
	rollbackErr := errors.New("rollback this tx")
	err = s.WithTx(ctx, func(txStore store.Store) error {
		p := &domain.ProviderConfig{
			WorkspaceID: ws.ID,
			Type:        "anthropic",
			Name:        "Rolled Back Provider",
			Enabled:     true,
		}
		if err := txStore.Providers().Create(ctx, p); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}

	listAfterRollback, err := s.Providers().ListForWorkspace(ctx, ws.ID)
	if err != nil || len(listAfterRollback) != 1 {
		t.Fatalf("expected still 1 provider after rollback, got %d (err: %v)", len(listAfterRollback), err)
	}
}
