package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

type fakeSSOProvider struct {
	name       string
	provisions bool
	identity   *auth.Identity
	err        error
}

func (f *fakeSSOProvider) Name() string {
	return f.name
}

func (f *fakeSSOProvider) Authenticate(ctx context.Context, credentials map[string]string) (*auth.Identity, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.identity, nil
}

func (f *fakeSSOProvider) Begin(ctx context.Context, params map[string]string) (string, string, error) {
	return "https://sso.example.com", "state-123", nil
}

func (f *fakeSSOProvider) Complete(ctx context.Context, state string, params map[string]string) (*auth.Identity, error) {
	return f.identity, f.err
}

func (f *fakeSSOProvider) ProvisionsUsers() bool {
	return f.provisions
}

func TestService_Login_UnknownProvider(t *testing.T) {
	st := fake.New()
	issuer := auth.NewJWTIssuer(auth.JWTConfig{Secret: "test-secret"})
	svc := auth.NewService(st, issuer, nil)
	ctx := context.Background()

	_, err := svc.Login(ctx, auth.LoginRequest{
		Provider:    "unknown-provider",
		Credentials: map[string]string{"email": "user@example.com"},
	})

	if err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for unknown provider, got: %v", err)
	}
}

func TestService_Login_PasswordProvider(t *testing.T) {
	st := fake.New()
	ctx := context.Background()

	hash, _ := auth.NewPasswordHasher().Hash("secret123")
	user := &domain.User{
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: &hash,
	}
	_ = st.Users().Create(ctx, user)

	issuer := auth.NewJWTIssuer(auth.JWTConfig{Secret: "test-secret"})
	svc := auth.NewService(st, issuer, nil)

	// Default provider is password when omitted
	res, err := svc.Login(ctx, auth.LoginRequest{
		Credentials: map[string]string{
			"email":    "alice@example.com",
			"password": "secret123",
		},
	})
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}
	if res.User.Email != "alice@example.com" {
		t.Errorf("got user email %q, want %q", res.User.Email, "alice@example.com")
	}
	if res.Token == "" {
		t.Errorf("expected non-empty JWT token")
	}
}

func TestService_Login_NonProvisioningRequiresAccount(t *testing.T) {
	st := fake.New()
	ctx := context.Background()

	reg := auth.NewRegistry()
	mockNonProv := &fakeSSOProvider{
		name:       "mock-ldap",
		provisions: false,
		identity: &auth.Identity{
			Email: "ldap-unknown@example.com",
			Name:  "LDAP User",
		},
	}
	reg.Register(mockNonProv)

	issuer := auth.NewJWTIssuer(auth.JWTConfig{Secret: "test-secret"})
	svc := auth.NewService(st, issuer, reg)

	_, err := svc.Login(ctx, auth.LoginRequest{
		Provider: "mock-ldap",
	})
	if err == nil || !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated when non-provisioning user does not exist, got: %v", err)
	}
}

func TestService_Login_ProvisioningJITCreatesUser(t *testing.T) {
	st := fake.New()
	ctx := context.Background()

	reg := auth.NewRegistry()
	mockProv := &fakeSSOProvider{
		name:       "mock-google",
		provisions: true,
		identity: &auth.Identity{
			Email:     "newuser@google.com",
			Name:      "New Google User",
			AvatarURL: "https://avatar.google.com/photo.png",
		},
	}
	reg.Register(mockProv)

	issuer := auth.NewJWTIssuer(auth.JWTConfig{Secret: "test-secret"})
	svc := auth.NewService(st, issuer, reg)

	res, err := svc.Login(ctx, auth.LoginRequest{
		Provider: "mock-google",
	})
	if err != nil {
		t.Fatalf("unexpected error during JIT provisioning login: %v", err)
	}

	if res.User.Email != "newuser@google.com" {
		t.Errorf("got email %q, want %q", res.User.Email, "newuser@google.com")
	}
	if res.User.Name != "New Google User" {
		t.Errorf("got name %q, want %q", res.User.Name, "New Google User")
	}
	if res.User.AvatarURL == nil || *res.User.AvatarURL != "https://avatar.google.com/photo.png" {
		t.Errorf("expected avatar_url to be populated on JIT creation")
	}
	if res.User.PasswordHash != nil {
		t.Errorf("expected password_hash to be nil for SSO JIT-created user")
	}

	// Verify in DB
	dbUser, err := st.Users().ByEmail(ctx, "newuser@google.com")
	if err != nil {
		t.Fatalf("user was not saved in store: %v", err)
	}
	if dbUser.ID != res.User.ID {
		t.Errorf("store user ID mismatch")
	}
}

