package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
)

func TestService_SetSecret_NotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := f.svc.SetSecret(ctx, "nonexistent-provider", "secret")
	if err == nil {
		t.Error("expected error setting secret for nonexistent provider")
	}
}

func TestService_GetSecretStatus_ShortKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "short-key-p", ProviderType: "openai"})

	// Set a short secret (<=4 chars)
	if err := f.svc.SetSecret(ctx, "short-key-p", "abc"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}

	status, err := f.svc.GetSecretStatus(ctx, "short-key-p")
	if err != nil {
		t.Fatalf("GetSecretStatus: %v", err)
	}
	if !status.Set {
		t.Error("expected Set=true for short key")
	}
	if status.Hint != "..." {
		t.Errorf("expected hint='...' for short key, got %q", status.Hint)
	}
}

func TestService_New_NilFunctions(t *testing.T) {
	// Creating service with nil hooks/functions should not panic
	svc := service.New(nil, nil, nil, nil, nil, slog.Default(), nil, nil, nil, nil, nil, nil, nil)
	if svc == nil {
		t.Fatal("expected non-nil service from service.New")
	}
}

func TestService_GetSecretStatus_NoSecret(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "no-key", ProviderType: "openai"})

	status, err := f.svc.GetSecretStatus(ctx, "no-key")
	if err != nil {
		t.Fatalf("GetSecretStatus: %v", err)
	}
	if status.Set {
		t.Error("expected Set=false")
	}
	if status.Hint != "" {
		t.Errorf("expected empty hint, got %q", status.Hint)
	}
}

func TestService_SetSecret_And_GetSecretStatus(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "with-key", ProviderType: "openai"})

	if err := f.svc.SetSecret(ctx, "with-key", "sk-1234567890abcdef"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}

	status, err := f.svc.GetSecretStatus(ctx, "with-key")
	if err != nil {
		t.Fatalf("GetSecretStatus: %v", err)
	}
	if !status.Set {
		t.Error("expected Set=true after SetSecret")
	}
	if status.Hint == "" {
		t.Error("expected non-empty hint")
	}
}
