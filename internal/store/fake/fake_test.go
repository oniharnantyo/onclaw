package fake_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
		Slug:        "acme-corp",
		Name:        "Acme Corporation",
		Description: "Acme main workspace",
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
	if ws.Description != "Acme main workspace" {
		t.Fatalf("expected description 'Acme main workspace', got %q", ws.Description)
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
	if bySlug.ID != ws.ID || bySlug.Name != "Acme Corporation" || bySlug.Description != "Acme main workspace" {
		t.Fatalf("unexpected workspace: %+v", bySlug)
	}

	// 6. Update
	bySlug.Name = "Acme Corp International"
	bySlug.Description = "Updated workspace description"
	bySlug.Timezone = "America/New_York"
	if err := s.Workspaces().Update(ctx, bySlug); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}
	reloaded, _ := s.Workspaces().ByID(ctx, ws.ID)
	if reloaded.Name != "Acme Corp International" || reloaded.Description != "Updated workspace description" || reloaded.Timezone != "America/New_York" {
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

func TestAgentStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws1 := &domain.Workspace{Slug: "ws-agent-1", Name: "Agent WS 1"}
	_ = s.Workspaces().Create(ctx, ws1)
	ws2 := &domain.Workspace{Slug: "ws-agent-2", Name: "Agent WS 2"}
	_ = s.Workspaces().Create(ctx, ws2)

	p1 := &domain.ProviderConfig{
		WorkspaceID: ws1.ID,
		Type:        "openai",
		Name:        "OpenAI",
		Enabled:     true,
	}
	_ = s.Providers().Create(ctx, p1)

	// 1. Create agent with defaults
	maxTok := 4096
	effort := "high"
	cw := 128000
	a1 := &domain.Agent{
		WorkspaceID:   ws1.ID,
		Slug:          "support-bot",
		Name:          "Support Bot",
		Role:          "customer-support",
		Description:   "Helps users with questions",
		Brief:         "Friendly support persona",
		ProviderID:    p1.ID,
		Model:         "gpt-4o",
		Temperature:   0.7,
		MaxTokens:     &maxTok,
		Effort:        &effort,
		ContextWindow: &cw,
		Tools:         []string{"search", "calculator"},
		EnabledMCPS:   []string{"github"},
		Avatar:        json.RawMessage(`{"shape":"circle","color":"blue"}`),
	}
	if err := s.Agents().Create(ctx, a1); err != nil {
		t.Fatalf("unexpected create agent error: %v", err)
	}
	if a1.ID == "" {
		t.Fatal("expected agent ID to be generated")
	}
	if a1.Autonomy != domain.AutonomyApproval {
		t.Fatalf("expected default autonomy approval, got %q", a1.Autonomy)
	}
	if a1.PromptsStatus != domain.PromptsStatusGenerating {
		t.Fatalf("expected default prompts_status generating, got %q", a1.PromptsStatus)
	}
	if a1.CreatedAt.IsZero() || a1.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	// 2. Validation on Create
	if err := s.Agents().Create(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for nil agent, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: "", Name: "A", Slug: "a", ProviderID: p1.ID, Model: "m"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing workspace_id, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "", Slug: "a", ProviderID: p1.ID, Model: "m"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for missing name, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "INVALID_SLUG", ProviderID: p1.ID, Model: "m"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for invalid slug, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "a", ProviderID: "missing-provider", Model: "m"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent provider, got %v", err)
	}
	badTok := 0
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "a2", ProviderID: p1.ID, Model: "m", MaxTokens: &badTok}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for max_tokens <= 0, got %v", err)
	}
	badCW := 0
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "a2b", ProviderID: p1.ID, Model: "m", ContextWindow: &badCW}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for context_window <= 0, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "a3", ProviderID: p1.ID, Model: "m", Temperature: 3.0}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for temp > 2.0, got %v", err)
	}
	if err := s.Agents().Create(ctx, &domain.Agent{WorkspaceID: ws1.ID, Name: "A", Slug: "a4", ProviderID: p1.ID, Model: "m", Autonomy: "unlimited"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad autonomy, got %v", err)
	}

	// 3. Duplicate slug in same workspace fails with ErrConflict
	err := s.Agents().Create(ctx, &domain.Agent{
		WorkspaceID: ws1.ID,
		Slug:        "support-bot",
		Name:        "Duplicate Slug Agent",
		ProviderID:  p1.ID,
		Model:       "gpt-4o",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate slug in workspace, got %v", err)
	}

	// 4. Same slug in different workspace succeeds
	p2 := &domain.ProviderConfig{
		WorkspaceID: ws2.ID,
		Type:        "openai",
		Name:        "OpenAI WS2",
		Enabled:     true,
	}
	_ = s.Providers().Create(ctx, p2)
	a2WS2 := &domain.Agent{
		WorkspaceID: ws2.ID,
		Slug:        "support-bot",
		Name:        "Support Bot WS2",
		ProviderID:  p2.ID,
		Model:       "gpt-4o",
	}
	if err := s.Agents().Create(ctx, a2WS2); err != nil {
		t.Fatalf("expected same slug in different workspace to succeed, got %v", err)
	}

	// 5. ByID lookup
	found, err := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if err != nil {
		t.Fatalf("unexpected ByID error: %v", err)
	}
	if found.ID != a1.ID || found.Slug != "support-bot" || found.Name != "Support Bot" || found.Autonomy != domain.AutonomyApproval {
		t.Fatalf("unexpected agent retrieved: %+v", found)
	}
	if found.ContextWindow == nil || *found.ContextWindow != 128000 {
		t.Fatalf("unexpected context_window: %+v", found.ContextWindow)
	}
	if len(found.Tools) != 2 || len(found.EnabledMCPS) != 1 {
		t.Fatalf("unexpected capabilities arrays: %+v", found)
	}

	// Cross-tenant ByID returns ErrNotFound
	_, err = s.Agents().ByID(ctx, ws2.ID, a1.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant ByID, got %v", err)
	}

	// 6. BySlug lookup
	foundSlug, err := s.Agents().BySlug(ctx, ws1.ID, "support-bot")
	if err != nil {
		t.Fatalf("unexpected BySlug error: %v", err)
	}
	if foundSlug.ID != a1.ID {
		t.Fatalf("expected ID %s, got %s", a1.ID, foundSlug.ID)
	}

	// Cross-tenant BySlug returns ErrNotFound
	_, err = s.Agents().BySlug(ctx, "nonexistent-ws", "support-bot")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown workspace slug, got %v", err)
	}

	// 7. ListForWorkspace
	list1, err := s.Agents().ListForWorkspace(ctx, ws1.ID)
	if err != nil {
		t.Fatalf("unexpected ListForWorkspace error: %v", err)
	}
	if len(list1) != 1 {
		t.Fatalf("expected 1 agent in ws1, got %d", len(list1))
	}

	// 8. CountByProvider
	cnt, err := s.Agents().CountByProvider(ctx, ws1.ID, p1.ID)
	if err != nil {
		t.Fatalf("unexpected CountByProvider error: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected provider count 1, got %d", cnt)
	}

	// 9. Update
	newCW := 200000
	found.Name = "Support Bot Pro"
	found.Role = "senior-support"
	found.Autonomy = domain.AutonomyFull
	found.ContextWindow = &newCW
	found.Tools = []string{"search", "calculator", "docs"}
	if err := s.Agents().Update(ctx, found); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}
	reloaded, _ := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if reloaded.Name != "Support Bot Pro" || reloaded.Autonomy != domain.AutonomyFull || len(reloaded.Tools) != 3 || reloaded.ContextWindow == nil || *reloaded.ContextWindow != 200000 {
		t.Fatalf("unexpected updated agent: %+v", reloaded)
	}

	// 10. SetPromptState
	if err := s.Agents().SetPromptState(ctx, ws1.ID, a1.ID, domain.PromptsStatusReady, nil); err != nil {
		t.Fatalf("unexpected SetPromptState error: %v", err)
	}
	readyAgent, _ := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if readyAgent.PromptsStatus != domain.PromptsStatusReady || readyAgent.PromptsError != nil {
		t.Fatalf("unexpected ready agent: %+v", readyAgent)
	}
	if readyAgent.Identity != "" || readyAgent.Soul != "" || readyAgent.Bootstrap != "" {
		t.Fatalf("expected store to persist no prompt content, got: %+v", readyAgent)
	}

	// 11. SweepGenerating
	aStuck := &domain.Agent{
		WorkspaceID: ws1.ID,
		Slug:        "stuck-bot",
		Name:        "Stuck Bot",
		ProviderID:  p1.ID,
		Model:       "gpt-4o",
	}
	_ = s.Agents().Create(ctx, aStuck)

	swept, err := s.Agents().SweepGenerating(ctx, "prompt generation interrupted — retry")
	if err != nil {
		t.Fatalf("unexpected SweepGenerating error: %v", err)
	}
	// aStuck and a2WS2 (both generating) should have been swept
	if swept < 1 {
		t.Fatalf("expected at least 1 agent swept, got %d", swept)
	}
	reloadedStuck, _ := s.Agents().ByID(ctx, ws1.ID, aStuck.ID)
	if reloadedStuck.PromptsStatus != domain.PromptsStatusFailed || reloadedStuck.PromptsError == nil || *reloadedStuck.PromptsError != "prompt generation interrupted — retry" {
		t.Fatalf("unexpected swept agent status: %+v", reloadedStuck)
	}
	// Ready agent was untouched
	reloadedReady, _ := s.Agents().ByID(ctx, ws1.ID, a1.ID)
	if reloadedReady.PromptsStatus != domain.PromptsStatusReady {
		t.Fatalf("ready agent should not be swept: %+v", reloadedReady)
	}

	// 12. Delete
	if err := s.Agents().Delete(ctx, ws1.ID, a1.ID); err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}
	if _, err := s.Agents().ByID(ctx, ws1.ID, a1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after agent delete, got %v", err)
	}
	if _, err := s.Agents().BySlug(ctx, ws1.ID, "support-bot"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for slug after delete, got %v", err)
	}
}

