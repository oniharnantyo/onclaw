package service_test

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestService_VerifyPassword_Correct(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Seed password
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}
	f.kvStore.Set(ctx, "web_password_hash", string(hash))

	ok, err := f.svc.VerifyPassword(ctx, "secret123")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("expected password verification to succeed")
	}
}

func TestService_VerifyPassword_Wrong(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}
	f.kvStore.Set(ctx, "web_password_hash", string(hash))

	ok, err := f.svc.VerifyPassword(ctx, "wrongpassword")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Error("expected verification to fail for wrong password")
	}
}

func TestService_VerifyPassword_NoHashStored(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// No hash set in KV store -> expects error
	_, err := f.svc.VerifyPassword(ctx, "secret123")
	if err == nil {
		t.Error("expected error when no password is set")
	}
}
