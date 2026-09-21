//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oniharnantyo/onclaw/internal/bootstrap"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/postgres"
)

// latestSchemaVersion is the newest embedded migration number.
const latestSchemaVersion = 61

// previousSchemaVersion is the migration version below latestSchemaVersion.
// The channel-teams wave numbered its migration 000040 after v1's 000031
// wave, leaving 000032–000039 unused (golang-migrate tolerates gaps), so one
// step down from the latest must land on the previous EXISTING version.
const previousSchemaVersion = 60

func getTestBaseDSN(t *testing.T) string {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://localhost:5432/postgres?sslmode=disable"
	}
	return dsn
}

func setupTestSchema(t *testing.T) (store.Store, string, context.Context) {
	t.Helper()
	ctx := context.Background()
	baseDSN := getTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	// Generate random schema name
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schemaName := fmt.Sprintf("test_schema_%s", hex.EncodeToString(b))

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	if err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s", baseDSN, separator, schemaName)

	// Run migrations on schema
	migrator := postgres.NewMigrator(schemaDSN)
	if err := migrator.Up(); err != nil {
		_, _ = conn.Exec(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		t.Fatalf("failed to run migrations: %v", err)
	}

	s, err := postgres.New(ctx, schemaDSN)
	if err != nil {
		_, _ = conn.Exec(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		t.Fatalf("failed to initialize postgres store: %v", err)
	}

	t.Cleanup(func() {
		_ = s.Close()
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
			_ = cleanupConn.Close(context.Background())
		}
	})

	return s, schemaDSN, ctx
}

func TestIntegration_Migration_IdempotenceAndRollback(t *testing.T) {
	ctx := context.Background()
	baseDSN := getTestBaseDSN(t)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		t.Skipf("skipping integration test: database connection failed: %v", err)
	}
	defer conn.Close(ctx)

	b := make([]byte, 8)
	_, _ = rand.Read(b)
	schemaName := fmt.Sprintf("test_mig_%s", hex.EncodeToString(b))

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName))
	if err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}
	defer func() {
		cleanupConn, err := pgx.Connect(context.Background(), baseDSN)
		if err == nil {
			_, _ = cleanupConn.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
			_ = cleanupConn.Close(context.Background())
		}
	}()

	separator := "?"
	if strings.Contains(baseDSN, "?") {
		separator = "&"
	}
	schemaDSN := fmt.Sprintf("%s%ssearch_path=%s", baseDSN, separator, schemaName)

	mig := postgres.NewMigrator(schemaDSN)

	// 1. Initial status before migration
	v, dirty, err := mig.Status()
	if err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
	if v != 0 || dirty {
		t.Fatalf("expected version 0 and dirty false, got version %d, dirty %v", v, dirty)
	}

	// 2. MigrateUp
	if err := mig.Up(); err != nil {
		t.Fatalf("unexpected MigrateUp error: %v", err)
	}
	v, dirty, err = mig.Status()
	if err != nil || v != latestSchemaVersion || dirty {
		t.Fatalf("expected latest version not dirty, got v=%d, dirty=%v, err=%v", v, dirty, err)
	}

	// 3. MigrateUp again is idempotent
	if err := mig.Up(); err != nil {
		t.Fatalf("expected MigrateUp to be idempotent, got %v", err)
	}
	v, dirty, err = mig.Status()
	if err != nil || v != latestSchemaVersion || dirty {
		t.Fatalf("expected latest version not dirty, got v=%d, dirty=%v, err=%v", v, dirty, err)
	}

	// 4. MigrateDown 1 step (lands on the previous EXISTING migration — the
	// 000032–000039 gap is intentional)
	if err := mig.Down(1); err != nil {
		t.Fatalf("unexpected MigrateDown step error: %v", err)
	}
	v, dirty, err = mig.Status()
	if err != nil || v != previousSchemaVersion || dirty {
		t.Fatalf("expected version %d, got v=%d, dirty=%v, err=%v", previousSchemaVersion, v, dirty, err)
	}

	// 5. MigrateDown all
	if err := mig.Down(0); err != nil {
		t.Fatalf("unexpected MigrateDown all error: %v", err)
	}
	v, dirty, err = mig.Status()
	if err != nil || v != 0 || dirty {
		t.Fatalf("expected version 0, got v=%d, dirty=%v, err=%v", v, dirty, err)
	}

	// 6. MigrateUp again back to latest
	if err := mig.Up(); err != nil {
		t.Fatalf("unexpected MigrateUp error: %v", err)
	}
	v, dirty, err = mig.Status()
	if err != nil || v != latestSchemaVersion || dirty {
		t.Fatalf("expected latest version not dirty, got v=%d, dirty=%v, err=%v", v, dirty, err)
	}
}