func TestAgentStore_ListOrdering(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws := &domain.Workspace{Slug: "ws-order", Name: "Order WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected create workspace: %v", err)
	}

	p := &domain.ProviderConfig{
		WorkspaceID: ws.ID,
		Type:        "openai",
		Name:        "OpenAI",
		Enabled:     true,
	}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("unexpected create provider: %v", err)
	}

	now := time.Now().UTC()
	aA := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "agent-a",
		Name:        "Agent A",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   now.Add(-2 * time.Minute),
	}
	if err := s.Agents().Create(ctx, aA); err != nil {
		t.Fatalf("create A: %v", err)
	}

	aB := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "agent-b",
		Name:        "Agent B",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   now.Add(-1 * time.Minute),
	}
	if err := s.Agents().Create(ctx, aB); err != nil {
		t.Fatalf("create B: %v", err)
	}

	aC := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "agent-c",
		Name:        "Agent C",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   now,
	}
	if err := s.Agents().Create(ctx, aC); err != nil {
		t.Fatalf("create C: %v", err)
	}

	list, err := s.Agents().ListForWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListForWorkspace: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(list))
	}
	if list[0].ID != aC.ID || list[1].ID != aB.ID || list[2].ID != aA.ID {
		t.Fatalf("expected newest-first ordering [C, B, A], got [%s, %s, %s]", list[0].Slug, list[1].Slug, list[2].Slug)
	}

	// Tiebreak by ID DESC when CreatedAt is identical
	tEqual := now.Add(1 * time.Hour)
	a1 := &domain.Agent{
		ID:          "11111111-1111-1111-1111-111111111111",
		WorkspaceID: ws.ID,
		Slug:        "agent-id-1",
		Name:        "Agent ID 1",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   tEqual,
	}
	if err := s.Agents().Create(ctx, a1); err != nil {
		t.Fatalf("create a1: %v", err)
	}
	a2 := &domain.Agent{
		ID:          "22222222-2222-2222-2222-222222222222",
		WorkspaceID: ws.ID,
		Slug:        "agent-id-2",
		Name:        "Agent ID 2",
		ProviderID:  p.ID,
		Model:       "gpt-4o",
		CreatedAt:   tEqual,
	}
	if err := s.Agents().Create(ctx, a2); err != nil {
		t.Fatalf("create a2: %v", err)
	}

	listWithTiebreak, err := s.Agents().ListForWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListForWorkspace with tiebreak: %v", err)
	}
	if len(listWithTiebreak) != 5 {
		t.Fatalf("expected 5 agents, got %d", len(listWithTiebreak))
	}
	// a2 has higher ID than a1, both newer than C, B, A
	if listWithTiebreak[0].ID != a2.ID || listWithTiebreak[1].ID != a1.ID {
		t.Fatalf("expected tiebreak [a2, a1], got [%s, %s]", listWithTiebreak[0].Slug, listWithTiebreak[1].Slug)
	}
}

// seedMemoryFixtures creates a workspace, user, and agent for memory tests,
// returning their IDs.
func seedMemoryFixtures(t *testing.T, s store.Store, wsSlug, userEmail, agentSlug string) (wsID, userID, agentID string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: wsSlug, Name: "Mem WS " + wsSlug}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	u := &domain.User{Email: userEmail, Name: "Mem User"}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: agentSlug, Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ws.ID, u.ID, a.ID
}

func TestMemoryStore_UserMemory(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws1, u1, _ := seedMemoryFixtures(t, s, "mem-user-ws1", "mem-u1@example.com", "atlas")
	ws2, u2, _ := seedMemoryFixtures(t, s, "mem-user-ws2", "mem-u2@example.com", "atlas")
	mem := s.Memories()

	// Get-absent returns (nil, nil), not a sentinel.
	got, err := mem.UserMemory(ctx, ws1, u1)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for absent user memory, got (%v, %v)", got, err)
	}

	// Upsert stores content.
	if err := mem.UpsertUserMemory(ctx, ws1, u1, "Prefers Python."); err != nil {
		t.Fatalf("unexpected UpsertUserMemory error: %v", err)
	}
	got, err = mem.UserMemory(ctx, ws1, u1)
	if err != nil || got == nil || got.Content != "Prefers Python." {
		t.Fatalf("expected stored content, got (%v, %v)", got, err)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}

	// Upsert REPLACES content (HTTP PUT semantics).
	if err := mem.UpsertUserMemory(ctx, ws1, u1, "Prefers Go."); err != nil {
		t.Fatalf("unexpected second UpsertUserMemory error: %v", err)
	}
	got, _ = mem.UserMemory(ctx, ws1, u1)
	if got.Content != "Prefers Go." {
		t.Fatalf("expected replaced content, got %q", got.Content)
	}

	// Scope isolation: another user and another workspace see nothing.
	got, _ = mem.UserMemory(ctx, ws1, u2)
	if got != nil {
		t.Fatalf("expected nil memory for other user, got %q", got.Content)
	}
	got, _ = mem.UserMemory(ctx, ws2, u1)
	if got != nil {
		t.Fatalf("expected nil memory for other workspace, got %q", got.Content)
	}

	// Append concatenates atomically.
	if err := mem.AppendUserMemory(ctx, ws1, u1, " Likes coffee."); err != nil {
		t.Fatalf("unexpected AppendUserMemory error: %v", err)
	}
	got, _ = mem.UserMemory(ctx, ws1, u1)
	if got.Content != "Prefers Go. Likes coffee." {
		t.Fatalf("expected concatenated content, got %q", got.Content)
	}

	// Cap rejection on Upsert.
	over := strings.Repeat("a", domain.MaxMemoryContentChars+1)
	if err := mem.UpsertUserMemory(ctx, ws1, u1, over); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap upsert, got %v", err)
	}

	// Cap rejection on Append (resulting size, not fragment size).
	if err := mem.AppendUserMemory(ctx, ws1, u1, strings.Repeat("b", domain.MaxMemoryContentChars)); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap append, got %v", err)
	}

	// Stored memory is unchanged after rejected writes.
	got, _ = mem.UserMemory(ctx, ws1, u1)
	if got.Content != "Prefers Go. Likes coffee." {
		t.Fatalf("expected content unchanged after rejected writes, got %q", got.Content)
	}
}

