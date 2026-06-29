package adapter

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestAdapterRegistry(t *testing.T) {
	r := NewRegistry()

	// 1. Get before register should fail
	_, err := r.Get("openai")
	if err == nil {
		t.Error("expected Get to fail before Register, got nil")
	}

	// 2. Register and Get
	r.Register("openai", func() Adapter {
		return NewStubAdapter()
	})

	ad, err := r.Get("openai")
	if err != nil {
		t.Fatalf("Register/Get failed: %v", err)
	}

	ctx := context.Background()
	p := &store.Profile{
		Name:         "test-openai",
		ProviderType: "openai",
	}

	cm, err := ad.Build(ctx, p, "gpt-4", "test-key")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if cm == nil {
		t.Error("expected non-nil ChatModel")
	}

	// Verify stub interface works
	if _, err := cm.WithTools(nil); err != nil {
		t.Errorf("WithTools failed: %v", err)
	}
	_, _ = cm.Generate(ctx, nil)
	_, _ = cm.Stream(ctx, nil)
}
