package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestJWTIssuer_IssueAndVerify(t *testing.T) {
	cfg := auth.JWTConfig{
		Secret: "super-secret-key-for-testing-123456",
		TTL:    1 * time.Hour,
	}
	issuer := auth.NewJWTIssuer(cfg)
	ctx := context.Background()

	user := &domain.User{
		ID:    "usr-123",
		Email: "test@example.com",
		Name:  "Test User",
	}

	tokenString, err := issuer.Issue(ctx, user)
	if err != nil {
		t.Fatalf("unexpected error issuing token: %v", err)
	}
	if tokenString == "" {
		t.Fatalf("expected non-empty token string")
	}

	claims, err := issuer.Verify(ctx, tokenString)
	if err != nil {
		t.Fatalf("unexpected error verifying token: %v", err)
	}

	if claims.UserID != "usr-123" {
		t.Errorf("got UserID %q, want %q", claims.UserID, "usr-123")
	}
	if claims.Email != "test@example.com" {
		t.Errorf("got Email %q, want %q", claims.Email, "test@example.com")
	}
	if claims.Subject != "usr-123" {
		t.Errorf("got Subject %q, want %q", claims.Subject, "usr-123")
	}
}

func TestJWTIssuer_EphemeralSecret(t *testing.T) {
	// Unset secret generates ephemeral key
	issuer1 := auth.NewJWTIssuer(auth.JWTConfig{Secret: ""})
	issuer2 := auth.NewJWTIssuer(auth.JWTConfig{Secret: ""})
	ctx := context.Background()

	user := &domain.User{ID: "usr-456", Email: "ephemeral@example.com"}

	token1, err := issuer1.Issue(ctx, user)
	if err != nil {
		t.Fatalf("failed to issue token with ephemeral secret: %v", err)
	}

	// Should verify with same issuer instance
	claims, err := issuer1.Verify(ctx, token1)
	if err != nil || claims.UserID != "usr-456" {
		t.Fatalf("failed to verify with same issuer: %v", err)
	}

	// Token from issuer1 must fail on a fresh restart/re-instantiation with different ephemeral key
	_, err = issuer2.Verify(ctx, token1)
	if err == nil || !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expected ErrUnauthenticated when verifying across different ephemeral instances, got: %v", err)
	}
}

func TestJWTIssuer_ExpiredAndInvalidTokens(t *testing.T) {
	// Negative TTL results in already-expired token
	issuer := auth.NewJWTIssuer(auth.JWTConfig{
		Secret: "test-secret-key",
		TTL:    -1 * time.Minute,
	})
	ctx := context.Background()

	user := &domain.User{ID: "usr-expired", Email: "exp@example.com"}
	token, err := issuer.Issue(ctx, user)
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	_, err = issuer.Verify(ctx, token)
	if err == nil || !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expected ErrUnauthenticated for expired token, got: %v", err)
	}

	// Empty token
	_, err = issuer.Verify(ctx, "")
	if err == nil || !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expected ErrUnauthenticated for empty token, got: %v", err)
	}

	// Tampered token
	_, err = issuer.Verify(ctx, "invalid.token.structure")
	if err == nil || !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("expected ErrUnauthenticated for tampered token, got: %v", err)
	}
}
