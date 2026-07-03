package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestService_ListTools_Empty(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	list, err := f.svc.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 tools, got %d", len(list))
	}
}

func TestService_ListTools_WithRegisteredTool(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.toolStore.UpsertTool(ctx, &store.ToolRegistry{Name: "my_tool", Enabled: 1})

	list, err := f.svc.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(list) == 0 || len(list[0].Tools) == 0 || list[0].Tools[0].Name != "my_tool" {
		t.Errorf("unexpected list tools result: %v", list)
	}
}

func TestService_ToggleTool_NotFound(t *testing.T) {
	f := newFixture(t)
	err := f.svc.ToggleTool(context.Background(), "ghost", true)
	if err == nil {
		t.Error("expected error toggling missing tool")
	}
}

func TestService_ToggleTool_Found(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.toolStore.UpsertTool(ctx, &store.ToolRegistry{Name: "t1", Enabled: 0})

	err := f.svc.ToggleTool(ctx, "t1", true)
	if err != nil {
		t.Fatalf("ToggleTool: %v", err)
	}

	tl, _ := f.toolStore.GetTool(ctx, "t1")
	if tl.Enabled != 1 {
		t.Error("expected tool to be enabled in store")
	}
}

func TestService_GetCategoryConfig_NonConfigurableCategory(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.GetCategoryConfig(context.Background(), "InvalidCategory")
	if err == nil || !strings.Contains(err.Error(), "is not configurable") {
		t.Errorf("expected non-configurable error, got %v", err)
	}
}

func TestService_PutCategoryConfig_NonConfigurableCategory(t *testing.T) {
	f := newFixture(t)
	err := f.svc.PutCategoryConfig(context.Background(), "InvalidCategory", "{}")
	if err == nil || !strings.Contains(err.Error(), "is not configurable") {
		t.Errorf("expected non-configurable error, got %v", err)
	}
}
