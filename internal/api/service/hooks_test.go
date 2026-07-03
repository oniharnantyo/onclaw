package service_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestService_AddHook_WithProvidedID(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Add hook with non-empty ID
	_, err := f.svc.AddHook(ctx, &store.Hook{
		ID:          "custom-hook-id",
		Name:        "test-hook",
		Scope:       "global",
		Event:       "user_prompt_submit",
		HandlerType: "log",
	})
	if err != nil {
		t.Fatalf("AddHook: %v", err)
	}

	h, err := f.svc.GetHook(ctx, "custom-hook-id")
	if err != nil {
		t.Fatalf("GetHook: %v", err)
	}
	if h.ID != "custom-hook-id" {
		t.Errorf("expected ID 'custom-hook-id', got %q", h.ID)
	}
}

func TestService_AddHook_And_ListHooks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.svc.AddHook(ctx, &store.Hook{
		Name:        "h1",
		Scope:       "global",
		Event:       "pre_tool_use",
		HandlerType: "filter",
	})
	if err != nil {
		t.Fatalf("AddHook: %v", err)
	}

	list, err := f.svc.ListHooks(ctx)
	if err != nil {
		t.Fatalf("ListHooks: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(list))
	}
}

func TestService_GetHook(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	h, _ := f.svc.AddHook(ctx, &store.Hook{Name: "my-hook", Scope: "global", Event: "pre_tool_use"})

	view, err := f.svc.GetHook(ctx, h.ID)
	if err != nil {
		t.Fatalf("GetHook: %v", err)
	}
	if view.Name != "my-hook" {
		t.Errorf("unexpected name: %q", view.Name)
	}
}

func TestService_GetHook_NotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.GetHook(context.Background(), "ghost")
	if err == nil {
		t.Error("expected error for missing hook")
	}
}

func TestService_UpdateHook(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	h, _ := f.svc.AddHook(ctx, &store.Hook{Name: "old-hook", Scope: "global", Event: "pre_tool_use"})

	_, err := f.svc.UpdateHook(ctx, &store.Hook{
		ID:    h.ID,
		Name:  "new-hook",
		Scope: "agent",
		Event: "post_tool_use",
	})
	if err != nil {
		t.Fatalf("UpdateHook: %v", err)
	}

	v, _ := f.svc.GetHook(ctx, h.ID)
	if v.Name != "new-hook" {
		t.Errorf("expected updated name 'new-hook', got %q", v.Name)
	}
}

func TestService_RemoveHook(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	h, _ := f.svc.AddHook(ctx, &store.Hook{Name: "kill-me", Scope: "global", Event: "pre_tool_use"})
	if err := f.svc.RemoveHook(ctx, h.ID); err != nil {
		t.Fatalf("RemoveHook: %v", err)
	}

	_, err := f.svc.GetHook(ctx, h.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestService_ToggleHook(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	h, _ := f.svc.AddHook(ctx, &store.Hook{Name: "toggle", Scope: "global", Event: "pre_tool_use"})

	err := f.svc.ToggleHook(ctx, h.ID, true)
	if err != nil {
		t.Fatalf("ToggleHook: %v", err)
	}

	v, _ := f.svc.GetHook(ctx, h.ID)
	if v.Enabled != 1 {
		t.Error("expected hook to be enabled")
	}
}

func TestService_ListHookExecutions_Empty(t *testing.T) {
	f := newFixture(t)
	list, err := f.svc.ListHookExecutions(context.Background())
	if err != nil {
		t.Fatalf("ListHookExecutions: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 executions, got %d", len(list))
	}
}