func TestMemoryStore_WorkspaceMemory(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws1, _, _ := seedMemoryFixtures(t, s, "mem-ws-ws1", "mem-ws1@example.com", "atlas")
	ws2, _, _ := seedMemoryFixtures(t, s, "mem-ws-ws2", "mem-ws2@example.com", "atlas")
	mem := s.Memories()

	// Get-absent returns (nil, nil).
	got, err := mem.WorkspaceMemory(ctx, ws1)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for absent workspace memory, got (%v, %v)", got, err)
	}

	if err := mem.UpsertWorkspaceMemory(ctx, ws1, "Deploy freeze on Fridays."); err != nil {
		t.Fatalf("unexpected UpsertWorkspaceMemory error: %v", err)
	}
	got, err = mem.WorkspaceMemory(ctx, ws1)
	if err != nil || got == nil || got.Content != "Deploy freeze on Fridays." {
		t.Fatalf("expected stored content, got (%v, %v)", got, err)
	}

	// Scope isolation between workspaces.
	got, _ = mem.WorkspaceMemory(ctx, ws2)
	if got != nil {
		t.Fatalf("expected nil memory for other workspace, got %q", got.Content)
	}

	// Append concatenates.
	if err := mem.AppendWorkspaceMemory(ctx, ws1, " On-call rota rotated weekly."); err != nil {
		t.Fatalf("unexpected AppendWorkspaceMemory error: %v", err)
	}
	got, _ = mem.WorkspaceMemory(ctx, ws1)
	if got.Content != "Deploy freeze on Fridays. On-call rota rotated weekly." {
		t.Fatalf("expected concatenated content, got %q", got.Content)
	}

	// Cap rejection on Upsert and Append.
	over := strings.Repeat("a", domain.MaxMemoryContentChars+1)
	if err := mem.UpsertWorkspaceMemory(ctx, ws1, over); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap upsert, got %v", err)
	}
	if err := mem.AppendWorkspaceMemory(ctx, ws1, over); !errors.Is(err, domain.ErrMemoryCapExceeded) {
		t.Fatalf("expected ErrMemoryCapExceeded on over-cap append, got %v", err)
	}

	// Workspace memory does not clobber other workspace fields.
	w, err := s.Workspaces().ByID(ctx, ws1)
	if err != nil {
		t.Fatalf("unexpected workspace read error: %v", err)
	}
	if w.Name != "Mem WS mem-ws-ws1" || w.Slug != "mem-ws-ws1" {
		t.Fatalf("expected workspace fields untouched, got name=%q slug=%q", w.Name, w.Slug)
	}
}

