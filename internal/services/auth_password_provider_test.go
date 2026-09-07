package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

func TestPasswordProvider_Uniform401Semantics(t *testing.T) {
	st := fake.New()
	ctx := context.Background()

	hasher := services.NewPasswordHasher()
	hash, err := hasher.Hash("valid-password")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	// Active user with password
	activeUser := &domain.User{
		Email:        "active@example.com",
		Name:         "Active User",
		PasswordHash: &hash,
	}
	if err := st.Users().Create(ctx, activeUser); err != nil {
		t.Fatalf("failed to create active user: %v", err)
	}

	// Passwordless user
	passwordlessUser := &domain.User{
		Email: "passwordless@example.com",
		Name:  "Passwordless User",
	}
	if err := st.Users().Create(ctx, passwordlessUser); err != nil {
		t.Fatalf("failed to create passwordless user: %v", err)
	}

	// Disabled user
	now := time.Now().UTC()
	disabledUser := &domain.User{
		Email:        "disabled@example.com",
		Name:         "Disabled User",
		PasswordHash: &hash,
		DisabledAt:   &now,
	}
	if err := st.Users().Create(ctx, disabledUser); err != nil {
		t.Fatalf("failed to create disabled user: %v", err)
	}

	provider := services.NewPasswordProvider(st.Users())

	testCases := []struct {
		name        string
		credentials map[string]string
	}{
		{
			name:        "nil credentials",
			credentials: nil,
		},
		{
			name:        "empty email",
			credentials: map[string]string{"email": "", "password": "valid-password"},
		},
		{
			name:        "empty password",
			credentials: map[string]string{"email": "active@example.com", "password": ""},
		},
		{
			name:        "unknown email",
			credentials: map[string]string{"email": "unknown@example.com", "password": "valid-password"},
		},
		{
			name:        "wrong password",
			credentials: map[string]string{"email": "active@example.com", "password": "wrong-password"},
		},
		{
			name:        "passwordless account",
			credentials: map[string]string{"email": "passwordless@example.com", "password": "any-password"},
		},
		{
			name:        "disabled account with correct password",
			credentials: map[string]string{"email": "disabled@example.com", "password": "valid-password"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			identity, err := provider.Authenticate(ctx, tc.credentials)
			if identity != nil {
				t.Errorf("expected identity to be nil, got: %v", identity)
			}
			if !errors.Is(err, domain.ErrUnauthenticated) {
				t.Errorf("expected ErrUnauthenticated for %s, got: %v", tc.name, err)
			}
		})
	}

	// Happy path
	identity, err := provider.Authenticate(ctx, map[string]string{
		"email":    "ACTIVE@example.com ", // Tests normalization
		"password": "valid-password",
	})
	if err != nil {
		t.Fatalf("unexpected error for valid credentials: %v", err)
	}
	if identity.Email != "active@example.com" {
		t.Errorf("got email %q, want %q", identity.Email, "active@example.com")
	}
	if identity.Name != "Active User" {
		t.Errorf("got name %q, want %q", identity.Name, "Active User")
	}
}

func TestPasswordProvider_UnsupportedMethods(t *testing.T) {
	provider := services.NewPasswordProvider(nil)
	ctx := context.Background()

	if provider.ProvisionsUsers() {
		t.Errorf("expected ProvisionsUsers to be false")
	}

	if _, _, err := provider.Begin(ctx, nil); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid from Begin, got: %v", err)
	}

	if _, err := provider.Complete(ctx, "", nil); err == nil || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid from Complete, got: %v", err)
	}
}
