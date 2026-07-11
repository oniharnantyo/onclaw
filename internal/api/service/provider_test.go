package service_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestService_ListProviderModels_NotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.svc.ListProviderModels(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestService_ListProviderModels_Success(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "openai-models", ProviderType: "openai"})

	resp, err := f.svc.ListProviderModels(ctx, "openai-models")
	if err != nil {
		t.Fatalf("ListProviderModels: %v", err)
	}
	if resp.Models == nil {
		t.Error("expected models slice to be initialized, got nil")
	}
}

func TestService_ListProviderModels_EnumerationSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "mock-model-1"},
				{"id": "mock-model-2"}
			]
		}`))
	}))
	defer server.Close()

	f := newFixture(t)
	ctx := context.Background()
	f.svc.CreateProvider(ctx, service.ProfileInput{
		Name:         "mock-prov",
		ProviderType: "openai",
		APIBase:      server.URL,
	})

	resp, err := f.svc.ListProviderModels(ctx, "mock-prov")
	if err != nil {
		t.Fatalf("ListProviderModels: %v", err)
	}
	if resp.Warning != "" {
		t.Errorf("unexpected warning: %q", resp.Warning)
	}
	if len(resp.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(resp.Models))
	}
	if resp.Models[0].ID != "mock-model-1" || resp.Models[1].ID != "mock-model-2" {
		t.Errorf("unexpected models: %+v", resp.Models)
	}
}