func TestWithTx_Memories(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws, u, _ := seedMemoryFixtures(t, s, "tx-mem-ws", "tx-mem@example.com", "atlas")

	// Commit test
	err := s.WithTx(ctx, func(txStore store.Store) error {
		if err := txStore.Memories().UpsertUserMemory(ctx, ws, u, "Tx user memory"); err != nil {
			return err
		}
		return txStore.Memories().AppendWorkspaceMemory(ctx, ws, "Tx workspace memory")
	})
	if err != nil {
		t.Fatalf("unexpected WithTx error: %v", err)
	}

	um, err := s.Memories().UserMemory(ctx, ws, u)
	if err != nil || um == nil || um.Content != "Tx user memory" {
		t.Fatalf("expected committed user memory, got (%v, %v)", um, err)
	}
	wm, err := s.Memories().WorkspaceMemory(ctx, ws)
	if err != nil || wm == nil || wm.Content != "Tx workspace memory" {
		t.Fatalf("expected committed workspace memory, got (%v, %v)", wm, err)
	}

	// Rollback test
	rollbackErr := errors.New("rollback transaction")
	err = s.WithTx(ctx, func(txStore store.Store) error {
		if err := txStore.Memories().AppendUserMemory(ctx, ws, u, " rolled back"); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}
	um, _ = s.Memories().UserMemory(ctx, ws, u)
	if um.Content != "Tx user memory" {
		t.Fatalf("expected rolled-back append to be discarded, got %q", um.Content)
	}
}

func TestWorkspaceSkillStore_CRUD(t *testing.T) {
	s := fake.New()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "skills-ws", Name: "Skills WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	other := &domain.Workspace{Slug: "skills-other", Name: "Other WS"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	skill := &domain.WorkspaceSkill{
		WorkspaceID:  ws.ID,
		Name:         "changelog-sweeper",
		Description:  "Sweeps changelogs",
		Version:      domain.DefaultSkillVersion,
		Source:       domain.SkillSourceAuthored,
		Enabled:      true,
		Dependencies: domain.SkillDependencies{Tools: []string{"execute"}},
	}
	if err := s.WorkspaceSkills().Create(ctx, skill); err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if skill.ID == "" || skill.CreatedAt.IsZero() {
		t.Fatalf("expected ID and created_at assigned: %+v", skill)
	}

	// Get + cross-tenant isolation.
	found, err := s.WorkspaceSkills().Get(ctx, ws.ID, skill.ID)
	if err != nil || found.Name != "changelog-sweeper" {
		t.Fatalf("get = %+v, err %v", found, err)
	}
	if _, err := s.WorkspaceSkills().Get(ctx, other.ID, skill.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign get should be ErrNotFound, got %v", err)
	}

	// Unique name per workspace.
	if err := s.WorkspaceSkills().Create(ctx, &domain.WorkspaceSkill{WorkspaceID: ws.ID, Name: "changelog-sweeper", Version: "0.1.0", Source: domain.SkillSourceUpload, Enabled: true}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate name should be ErrConflict, got %v", err)
	}
	if err := s.WorkspaceSkills().Create(ctx, &domain.WorkspaceSkill{WorkspaceID: other.ID, Name: "changelog-sweeper", Version: "0.1.0", Source: domain.SkillSourceGit, Enabled: false}); err != nil {
		t.Fatalf("same name in other workspace should succeed: %v", err)
	}

	// Validation.
	if err := s.WorkspaceSkills().Create(ctx, &domain.WorkspaceSkill{WorkspaceID: ws.ID, Name: "Not A Slug", Version: "0.1.0", Source: domain.SkillSourceAuthored, Enabled: true}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid name should be ErrInvalid, got %v", err)
	}
	if err := s.WorkspaceSkills().Create(ctx, &domain.WorkspaceSkill{WorkspaceID: "missing-ws", Name: "orphan", Version: "0.1.0", Source: domain.SkillSourceAuthored, Enabled: true}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown workspace should be ErrNotFound, got %v", err)
	}

	// Update: rename + dependency replacement.
	found.Description = "Updated"
	found.Version = "0.2.0"
	found.Name = "renamed-sweeper"
	found.Dependencies = domain.SkillDependencies{Binaries: []string{"pdftotext"}}
	if err := s.WorkspaceSkills().Update(ctx, found); err != nil {
		t.Fatalf("update skill: %v", err)
	}
	updated, err := s.WorkspaceSkills().GetByName(ctx, ws.ID, "renamed-sweeper")
	if err != nil || updated.Version != "0.2.0" || !updated.Enabled {
		t.Fatalf("get by name after update = %+v, err %v", updated, err)
	}
	if len(updated.Dependencies.Binaries) != 1 || len(updated.Dependencies.Tools) != 0 {
		t.Fatalf("dependencies not replaced: %+v", updated.Dependencies)
	}
	if _, err := s.WorkspaceSkills().GetByName(ctx, ws.ID, "changelog-sweeper"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("old name should be gone after rename, got %v", err)
	}

	// Enabled toggle round-trip via slug-based ListEnabled.
	names, err := s.WorkspaceSkills().ListEnabled(ctx, "skills-ws")
	if err != nil || len(names) != 1 || names[0] != "renamed-sweeper" {
		t.Fatalf("list enabled = %v, err %v", names, err)
	}
	if err := s.WorkspaceSkills().SetEnabled(ctx, ws.ID, updated.ID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if names, err = s.WorkspaceSkills().ListEnabled(ctx, "skills-ws"); err != nil || len(names) != 0 {
		t.Fatalf("disabled skill must not list, got %v err %v", names, err)
	}
	if _, err := s.WorkspaceSkills().ListEnabled(ctx, "no-such-slug"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown slug should be ErrNotFound, got %v", err)
	}

	// List is workspace-scoped.
	list, err := s.WorkspaceSkills().List(ctx, other.ID)
	if err != nil || len(list) != 1 || list[0].Enabled {
		t.Fatalf("other list = %+v, err %v", list, err)
	}

	// Delete.
	if err := s.WorkspaceSkills().Delete(ctx, ws.ID, updated.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.WorkspaceSkills().Delete(ctx, ws.ID, updated.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("double delete should be ErrNotFound, got %v", err)
	}
}

func TestWorkspaceMCPServerStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws := &domain.Workspace{Slug: "mcp-ws", Name: "MCP WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	other := &domain.Workspace{Slug: "mcp-other", Name: "Other WS"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}

	server := &domain.WorkspaceMCPServer{
		WorkspaceID: ws.ID,
		Name:        "GitHub",
		Enabled:     true,
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStdio,
			Command:   "npx",
			Args:      []string{"-y", "@modelcontextprotocol/server-github"},
			Env:       []domain.EnvRow{{Name: "GITHUB_TOKEN", Value: "secret"}},
		},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, server); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if server.ID == "" || server.CreatedAt.IsZero() || server.UpdatedAt.IsZero() {
		t.Fatalf("expected ID and timestamps assigned: %+v", server)
	}

	// Get + cross-tenant isolation.
	found, err := s.WorkspaceMCPServers().Get(ctx, ws.ID, server.ID)
	if err != nil || found.Name != "GitHub" || found.Command != "npx" {
		t.Fatalf("get = %+v, err %v", found, err)
	}
	if _, err := s.WorkspaceMCPServers().Get(ctx, other.ID, server.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign get should be ErrNotFound, got %v", err)
	}

	// Returned copies must not alias stored state.
	found.Env[0].Value = "mutated"
	again, _ := s.WorkspaceMCPServers().Get(ctx, ws.ID, server.ID)
	if again.Env[0].Value != "secret" {
		t.Fatalf("stored env aliased by caller: %+v", again.Env)
	}

	// Name uniqueness: exact, case-insensitive, and scoped to the workspace.
	dup := *server
	dup.ID = ""
	if err := s.WorkspaceMCPServers().Create(ctx, &dup); !errors.Is(err, domain.ErrMCPServerNameTaken) {
		t.Fatalf("duplicate name should be ErrMCPServerNameTaken, got %v", err)
	}
	cased := *server
	cased.ID = ""
	cased.Name = "GITHUB"
	if err := s.WorkspaceMCPServers().Create(ctx, &cased); !errors.Is(err, domain.ErrMCPServerNameTaken) {
		t.Fatalf("case-insensitive duplicate should be ErrMCPServerNameTaken, got %v", err)
	}
	if !errors.Is(domain.ErrMCPServerNameTaken, domain.ErrConflict) {
		t.Fatal("ErrMCPServerNameTaken must chain to ErrConflict")
	}
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		WorkspaceID: other.ID,
		Name:        "github",
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       "https://mcp.example.com",
		},
	}); err != nil {
		t.Fatalf("same name in other workspace should succeed: %v", err)
	}

	// Validation and unknown scope.
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		WorkspaceID:   ws.ID,
		Name:          "broken",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio},
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("stdio without command should be ErrInvalid, got %v", err)
	}
	if err := s.WorkspaceMCPServers().Create(ctx, &domain.WorkspaceMCPServer{
		WorkspaceID:   "missing-ws",
		Name:          "orphan",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown workspace should be ErrNotFound, got %v", err)
	}

	// SetStatus persists probe outcome; unknown id is ErrNotFound.
	if err := s.WorkspaceMCPServers().SetStatus(ctx, ws.ID, server.ID, domain.MCPStatusConnected, "", 24); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if err := s.WorkspaceMCPServers().SetStatus(ctx, other.ID, server.ID, domain.MCPStatusError, "boom", 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign set status should be ErrNotFound, got %v", err)
	}

	// Update replaces editable fields but keeps probe status.
	found.Name = "GitHub Enterprise"
	found.Enabled = false
	found.MCPConnection = domain.MCPConnection{
		Transport: domain.MCPTransportStreamableHTTP,
		URL:       "https://mcp.corp.example.com",
		Headers:   []domain.EnvRow{{Name: "Authorization", Value: "Bearer tok"}},
	}
	if err := s.WorkspaceMCPServers().Update(ctx, found); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := s.WorkspaceMCPServers().Get(ctx, ws.ID, server.ID)
	if updated.Name != "GitHub Enterprise" || updated.Enabled || updated.URL != "https://mcp.corp.example.com" || len(updated.Headers) != 1 {
		t.Fatalf("update not applied: %+v", updated)
	}
	if updated.Status != domain.MCPStatusConnected || updated.ToolCount != 24 || updated.StatusError != "" {
		t.Fatalf("update must preserve probe status: %+v", updated)
	}
	if updated.Command != "" || len(updated.Env) != 0 {
		t.Fatalf("old transport fields must be replaced: %+v", updated.MCPConnection)
	}

	// Rename onto another server's name in the same scope collides.
	helper := &domain.WorkspaceMCPServer{
		WorkspaceID:   ws.ID,
		Name:          "Registry Helper",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}
	if err := s.WorkspaceMCPServers().Create(ctx, helper); err != nil {
		t.Fatalf("create helper server: %v", err)
	}
	updated.Name = "registry helper"
	if err := s.WorkspaceMCPServers().Update(ctx, updated); !errors.Is(err, domain.ErrMCPServerNameTaken) {
		t.Fatalf("rename collision should be ErrMCPServerNameTaken, got %v", err)
	}

	// List is workspace-scoped.
	list, err := s.WorkspaceMCPServers().List(ctx, ws.ID)
	if err != nil || len(list) != 2 || list[0].ID != server.ID || list[1].ID != helper.ID {
		t.Fatalf("list = %+v, err %v", list, err)
	}

	// Delete + double delete.
	if err := s.WorkspaceMCPServers().Delete(ctx, ws.ID, server.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.WorkspaceMCPServers().Delete(ctx, ws.ID, server.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("double delete should be ErrNotFound, got %v", err)
	}
}

func TestAgentMCPServerStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws := &domain.Workspace{Slug: "agent-mcp-ws", Name: "Agent MCP WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	other := &domain.Workspace{Slug: "agent-mcp-other", Name: "Other WS"}
	if err := s.Workspaces().Create(ctx, other); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	agentA := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, agentA); err != nil {
		t.Fatalf("create agent A: %v", err)
	}
	agentB := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, agentB); err != nil {
		t.Fatalf("create agent B: %v", err)
	}

	server := &domain.AgentMCPServer{
		WorkspaceID: ws.ID,
		AgentID:     agentA.ID,
		Name:        "Private Fetch",
		Enabled:     true,
		MCPConnection: domain.MCPConnection{
			Transport: domain.MCPTransportStreamableHTTP,
			URL:       "https://internal.example.com/mcp",
			Headers:   []domain.EnvRow{{Name: "Authorization", Value: "Bearer tok"}},
		},
	}
	if err := s.AgentMCPServers().Create(ctx, server); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if server.ID == "" || server.CreatedAt.IsZero() {
		t.Fatalf("expected ID and timestamps assigned: %+v", server)
	}

	// Get + agent-scope isolation: another agent cannot see it.
	found, err := s.AgentMCPServers().Get(ctx, agentA.ID, server.ID)
	if err != nil || found.Name != "Private Fetch" {
		t.Fatalf("get = %+v, err %v", found, err)
	}
	if _, err := s.AgentMCPServers().Get(ctx, agentB.ID, server.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other agent's get should be ErrNotFound, got %v", err)
	}

	// Names are unique per agent: same name under another agent is fine;
	// a case-insensitive repeat under the same agent collides.
	if err := s.AgentMCPServers().Create(ctx, &domain.AgentMCPServer{
		WorkspaceID:   ws.ID,
		AgentID:       agentB.ID,
		Name:          "private fetch",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); err != nil {
		t.Fatalf("same name under other agent should succeed: %v", err)
	}
	if err := s.AgentMCPServers().Create(ctx, &domain.AgentMCPServer{
		WorkspaceID:   ws.ID,
		AgentID:       agentA.ID,
		Name:          "PRIVATE FETCH",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrMCPServerNameTaken) {
		t.Fatalf("case-insensitive duplicate should be ErrMCPServerNameTaken, got %v", err)
	}

	// Scope integrity: the agent must exist in the server's workspace.
	if err := s.AgentMCPServers().Create(ctx, &domain.AgentMCPServer{
		WorkspaceID:   other.ID,
		AgentID:       agentA.ID,
		Name:          "cross-ws",
		MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("agent in another workspace should be ErrNotFound, got %v", err)
	}

	// SetStatus then update: editable fields change, probe status persists.
	if err := s.AgentMCPServers().SetStatus(ctx, agentA.ID, server.ID, domain.MCPStatusError, "connection refused", 0); err != nil {
		t.Fatalf("set status: %v", err)
	}
	found.URL = "https://internal.example.com/mcp-v2"
	if err := s.AgentMCPServers().Update(ctx, found); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := s.AgentMCPServers().Get(ctx, agentA.ID, server.ID)
	if updated.URL != "https://internal.example.com/mcp-v2" {
		t.Fatalf("update not applied: %+v", updated)
	}
	if updated.Status != domain.MCPStatusError || updated.StatusError != "connection refused" || updated.ToolCount != 0 {
		t.Fatalf("update must preserve probe status: %+v", updated)
	}

	// List is agent-scoped.
	listA, err := s.AgentMCPServers().List(ctx, agentA.ID)
	if err != nil || len(listA) != 1 {
		t.Fatalf("agent A list = %+v, err %v", listA, err)
	}
	listB, err := s.AgentMCPServers().List(ctx, agentB.ID)
	if err != nil || len(listB) != 1 {
		t.Fatalf("agent B list = %+v, err %v", listB, err)
	}

	// Cascade: deleting agent A removes its private servers; agent B untouched.
	if err := s.Agents().Delete(ctx, ws.ID, agentA.ID); err != nil {
		t.Fatalf("delete agent A: %v", err)
	}
	if listA, err = s.AgentMCPServers().List(ctx, agentA.ID); err != nil || len(listA) != 0 {
		t.Fatalf("agent A private servers must die with the agent, got %+v err %v", listA, err)
	}
	if listB, err = s.AgentMCPServers().List(ctx, agentB.ID); err != nil || len(listB) != 1 {
		t.Fatalf("agent B private servers must survive, got %+v err %v", listB, err)
	}

	// Delete + double delete.
	if err := s.AgentMCPServers().Delete(ctx, agentB.ID, listB[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.AgentMCPServers().Delete(ctx, agentB.ID, listB[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("double delete should be ErrNotFound, got %v", err)
	}
}

func TestWithTx_MCPServers(t *testing.T) {
	ctx := context.Background()
	s := fake.New()

	ws := &domain.Workspace{Slug: "mcp-tx-ws", Name: "MCP Tx WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// Rollback discards MCP server writes.
	if err := s.WithTx(ctx, func(txStore store.Store) error {
		srv := &domain.WorkspaceMCPServer{
			WorkspaceID:   ws.ID,
			Name:          "Rollback",
			MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportStdio, Command: "npx"},
		}
		if err := txStore.WorkspaceMCPServers().Create(ctx, srv); err != nil {
			return err
		}
		return errors.New("force rollback")
	}); err == nil {
		t.Fatal("expected forced rollback error")
	}
	if list, err := s.WorkspaceMCPServers().List(ctx, ws.ID); err != nil || len(list) != 0 {
		t.Fatalf("rolled back server must not persist, got %+v err %v", list, err)
	}

	// Commit persists MCP server writes.
	if err := s.WithTx(ctx, func(txStore store.Store) error {
		srv := &domain.WorkspaceMCPServer{
			WorkspaceID:   ws.ID,
			Name:          "Committed",
			MCPConnection: domain.MCPConnection{Transport: domain.MCPTransportSSE, URL: "https://mcp.example.com/sse"},
		}
		return txStore.WorkspaceMCPServers().Create(ctx, srv)
	}); err != nil {
		t.Fatalf("unexpected WithTx error: %v", err)
	}
	list, err := s.WorkspaceMCPServers().List(ctx, ws.ID)
	if err != nil || len(list) != 1 || list[0].Name != "Committed" {
		t.Fatalf("committed server must persist, got %+v err %v", list, err)
	}
}

// seedMemoryScopes creates one workspace with two members and two agents so
// the visibility tiers (shared / user / agent) can be exercised for
// cross-member and cross-agent exclusion.
func seedMemoryScopes(t *testing.T, s store.Store) (wsID, userA, userB, agentX, agentY string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "mem-scopes-ws", Name: "Mem Scopes WS"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	uA := &domain.User{Email: "member-a@example.com", Name: "Member A"}
	if err := s.Users().Create(ctx, uA); err != nil {
		t.Fatalf("create user A: %v", err)
	}
	uB := &domain.User{Email: "member-b@example.com", Name: "Member B"}
	if err := s.Users().Create(ctx, uB); err != nil {
		t.Fatalf("create user B: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	aX := &domain.Agent{WorkspaceID: ws.ID, Slug: "atlas", Name: "Atlas", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, aX); err != nil {
		t.Fatalf("create agent X: %v", err)
	}
	aY := &domain.Agent{WorkspaceID: ws.ID, Slug: "beacon", Name: "Beacon", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, aY); err != nil {
		t.Fatalf("create agent Y: %v", err)
	}
	return ws.ID, uA.ID, uB.ID, aX.ID, aY.ID
}

// newMemoryNote builds a write-valid note owned per its tier (ownerID is the
// user or agent owner depending on visibility).
func newMemoryNote(wsID string, visibility domain.MemoryVisibility, ownerID, content string) *domain.MemoryNote {
	n := &domain.MemoryNote{
		WorkspaceID:   wsID,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		LearnedAt:     time.Date(2026, 9, 15, 10, 0, 1, 0, time.UTC),
		SourceEventID: "evt_test_1",
		Content:       content,
	}
	switch visibility {
	case domain.MemoryVisibilityUser:
		n.UserID = &ownerID
	case domain.MemoryVisibilityAgent:
		n.AgentID = &ownerID
	}
	return n
}

// newMemoryEvent builds a write-valid gist event owned per its tier
// (ownerID is the user owner for user-visibility rows, else "").
func newMemoryEvent(wsID, agentID, sessionID, turnID string, visibility domain.MemoryVisibility, ownerID, description string) *domain.MemoryEvent {
	e := &domain.MemoryEvent{
		WorkspaceID:   wsID,
		AgentID:       agentID,
		SessionID:     sessionID,
		TurnID:        turnID,
		Visibility:    visibility,
		Origin:        domain.MemoryOriginDialogue,
		EventTime:     time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		LearnedAt:     time.Date(2026, 9, 15, 10, 0, 1, 0, time.UTC),
		SourceEventID: "evt_test_1",
		Description:   description,
	}
	if visibility == domain.MemoryVisibilityUser {
		e.UserID = &ownerID
	}
	return e
}

func TestMemoryNotes_VisibilityScoping(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws, userA, userB, agentX, agentY := seedMemoryScopes(t, s)

	// Seed one note per tier/owner combination.
	notes := s.MemoryNotes()
	shared := newMemoryNote(ws, domain.MemoryVisibilityShared, "", "Deploy freeze on Fridays.")
	if err := notes.InsertNote(ctx, shared, domain.MemoryVisibilityShared); err != nil {
		t.Fatalf("insert shared note: %v", err)
	}
	noteA := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Alice deploys the payments service.")
	if err := notes.InsertNote(ctx, noteA, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert user A note: %v", err)
	}
	noteB := newMemoryNote(ws, domain.MemoryVisibilityUser, userB, "Bob owns the on-call rota.")
	if err := notes.InsertNote(ctx, noteB, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert user B note: %v", err)
	}
	noteX := newMemoryNote(ws, domain.MemoryVisibilityAgent, agentX, "Atlas retries failed webhooks twice.")
	if err := notes.InsertNote(ctx, noteX, domain.MemoryVisibilityAgent); err != nil {
		t.Fatalf("insert agent X note: %v", err)
	}
	noteY := newMemoryNote(ws, domain.MemoryVisibilityAgent, agentY, "Beacon drafts incident timelines.")
	if err := notes.InsertNote(ctx, noteY, domain.MemoryVisibilityAgent); err != nil {
		t.Fatalf("insert agent Y note: %v", err)
	}

	// Viewer A served by X sees shared + own-user + serving-agent rows only.
	list, err := notes.ListNotesForUI(ctx, ws, userA, agentX, store.MemoryNoteFilters{})
	if err != nil {
		t.Fatalf("list for A/X: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 visible notes for A/X, got %d: %+v", len(list), list)
	}
	seen := map[string]bool{}
	for _, n := range list {
		seen[n.ID] = true
	}
	if !seen[shared.ID] || !seen[noteA.ID] || !seen[noteX.ID] {
		t.Fatalf("expected shared + own-user + serving-agent notes, got %+v", list)
	}

	// Cross-member exclusion: B served by X sees shared + own-user + agent X
	// (3) — noteA must be absent.
	list, _ = notes.ListNotesForUI(ctx, ws, userB, agentX, store.MemoryNoteFilters{})
	if len(list) != 3 {
		t.Fatalf("expected 3 visible notes for B/X, got %d", len(list))
	}
	for _, n := range list {
		if n.ID == noteA.ID {
			t.Fatalf("B must not see A's user-visibility note, got %+v", list)
		}
	}

	// Cross-agent exclusion: A served by Y sees shared + own-user + agent Y
	// (3) — noteX must be absent.
	list, _ = notes.ListNotesForUI(ctx, ws, userA, agentY, store.MemoryNoteFilters{})
	if len(list) != 3 {
		t.Fatalf("expected 3 visible notes for A/Y, got %d", len(list))
	}
	for _, n := range list {
		if n.ID == noteX.ID {
			t.Fatalf("A served by Y must not see agent X's note, got %+v", list)
		}
	}

	// Search applies the same structural filter: B searching for A's note
	// content finds nothing; A finds it.
	found, err := notes.SearchNotes(ctx, ws, userB, agentX, "payments service", store.MemoryNoteFilters{})
	if err != nil {
		t.Fatalf("search for B: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 search hits for B on A's note, got %d", len(found))
	}
	found, err = notes.SearchNotes(ctx, ws, userA, agentX, "payments service", store.MemoryNoteFilters{})
	if err != nil {
		t.Fatalf("search for A: %v", err)
	}
	if len(found) != 1 || found[0].ID != noteA.ID {
		t.Fatalf("expected A's note, got %+v", found)
	}

	// Direct fetch by ID is scope-filtered too: a foreign note is
	// indistinguishable from an absent one.
	got, err := notes.GetNote(ctx, ws, userA, agentX, noteB.ID)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for foreign note, got (%+v, %v)", got, err)
	}
	got, err = notes.GetNote(ctx, ws, userB, agentX, noteB.ID)
	if err != nil || got == nil || got.ID != noteB.ID {
		t.Fatalf("expected own note, got (%+v, %v)", got, err)
	}

	// Events obey the same rule.
	events := s.MemoryEvents()
	evShared := newMemoryEvent(ws, agentX, "sess_1", "turn_1", domain.MemoryVisibilityShared, "", "Channel triaged the outage.")
	if err := events.InsertEvent(ctx, evShared); err != nil {
		t.Fatalf("insert shared event: %v", err)
	}
	evA := newMemoryEvent(ws, agentX, "sess_1", "turn_2", domain.MemoryVisibilityUser, userA, "Alice walked through the migration.")
	if err := events.InsertEvent(ctx, evA); err != nil {
		t.Fatalf("insert user event: %v", err)
	}
	evX := newMemoryEvent(ws, agentX, "sess_1", "turn_3", domain.MemoryVisibilityAgent, "", "Atlas finished the sweep.")
	if err := events.InsertEvent(ctx, evX); err != nil {
		t.Fatalf("insert agent event: %v", err)
	}

	// B served by Y sees only the shared gist; A served by X sees all three.
	evList, err := events.ListEventsForUI(ctx, ws, userB, agentY, store.MemoryEventFilters{})
	if err != nil || len(evList) != 1 || evList[0].ID != evShared.ID {
		t.Fatalf("expected only the shared event for B/Y, got (%d, %v)", len(evList), err)
	}
	evList, err = events.ListEventsForUI(ctx, ws, userA, agentX, store.MemoryEventFilters{})
	if err != nil || len(evList) != 3 {
		t.Fatalf("expected 3 events for A/X, got (%d, %v)", len(evList), err)
	}
	evFound, err := events.SearchEvents(ctx, ws, userB, agentY, "migration", store.MemoryEventFilters{})
	if err != nil {
		t.Fatalf("event search for B: %v", err)
	}
	if len(evFound) != 0 {
		t.Fatalf("expected 0 event hits for B on A's gist, got %d", len(evFound))
	}

	// Workspace partition: ws1 caller identity scoped into ws2 sees nothing.
	ws2, _, _, _, _ := seedMemoryScopes2(t, s)
	list, _ = notes.ListNotesForUI(ctx, ws2, userA, agentX, store.MemoryNoteFilters{})
	if len(list) != 0 {
		t.Fatalf("expected no notes across the workspace partition, got %d", len(list))
	}
}

// seedMemoryScopes2 creates a second workspace for partition tests.
func seedMemoryScopes2(t *testing.T, s store.Store) (wsID, userA, userB, agentX, agentY string) {
	t.Helper()
	ctx := context.Background()

	ws := &domain.Workspace{Slug: "mem-scopes-ws-2", Name: "Mem Scopes WS 2"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create workspace 2: %v", err)
	}
	u := &domain.User{Email: "member-c@example.com", Name: "Member C"}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("create user C: %v", err)
	}
	p := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "P2", Enabled: true}
	if err := s.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider 2: %v", err)
	}
	a := &domain.Agent{WorkspaceID: ws.ID, Slug: "cobalt", Name: "Cobalt", ProviderID: p.ID, Model: "gpt-4o"}
	if err := s.Agents().Create(ctx, a); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ws.ID, u.ID, u.ID, a.ID, a.ID
}

func TestMemoryNotes_CeilingRejection(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws, userA, _, agentX, _ := seedMemoryScopes(t, s)
	notes := s.MemoryNotes()

	// The domain ordering: agent < user < shared. Widening beyond the
	// ceiling is rejected; equal or narrower proposals pass.
	if err := domain.ValidateMemoryVisibilityWithin(domain.MemoryVisibilityUser, domain.MemoryVisibilityShared); !errors.Is(err, domain.ErrMemoryVisibilityExceeded) {
		t.Fatalf("expected ErrMemoryVisibilityExceeded for shared over user, got %v", err)
	}
	if err := domain.ValidateMemoryVisibilityWithin(domain.MemoryVisibilityShared, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("expected user under shared ceiling to pass, got %v", err)
	}
	if err := domain.ValidateMemoryVisibilityWithin(domain.MemoryVisibilityUser, domain.MemoryVisibilityAgent); err != nil {
		t.Fatalf("expected agent under user ceiling to pass, got %v", err)
	}
	if err := domain.ValidateMemoryVisibilityWithin(domain.MemoryVisibilityUser, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("expected equal ceiling to pass, got %v", err)
	}

	// A DM-ceiling write proposing shared is rejected at the store boundary.
	note := newMemoryNote(ws, domain.MemoryVisibilityShared, "", "Share everything with everyone.")
	if err := notes.InsertNote(ctx, note, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryVisibilityExceeded) {
		t.Fatalf("expected ErrMemoryVisibilityExceeded inserting shared under user ceiling, got %v", err)
	}

	// Supersede proposals are ceiling-checked the same way.
	old := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Provider is Stripe.")
	if err := notes.InsertNote(ctx, old, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert old note: %v", err)
	}
	correction := newMemoryNote(ws, domain.MemoryVisibilityShared, "", "Provider is Midtrans, share it.")
	if err := notes.SupersedeNote(ctx, ws, old.ID, correction, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryVisibilityExceeded) {
		t.Fatalf("expected ErrMemoryVisibilityExceeded superseding with shared under user ceiling, got %v", err)
	}

	// Unknown tiers are invalid input, not ceiling violations.
	if err := domain.ValidateMemoryVisibilityWithin("bogus", domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown ceiling, got %v", err)
	}

	// The agent-tier pipeline can never birth user- or shared-visibility rows.
	agentNote := newMemoryNote(ws, domain.MemoryVisibilityAgent, agentX, "Scheduled sweep finished clean.")
	if err := notes.InsertNote(ctx, agentNote, domain.MemoryVisibilityAgent); err != nil {
		t.Fatalf("insert agent note: %v", err)
	}
	if err := notes.InsertNote(ctx, newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "From a scheduled run."), domain.MemoryVisibilityAgent); !errors.Is(err, domain.ErrMemoryVisibilityExceeded) {
		t.Fatalf("expected ErrMemoryVisibilityExceeded inserting user under agent ceiling, got %v", err)
	}
}

func TestMemoryNotes_SupersedeTombstonePromote(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws, userA, userB, _, _ := seedMemoryScopes(t, s)
	notes := s.MemoryNotes()

	// Birth a fact from a DM (user ceiling, user owner).
	old := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "The payment provider is Stripe.")
	if err := notes.InsertNote(ctx, old, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert old note: %v", err)
	}

	// Correction: new row supersedes, nothing is overwritten (D6).
	correction := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "The payment provider is Midtrans.")
	if err := notes.SupersedeNote(ctx, ws, old.ID, correction, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if correction.ID == "" || correction.Supersedes == nil || *correction.Supersedes != old.ID {
		t.Fatalf("expected correction linked to old note, got %+v", correction)
	}

	// Default list carries the current state only; history shows both.
	list, err := notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{})
	if err != nil || len(list) != 1 || list[0].ID != correction.ID {
		t.Fatalf("expected only the correction in the default list, got (%+v, %v)", list, err)
	}
	history, err := notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{History: true})
	if err != nil || len(history) != 2 {
		t.Fatalf("expected both rows in the history view, got (%d, %v)", len(history), err)
	}

	// The superseded row stays queryable as history by ID.
	got, err := notes.GetNote(ctx, ws, userA, "", old.ID)
	if err != nil || got == nil || got.SupersededBy == nil || *got.SupersededBy != correction.ID {
		t.Fatalf("expected superseded row retrievable with pointer, got (%+v, %v)", got, err)
	}

	// A superseded row cannot be superseded twice, nor promoted.
	second := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Second correction.")
	if err := notes.SupersedeNote(ctx, ws, old.ID, second, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict double-superseding, got %v", err)
	}
	if err := notes.PromoteNote(ctx, ws, old.ID, userA); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound promoting superseded note, got %v", err)
	}

	// Tombstoning hides the row from every read path; tombstoned is
	// indistinguishable from absent on re-delete.
	if err := notes.TombstoneNote(ctx, ws, correction.ID); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	got, err = notes.GetNote(ctx, ws, userA, "", correction.ID)
	if err != nil || got != nil {
		t.Fatalf("expected tombstoned note invisible to GetNote, got (%+v, %v)", got, err)
	}
	list, _ = notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{History: true})
	if len(list) != 1 || list[0].ID != old.ID {
		t.Fatalf("expected tombstoned note excluded even from history, got %+v", list)
	}
	found, _ := notes.SearchNotes(ctx, ws, userA, "", "Midtrans", store.MemoryNoteFilters{})
	if len(found) != 0 {
		t.Fatalf("expected tombstoned note excluded from search, got %d", len(found))
	}
	if err := notes.TombstoneNote(ctx, ws, correction.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-tombstoning, got %v", err)
	}

	// Promotion widens (the only human widening path) and is audited.
	promoted := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "The staging window is 02:00-04:00 UTC.")
	if err := notes.InsertNote(ctx, promoted, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert promoted note: %v", err)
	}
	if err := notes.PromoteNote(ctx, ws, promoted.ID, userA); err != nil {
		t.Fatalf("promote: %v", err)
	}
	got, err = notes.GetNote(ctx, ws, userB, "", promoted.ID)
	if err != nil || got == nil || got.Visibility != domain.MemoryVisibilityShared {
		t.Fatalf("expected promoted note visible to B as shared, got (%+v, %v)", got, err)
	}
	if got.UserID != nil || got.AgentID != nil || got.PromotedBy == nil || *got.PromotedBy != userA || got.PromotedAt == nil {
		t.Fatalf("expected owner cleared and promotion audited, got %+v", got)
	}
	if err := notes.PromoteNote(ctx, ws, promoted.ID, userA); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict promoting an already-shared note, got %v", err)
	}
	if err := notes.PromoteNote(ctx, ws, "00000000-0000-0000-0000-000000000000", userA); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound promoting unknown note, got %v", err)
	}

	// Workspace-wide counters reflect tombstones and the promotion.
	counts, err := notes.CountByVisibility(ctx, ws)
	if err != nil {
		t.Fatalf("count by visibility: %v", err)
	}
	// old is superseded but alive (history), promoted is shared, correction
	// is tombstoned — only tombstones leave the counters.
	if counts[domain.MemoryVisibilityShared] != 1 || counts[domain.MemoryVisibilityUser] != 1 {
		t.Fatalf("expected 1 shared and 1 user note, got %+v", counts)
	}

	// Dedupe candidate count (current notes only — superseded rows are dead
	// candidates): the fresh fact overlaps the stored one, an unrelated
	// proposal does not.
	current := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "The payment provider is Midtrans.")
	if err := notes.InsertNote(ctx, current, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert current note: %v", err)
	}
	similar, err := notes.CountSimilar(ctx, ws, userA, "", "The payment provider is Midtrans, launched yesterday.", 0.3)
	if err != nil {
		t.Fatalf("count similar: %v", err)
	}
	if similar != 1 {
		t.Fatalf("expected 1 similar candidate, got %d", similar)
	}
	// A proposal matching an already-stored current fact finds it — that is
	// the ADD-vs-SUPERSEDE decision input.
	similar, err = notes.CountSimilar(ctx, ws, userA, "", "The staging window is 02:00-04:00 UTC.", 0.3)
	if err != nil {
		t.Fatalf("count exact similar: %v", err)
	}
	if similar != 1 {
		t.Fatalf("expected 1 candidate for the already-stored fact, got %d", similar)
	}
}

