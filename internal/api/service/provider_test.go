package service_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
)

func TestService_SetDefaultProvider_NotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	err := f.svc.SetDefaultProvider(ctx, "nonexistent-provider")
	if err == nil {
		t.Error("expected error setting nonexistent provider as default")
	}
}

func TestService_DeleteProvider_WasDefault(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "openai", ProviderType: "openai"})
	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "anthropic", ProviderType: "anthropic"})

	f.svc.SetDefaultProvider(ctx, "openai")

	// Delete default provider
	err := f.svc.DeleteProvider(ctx, "openai")
	if err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	_, err = f.kvStore.Get(ctx, "default_provider")
	if err == nil {
		t.Error("expected default_provider kv entry to be cleared after delete")
	}
}

func TestService_ListProviders_Empty(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	list, err := f.svc.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestService_CreateProvider_And_GetProvider(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.svc.CreateProvider(ctx, service.ProfileInput{
		Name:         "openai-test",
		ProviderType: "openai",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	v, err := f.svc.GetProvider(ctx, "openai-test")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if v.Name != "openai-test" {
		t.Errorf("unexpected name: %q", v.Name)
	}
	if !v.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestService_GetProvider_NotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.GetProvider(context.Background(), "ghost")
	if err == nil {
		t.Error("expected error for missing provider")
	}
}

func TestService_DeleteProvider(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "to-delete", ProviderType: "openai"})
	if err := f.svc.DeleteProvider(ctx, "to-delete"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	_, err := f.svc.GetProvider(ctx, "to-delete")
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestService_SetDefaultProvider(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "p1", ProviderType: "openai"})
	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "p2", ProviderType: "anthropic"})

	if err := f.svc.SetDefaultProvider(ctx, "p2"); err != nil {
		t.Fatalf("SetDefaultProvider: %v", err)
	}

	v2, _ := f.svc.GetProvider(ctx, "p2")
	if !v2.IsDefault {
		t.Error("expected p2 to be default")
	}

	v1, _ := f.svc.GetProvider(ctx, "p1")
	if v1.IsDefault {
		t.Error("expected p1 default to be cleared")
	}
}

func TestService_UpdateProvider(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "upd-p", ProviderType: "openai"})

	_, err := f.svc.UpdateProvider(ctx, "upd-p", service.ProfileInput{
		ProviderType: "anthropic",
		Enabled:      false,
	})
	if err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}

	v, _ := f.svc.GetProvider(ctx, "upd-p")
	if v.ProviderType != "anthropic" {
		t.Errorf("expected ProviderType=anthropic, got %q", v.ProviderType)
	}
}
