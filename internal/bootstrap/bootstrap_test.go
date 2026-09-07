package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/bootstrap"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestEnsureMaster_Idempotence(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	b := bootstrap.New(st)

	// First invocation creates master and roles
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

	// Verify built-in roles
	roles, err := st.Roles().ListForWorkspace(ctx, master.ID)
	if err != nil {
		t.Fatalf("failed to list roles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected 2 built-in roles, got %d", len(roles))
	}

	superadminRole, err := st.Roles().FindByName(ctx, master.ID, domain.RoleSuperadmin)
	if err != nil {
		t.Fatalf("failed to find Superadmin role: %v", err)
	}
	if !superadminRole.IsOwner || !superadminRole.BuiltIn {
		t.Errorf("Superadmin role flags incorrect: is_owner=%v, built_in=%v", superadminRole.IsOwner, superadminRole.BuiltIn)
	}
	if len(superadminRole.Permissions) != len(domain.SuperadminPermissions) {
		t.Errorf("Superadmin role has %d permissions, want %d", len(superadminRole.Permissions), len(domain.SuperadminPermissions))
	}

	memberRole, err := st.Roles().FindByName(ctx, master.ID, domain.RoleMember)
	if err != nil {
		t.Fatalf("failed to find Member role: %v", err)
	}
	if memberRole.IsOwner || !memberRole.BuiltIn {
		t.Errorf("Member role flags incorrect: is_owner=%v, built_in=%v", memberRole.IsOwner, memberRole.BuiltIn)
	}
	if len(memberRole.Permissions) != len(domain.MemberPermissions) {
		t.Errorf("Member role has %d permissions, want %d", len(memberRole.Permissions), len(domain.MemberPermissions))
	}

	// Second invocation is a no-op / returns existing
	master2, err := b.EnsureMaster(ctx)
	if err != nil {
		t.Fatalf("unexpected error from second EnsureMaster: %v", err)
	}
	if master2.ID != master.ID {
		t.Errorf("second invocation returned different master ID")
	}

	roles2, _ := st.Roles().ListForWorkspace(ctx, master.ID)
	if len(roles2) != 2 {
		t.Errorf("roles duplicated: expected 2, got %d", len(roles2))
	}
}

func TestSeedSuperadmin(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	b := bootstrap.New(st)

	// Case 1: Empty config skips without error
	user, err := b.SeedSuperadmin(ctx, bootstrap.SuperadminSeedConfig{})
	if err != nil || user != nil {
		t.Fatalf("expected nil user and no error for empty seed config, got: %v, %v", user, err)
	}

	// Case 2: Seed with password
	cfg := bootstrap.SuperadminSeedConfig{
		Email:    "admin@onclaw.local",
		Password: "supersecretpassword",
		Name:     "Root Superadmin",
	}
	user, err = b.SeedSuperadmin(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to seed superadmin: %v", err)
	}
	if user == nil {
		t.Fatalf("expected non-nil user")
	}
	if user.Email != "admin@onclaw.local" {
		t.Errorf("got email %q, want %q", user.Email, "admin@onclaw.local")
	}
	if user.Name != "Root Superadmin" {
		t.Errorf("got name %q, want %q", user.Name, "Root Superadmin")
	}
	if user.PasswordHash == nil {
		t.Fatalf("expected non-nil password hash")
	}

	// Verify password hash can authenticate
	ok, err := services.NewPasswordHasher().Verify("supersecretpassword", *user.PasswordHash)
	if err != nil || !ok {
		t.Fatalf("superadmin password hash verification failed: %v", err)
	}

	// Verify membership in master workspace
	master, err := st.Workspaces().BySlug(ctx, domain.MasterWorkspaceSlug)
	if err != nil {
		t.Fatalf("failed to get master workspace: %v", err)
	}
	member, err := st.Members().Get(ctx, master.ID, user.ID)
	if err != nil {
		t.Fatalf("failed to get superadmin membership: %v", err)
	}
	if member.Role.Name != domain.RoleSuperadmin {
		t.Errorf("got role name %q, want %q", member.Role.Name, domain.RoleSuperadmin)
	}

	// Case 3: Idempotent re-seed skips when superadmin already exists in master
	reseedCfg := bootstrap.SuperadminSeedConfig{
		Email:    "other-admin@onclaw.local",
		Password: "different-password",
	}
	user2, err := b.SeedSuperadmin(ctx, reseedCfg)
	if err != nil {
		t.Fatalf("unexpected error on idempotent re-seed: %v", err)
	}
	if user2 != nil {
		t.Errorf("expected re-seed to return nil user when already seeded, got %v", user2)
	}

	// Verify original superadmin is unchanged
	origUser, _ := st.Users().ByEmail(ctx, "admin@onclaw.local")
	if origUser == nil {
		t.Fatalf("original superadmin was deleted or modified")
	}
}

func TestSeedSuperadmin_PasswordFile(t *testing.T) {
	st := fake.New()
	ctx := context.Background()
	b := bootstrap.New(st)

	tmpDir := t.TempDir()
	pwFile := filepath.Join(tmpDir, "superadmin.secret")
	if err := os.WriteFile(pwFile, []byte("filepassword123\n"), 0600); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	cfg := bootstrap.SuperadminSeedConfig{
		Email:        "admin-file@onclaw.local",
		PasswordFile: pwFile,
	}

	user, err := b.SeedSuperadmin(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to seed superadmin via password file: %v", err)
	}
	if user == nil {
		t.Fatalf("expected non-nil user")
	}

	ok, err := services.NewPasswordHasher().Verify("filepassword123", *user.PasswordHash)
	if err != nil || !ok {
		t.Fatalf("password from file verification failed: %v", err)
	}
}