func TestMemoryEvents_CursorCountsAndTombstone(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws, userA, _, agentX, _ := seedMemoryScopes(t, s)
	events := s.MemoryEvents()

	// The gister's incremental cursor: the newest non-tombstoned gist for
	// the session, (nil, nil) before any gist exists.
	latest, err := events.LatestEventForSession(ctx, ws, "sess_cursor")
	if err != nil || latest != nil {
		t.Fatalf("expected (nil, nil) for ungisted session, got (%+v, %v)", latest, err)
	}

	first := newMemoryEvent(ws, agentX, "sess_cursor", "turn_1", domain.MemoryVisibilityUser, userA, "Window one.")
	first.EventTime = time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	if err := events.InsertEvent(ctx, first); err != nil {
		t.Fatalf("insert first: %v", err)
	}
	second := newMemoryEvent(ws, agentX, "sess_cursor", "turn_2", domain.MemoryVisibilityUser, userA, "Window two.")
	second.EventTime = time.Date(2026, 9, 15, 9, 30, 0, 0, time.UTC)
	if err := events.InsertEvent(ctx, second); err != nil {
		t.Fatalf("insert second: %v", err)
	}

	latest, err = events.LatestEventForSession(ctx, ws, "sess_cursor")
	if err != nil || latest == nil || latest.ID != second.ID {
		t.Fatalf("expected the newest gist as cursor, got (%+v, %v)", latest, err)
	}

	// Per-turn counts back the chip's breakdown.
	counts, err := events.CountByVisibility(ctx, ws, "sess_cursor", "turn_2")
	if err != nil || counts[domain.MemoryVisibilityUser] != 1 {
		t.Fatalf("expected 1 user gist for turn_2, got (%+v, %v)", counts, err)
	}
	counts, err = events.CountByVisibility(ctx, ws, "sess_cursor", "turn_unknown")
	if err != nil || len(counts) != 0 {
		t.Fatalf("expected empty counts for unknown turn, got (%+v, %v)", counts, err)
	}

	// Tombstoning the latest gist makes the previous one the cursor again.
	if err := events.TombstoneEvent(ctx, ws, second.ID); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	latest, err = events.LatestEventForSession(ctx, ws, "sess_cursor")
	if err != nil || latest == nil || latest.ID != first.ID {
		t.Fatalf("expected first gist after tombstoning second, got (%+v, %v)", latest, err)
	}
	list, _ := events.ListEventsForUI(ctx, ws, userA, agentX, store.MemoryEventFilters{})
	if len(list) != 1 {
		t.Fatalf("expected tombstoned event hidden from list, got %d", len(list))
	}
	if err := events.TombstoneEvent(ctx, ws, second.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-tombstoning, got %v", err)
	}

	// Empty search queries are invalid input on both stores.
	if _, err := events.SearchEvents(ctx, ws, userA, agentX, "  ", store.MemoryEventFilters{}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty event query, got %v", err)
	}
	if _, err := s.MemoryNotes().SearchNotes(ctx, ws, userA, agentX, "", store.MemoryNoteFilters{}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for empty note query, got %v", err)
	}

	// Topic and time-window filters narrow the note listing.
	notes := s.MemoryNotes()
	topic := "deployments"
	n := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Staging redeploys nightly.")
	n.Topic = &topic
	n.EventTime = time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	if err := notes.InsertNote(ctx, n, domain.MemoryVisibilityUser); err != nil {
		t.Fatalf("insert topic note: %v", err)
	}
	noteList, err := notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{Topic: "deployments"})
	if err != nil || len(noteList) != 1 {
		t.Fatalf("expected 1 topic-filtered note, got (%d, %v)", len(noteList), err)
	}
	noteList, err = notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{
		TimeWindow: &store.MemoryTimeWindow{From: time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)},
	})
	if err != nil || len(noteList) != 0 {
		t.Fatalf("expected 0 notes before the window, got (%d, %v)", len(noteList), err)
	}
}