func TestIntegration_UserStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	// 1. Create user with email normalization
	u := &domain.User{
		Email: "Alice@Example.COM  ",
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

	// 2. Duplicate email conflict
	err := s.Users().Create(ctx, &domain.User{
		Email: "alice@example.com",
		Name:  "Alice Dup",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate email, got %v", err)
	}

	// 3. Invalid email error
	err = s.Users().Create(ctx, &domain.User{
		Email: "not-an-email",
		Name:  "Bad",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for invalid email, got %v", err)
	}

	// 4. ByEmail lookup
	byEmail, err := s.Users().ByEmail(ctx, "ALICE@EXAMPLE.COM")
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

	// ByID non-existent UUID or invalid string
	_, err = s.Users().ByID(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent ID, got %v", err)
	}
	_, err = s.Users().ByID(ctx, "invalid-uuid-format")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for invalid UUID format, got %v", err)
	}

	// 6. SetPasswordHash
	if err := s.Users().SetPasswordHash(ctx, u.ID, "argon2id$secret"); err != nil {
		t.Fatalf("unexpected SetPasswordHash error: %v", err)
	}
	updatedUser, _ := s.Users().ByID(ctx, u.ID)
	if updatedUser.PasswordHash == nil || *updatedUser.PasswordHash != "argon2id$secret" {
		t.Fatalf("expected password hash to be updated")
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
	avatarKey := "avatars/alice123"
	enabledUser.Name = "Alice Wonderland"
	enabledUser.AvatarKey = &avatarKey
	if err := s.Users().Update(ctx, enabledUser); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}
	reloaded, _ := s.Users().ByID(ctx, u.ID)
	if reloaded.Name != "Alice Wonderland" || reloaded.AvatarKey == nil || *reloaded.AvatarKey != avatarKey {
		t.Fatalf("unexpected user after update: %+v", reloaded)
	}

	// 9. List
	u2 := &domain.User{Email: "bob@example.com", Name: "Bob"}
	if err := s.Users().Create(ctx, u2); err != nil {
		t.Fatalf("unexpected Create u2 error: %v", err)
	}
	list, err := s.Users().List(ctx)
	if err != nil {
		t.Fatalf("unexpected List error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 users, got %d", len(list))
	}
}

func TestIntegration_WorkspaceStore_CRUD(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

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

	// 2. Duplicate slug conflict
	err := s.Workspaces().Create(ctx, &domain.Workspace{
		Slug: "acme-corp",
		Name: "Acme 2",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate slug, got %v", err)
	}

	// 3. Reserved slug rejected when IsMaster=false
	err = s.Workspaces().Create(ctx, &domain.Workspace{
		Slug: "master",
		Name: "Fake Master",
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

	// 5. Second master workspace rejected by partial unique index
	err = s.Workspaces().Create(ctx, &domain.Workspace{
		Slug:     "second-master",
		Name:     "Second Master",
		IsMaster: true,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate master workspace, got %v", err)
	}

	// 6. BySlug lookup
	bySlug, err := s.Workspaces().BySlug(ctx, "acme-corp")
	if err != nil {
		t.Fatalf("unexpected BySlug error: %v", err)
	}
	if bySlug.ID != ws.ID || bySlug.Name != "Acme Corporation" {
		t.Fatalf("unexpected workspace retrieved: %+v", bySlug)
	}

	// 7. Update
	bySlug.Name = "Acme Corp International"
	bySlug.Timezone = "America/New_York"
	if err := s.Workspaces().Update(ctx, bySlug); err != nil {
		t.Fatalf("unexpected Update error: %v", err)
	}
	reloaded, _ := s.Workspaces().ByID(ctx, ws.ID)
	if reloaded.Name != "Acme Corp International" || reloaded.Timezone != "America/New_York" {
		t.Fatalf("unexpected updated workspace: %+v", reloaded)
	}

	// 8. ListAll
	all, err := s.Workspaces().ListAll(ctx)
	if err != nil {
		t.Fatalf("unexpected ListAll error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(all))
	}
}

func TestIntegration_RoleAndMemberStore(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	// Seed user and workspace
	u := &domain.User{Email: "charlie@example.com", Name: "Charlie"}
	if err := s.Users().Create(ctx, u); err != nil {
		t.Fatalf("unexpected Create user error: %v", err)
	}

	ws := &domain.Workspace{Slug: "startup", Name: "Startup Inc"}
	if err := s.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("unexpected Create workspace error: %v", err)
	}

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

	// Duplicate member add fails with ErrConflict
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
	if userViews[0].Workspace == nil || userViews[0].Workspace.Slug != ws.Slug {
		t.Fatalf("expected joined workspace in member view")
	}

	// UpdateRole
	if err := s.Members().UpdateRole(ctx, ws.ID, u.ID, adminRole.ID); err != nil {
		t.Fatalf("unexpected UpdateRole error: %v", err)
	}
	gotMemberAfterUpdate, _ := s.Members().Get(ctx, ws.ID, u.ID)
	if gotMemberAfterUpdate.RoleID != adminRole.ID || gotMemberAfterUpdate.Role.Name != domain.RoleAdmin {
		t.Fatalf("expected role Admin after update, got %+v", gotMemberAfterUpdate.Role)
	}

	// UpdateRole with invalid role from other workspace
	otherWS := &domain.Workspace{Slug: "other", Name: "Other WS"}
	_ = s.Workspaces().Create(ctx, otherWS)
	otherRole := &domain.Role{WorkspaceID: otherWS.ID, Name: "OtherRole"}
	_ = s.Roles().Create(ctx, otherRole)
	err = s.Members().UpdateRole(ctx, ws.ID, u.ID, otherRole.ID)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid when assigning role from another workspace, got %v", err)
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

func TestIntegration_WithTx_Commit(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

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

func TestIntegration_WithTx_Rollback(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	// Initial user
	u0 := &domain.User{Email: "existing@example.com", Name: "Existing"}
	if err := s.Users().Create(ctx, u0); err != nil {
		t.Fatalf("unexpected Create u0 error: %v", err)
	}

	expectedErr := errors.New("abort transaction")

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

		// Abort
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

	// Verify rollback - none of the tx changes should exist in store
	_, err = s.Users().ByEmail(ctx, "rollback@example.com")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for rolled back user, got %v", err)
	}

	_, err = s.Workspaces().BySlug(ctx, "rollback-ws")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for rolled back workspace, got %v", err)
	}

	// Existing user should NOT be disabled in store
	reloadedU0, _ := s.Users().ByID(ctx, u0.ID)
	if reloadedU0.IsDisabled() {
		t.Fatal("expected existing user not to be modified after rollback")
	}
}

func TestIntegration_WithTx_Nested(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

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

func TestIntegration_EnsureMaster_And_SeedSuperadmin_Idempotence(t *testing.T) {
	s, _, ctx := setupTestSchema(t)
	b := bootstrap.New(s)

	// 1. First invocation of EnsureMaster creates master workspace and built-in roles
	master, err := b.EnsureMaster(ctx)
	if err != nil {
		t.Fatalf("unexpected error from EnsureMaster: %v", err)
	}

	if master.Slug != domain.MasterWorkspaceSlug {
		t.Errorf("got slug %q, want %q", master.Slug, domain.MasterWorkspaceSlug)
	}
	if !master.IsMaster {
		t.Errorf("expected is_master to be true")
	}

	// Verify roles in Postgres
	superRole, err := s.Roles().FindByName(ctx, master.ID, domain.RoleSuperadmin)
	if err != nil {
		t.Fatalf("failed to find superadmin role: %v", err)
	}
	if !superRole.IsOwner || !superRole.BuiltIn {
		t.Errorf("expected Superadmin role to be owner and built-in")
	}
	if len(superRole.Permissions) != len(domain.SuperadminPermissions) {
		t.Errorf("expected %d permissions, got %d", len(domain.SuperadminPermissions), len(superRole.Permissions))
	}

	memberRole, err := s.Roles().FindByName(ctx, master.ID, domain.RoleMember)
	if err != nil {
		t.Fatalf("failed to find member role: %v", err)
	}
	if memberRole.IsOwner || !memberRole.BuiltIn {
		t.Errorf("expected Member role to not be owner and built-in")
	}

	// 2. Second invocation of EnsureMaster is idempotent
	master2, err := b.EnsureMaster(ctx)
	if err != nil {
		t.Fatalf("unexpected error on second EnsureMaster: %v", err)
	}
	if master2.ID != master.ID {
		t.Errorf("expected same master ID, got %s vs %s", master.ID, master2.ID)
	}

	roles, err := s.Roles().ListForWorkspace(ctx, master.ID)
	if err != nil || len(roles) != 2 {
		t.Fatalf("expected 2 roles after second EnsureMaster, got %d (err: %v)", len(roles), err)
	}

	// 3. SeedSuperadmin creates user and master membership
	cfg := bootstrap.SuperadminSeedConfig{
		Email:    "admin@onclaw.local",
		Password: "supersecretpassword123",
		Name:     "Root Superadmin",
	}
	user, err := b.SeedSuperadmin(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to seed superadmin: %v", err)
	}
	if user == nil {
		t.Fatalf("expected non-nil user from SeedSuperadmin")
	}
	if user.Email != "admin@onclaw.local" || user.Name != "Root Superadmin" {
		t.Errorf("unexpected user seeded: %+v", user)
	}
	if user.PasswordHash == nil {
		t.Fatalf("expected password hash to be set")
	}

	// Verify password hash can authenticate
	ok, err := services.NewPasswordHasher().Verify("supersecretpassword123", *user.PasswordHash)
	if err != nil || !ok {
		t.Fatalf("password verification failed: %v", err)
	}

	// Verify membership in master
	member, err := s.Members().Get(ctx, master.ID, user.ID)
	if err != nil {
		t.Fatalf("failed to get superadmin membership: %v", err)
	}
	if member.RoleID != superRole.ID {
		t.Errorf("expected Superadmin role ID %s, got %s", superRole.ID, member.RoleID)
	}

	// 4. SeedSuperadmin again is idempotent and skips
	reseedCfg := bootstrap.SuperadminSeedConfig{
		Email:    "other-superadmin@onclaw.local",
		Password: "some-password",
	}
	user2, err := b.SeedSuperadmin(ctx, reseedCfg)
	if err != nil {
		t.Fatalf("unexpected error on idempotent re-seed: %v", err)
	}
	if user2 != nil {
		t.Errorf("expected reseed to return nil user when already seeded, got %+v", user2)
	}

	// Confirm original user unchanged
	origUser, err := s.Users().ByEmail(ctx, "admin@onclaw.local")
	if err != nil || origUser == nil {
		t.Fatalf("original superadmin was deleted or modified: %v", err)
	}
}

func TestIntegration_WorkspaceMaster_UniqueConstraint(t *testing.T) {
	s, _, ctx := setupTestSchema(t)

	// 1. Create first master workspace
	w1 := &domain.Workspace{
		Slug:     domain.MasterWorkspaceSlug,
		Name:     "Master Tenant",
		IsMaster: true,
	}
	if err := s.Workspaces().Create(ctx, w1); err != nil {
		t.Fatalf("expected first master workspace creation to succeed: %v", err)
	}

	// 2. Attempt to create second master workspace with a different slug
	w2 := &domain.Workspace{
		Slug:     "second-master",
		Name:     "Second Master",
		IsMaster: true,
	}
	err := s.Workspaces().Create(ctx, w2)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate is_master=true workspace, got %v", err)
	}

	// 3. Multiple non-master workspaces can be created without conflict
	w3 := &domain.Workspace{
		Slug:     "team-alpha",
		Name:     "Team Alpha",
		IsMaster: false,
	}
	if err := s.Workspaces().Create(ctx, w3); err != nil {
		t.Fatalf("expected team-alpha workspace creation to succeed: %v", err)
	}

	w4 := &domain.Workspace{
		Slug:     "team-beta",
		Name:     "Team Beta",
		IsMaster: false,
	}
	if err := s.Workspaces().Create(ctx, w4); err != nil {
		t.Fatalf("expected team-beta workspace creation to succeed: %v", err)
	}
}