func TestService_Login_AvatarBackfill(t *testing.T) {
	st := fake.New()
	ctx := context.Background()

	// Existing user without avatar
	existing := &domain.User{
		Email: "existing@example.com",
		Name:  "Existing User",
	}
	_ = st.Users().Create(ctx, existing)

	reg := auth.NewRegistry()
	mockProv := &fakeSSOProvider{
		name:       "sso",
		provisions: true,
		identity: &auth.Identity{
			Email:     "existing@example.com",
			Name:      "Existing User",
			AvatarURL: "https://sso.example.com/avatar.jpg",
		},
	}
	reg.Register(mockProv)

	issuer := auth.NewJWTIssuer(auth.JWTConfig{Secret: "test-secret"})
	svc := auth.NewService(st, issuer, reg)

	res, err := svc.Login(ctx, auth.LoginRequest{Provider: "sso"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.User.AvatarURL == nil || *res.User.AvatarURL != "https://sso.example.com/avatar.jpg" {
		t.Errorf("expected avatar to be backfilled")
	}

	// Verify in DB
	dbUser, _ := st.Users().ByEmail(ctx, "existing@example.com")
	if dbUser.AvatarURL == nil || *dbUser.AvatarURL != "https://sso.example.com/avatar.jpg" {
		t.Errorf("expected DB avatar_url to be updated")
	}

	// If user already has an avatar key, do NOT overwrite with SSO avatar URL
	avatarKey := "key-1234"
	existing2 := &domain.User{
		Email:     "custom-avatar@example.com",
		Name:      "Custom Avatar User",
		AvatarKey: &avatarKey,
	}
	_ = st.Users().Create(ctx, existing2)

	mockProv2 := &fakeSSOProvider{
		name:       "sso2",
		provisions: true,
		identity: &auth.Identity{
			Email:     "custom-avatar@example.com",
			AvatarURL: "https://sso.example.com/override.jpg",
		},
	}
	reg.Register(mockProv2)

	res2, err := svc.Login(ctx, auth.LoginRequest{Provider: "sso2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.User.AvatarURL != nil {
		t.Errorf("expected AvatarURL not to be overwritten when AvatarKey is present")
	}
}

func TestService_Login_DisabledUserRejected(t *testing.T) {
	st := fake.New()
	ctx := context.Background()

	now := time.Now().UTC()
	disabled := &domain.User{
		Email:      "disabled-jit@example.com",
		Name:       "Disabled JIT",
		DisabledAt: &now,
	}
	_ = st.Users().Create(ctx, disabled)

	reg := auth.NewRegistry()
	mockProv := &fakeSSOProvider{
		name:       "sso",
		provisions: true,
		identity: &auth.Identity{
			Email: "disabled-jit@example.com",
		},
	}
	reg.Register(mockProv)

	issuer := auth.NewJWTIssuer(auth.JWTConfig{Secret: "test-secret"})
	svc := auth.NewService(st, issuer, reg)

	_, err := svc.Login(ctx, auth.LoginRequest{Provider: "sso"})
	if err == nil || !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated for disabled user, got: %v", err)
	}
}

func TestService_Me(t *testing.T) {
	st := fake.New()
	ctx := context.Background()

	user := &domain.User{
		Email: "me-user@example.com",
		Name:  "Me User",
	}
	_ = st.Users().Create(ctx, user)

	ws := &domain.Workspace{
		Slug: "test-workspace",
		Name: "Test Workspace",
	}
	_ = st.Workspaces().Create(ctx, ws)

	role := &domain.Role{
		WorkspaceID: ws.ID,
		Name:        "Owner",
		IsOwner:     true,
		Permissions: domain.OwnerPermissions,
	}
	_ = st.Roles().Create(ctx, role)

	member := &domain.Member{
		WorkspaceID: ws.ID,
		UserID:      user.ID,
		RoleID:      role.ID,
	}
	_ = st.Members().Add(ctx, member)

	issuer := auth.NewJWTIssuer(auth.JWTConfig{Secret: "test-secret"})
	svc := auth.NewService(st, issuer, nil)

	meRes, err := svc.Me(ctx, user.ID)
	if err != nil {
		t.Fatalf("unexpected error from Me: %v", err)
	}

	if meRes.User.ID != user.ID || meRes.User.Email != user.Email {
		t.Errorf("unexpected user in Me response")
	}
	if len(meRes.Memberships) != 1 {
		t.Fatalf("expected 1 membership, got %d", len(meRes.Memberships))
	}
	if meRes.Memberships[0].WorkspaceSlug != "test-workspace" {
		t.Errorf("got slug %q, want %q", meRes.Memberships[0].WorkspaceSlug, "test-workspace")
	}
	if meRes.Memberships[0].RoleName != "Owner" {
		t.Errorf("got role %q, want %q", meRes.Memberships[0].RoleName, "Owner")
	}

	// Disabled user returns ErrUnauthenticated
	now := time.Now().UTC()
	_ = st.Users().SetDisabled(ctx, user.ID, &now)
	_, err = svc.Me(ctx, user.ID)
	if err == nil || !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expected ErrUnauthenticated for disabled user from Me, got: %v", err)
	}

	// Unknown user returns ErrUnauthenticated
	_, err = svc.Me(ctx, "unknown-user-id")
	if err == nil || !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expected ErrUnauthenticated for unknown user from Me, got: %v", err)
	}
}