func TestMemoryNotes_ProvenanceAndOwnerRejection(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws, userA, _, agentX, _ := seedMemoryScopes(t, s)
	notes := s.MemoryNotes()

	// Birth-tuple completeness (D5): each missing member blocks the write.
	incomplete := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "No evidence pointer.")
	incomplete.SourceEventID = ""
	if err := notes.InsertNote(ctx, incomplete, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Fatalf("expected ErrMemoryProvenanceIncomplete for empty source_event_id, got %v", err)
	}
	incomplete = newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "No event time.")
	incomplete.EventTime = time.Time{}
	if err := notes.InsertNote(ctx, incomplete, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Fatalf("expected ErrMemoryProvenanceIncomplete for zero event_time, got %v", err)
	}
	incomplete = newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "No learned time.")
	incomplete.LearnedAt = time.Time{}
	if err := notes.InsertNote(ctx, incomplete, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Fatalf("expected ErrMemoryProvenanceIncomplete for zero learned_at, got %v", err)
	}
	incomplete = newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Unknown origin.")
	incomplete.Origin = "whispered"
	if err := notes.InsertNote(ctx, incomplete, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Fatalf("expected ErrMemoryProvenanceIncomplete for unknown origin, got %v", err)
	}

	// Owner shape mirrors the SQL CHECKs.
	badOwner := newMemoryNote(ws, domain.MemoryVisibilityUser, "", "No owner.")
	badOwner.UserID = nil
	if err := notes.InsertNote(ctx, badOwner, domain.MemoryVisibilityUser); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for user-visibility note without owner, got %v", err)
	}
	badShared := newMemoryNote(ws, domain.MemoryVisibilityShared, "", "Shared with a stray owner.")
	badShared.UserID = &userA
	if err := notes.InsertNote(ctx, badShared, domain.MemoryVisibilityShared); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for shared note with user owner, got %v", err)
	}

	// Events enforce the same rules.
	events := s.MemoryEvents()
	badEvent := newMemoryEvent(ws, agentX, "sess_p", "turn_1", domain.MemoryVisibilityUser, userA, "No provenance.")
	badEvent.LearnedAt = time.Time{}
	if err := events.InsertEvent(ctx, badEvent); !errors.Is(err, domain.ErrMemoryProvenanceIncomplete) {
		t.Fatalf("expected ErrMemoryProvenanceIncomplete for event without learned_at, got %v", err)
	}
	badEvent = newMemoryEvent(ws, agentX, "sess_p", "turn_1", domain.MemoryVisibilityUser, userA, "No owner.")
	badEvent.UserID = nil
	if err := events.InsertEvent(ctx, badEvent); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for user-visibility event without owner, got %v", err)
	}
}

func TestMemoryNotes_WithTxSnapshotIsolation(t *testing.T) {
	ctx := context.Background()
	s := fake.New()
	ws, userA, _, _, _ := seedMemoryScopes(t, s)
	notes := s.MemoryNotes()

	// Rollback discards memory writes (exercises the clone/apply snapshot).
	if err := s.WithTx(ctx, func(txStore store.Store) error {
		n := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Rolled back fact.")
		if err := txStore.MemoryNotes().InsertNote(ctx, n, domain.MemoryVisibilityUser); err != nil {
			return err
		}
		e := newMemoryEvent(ws, agentIDForTx(t, txStore, ws), "sess_tx", "turn_1", domain.MemoryVisibilityUser, userA, "Rolled back gist.")
		if err := txStore.MemoryEvents().InsertEvent(ctx, e); err != nil {
			return err
		}
		return errors.New("abort the transaction")
	}); err == nil {
		t.Fatal("expected the transaction to fail")
	}
	list, _ := notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{})
	if len(list) != 0 {
		t.Fatalf("expected rolled-back note to be discarded, got %d", len(list))
	}
	events, _ := s.MemoryEvents().ListEventsForUI(ctx, ws, userA, "", store.MemoryEventFilters{})
	if len(events) != 0 {
		t.Fatalf("expected rolled-back event to be discarded, got %d", len(events))
	}

	// Commit persists both writes.
	if err := s.WithTx(ctx, func(txStore store.Store) error {
		n := newMemoryNote(ws, domain.MemoryVisibilityUser, userA, "Committed fact.")
		if err := txStore.MemoryNotes().InsertNote(ctx, n, domain.MemoryVisibilityUser); err != nil {
			return err
		}
		e := newMemoryEvent(ws, agentIDForTx(t, txStore, ws), "sess_tx", "turn_1", domain.MemoryVisibilityUser, userA, "Committed gist.")
		return txStore.MemoryEvents().InsertEvent(ctx, e)
	}); err != nil {
		t.Fatalf("unexpected WithTx error: %v", err)
	}
	list, _ = notes.ListNotesForUI(ctx, ws, userA, "", store.MemoryNoteFilters{})
	if len(list) != 1 {
		t.Fatalf("expected committed note to persist, got %d", len(list))
	}
	events, _ = s.MemoryEvents().ListEventsForUI(ctx, ws, userA, "", store.MemoryEventFilters{})
	if len(events) != 1 {
		t.Fatalf("expected committed event to persist, got %d", len(events))
	}
}

// agentIDForTx resolves the workspace's first agent inside a transaction —
// events require a producing agent.
func agentIDForTx(t *testing.T, s store.Store, wsID string) string {
	t.Helper()
	agents, err := s.Agents().ListForWorkspace(context.Background(), wsID)
	if err != nil || len(agents) == 0 {
		t.Fatalf("expected a seeded agent, got (%d, %v)", len(agents), err)
	}
	return agents[0].ID
}
