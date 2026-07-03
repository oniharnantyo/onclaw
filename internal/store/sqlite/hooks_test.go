package sqlite_test

import (
	"context"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestHookStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	hs := sqlite.NewHookStore(db)
	es := sqlite.NewHookExecutionStore(db)

	// Test adding invalid hooks
	if err := hs.AddHook(ctx, &store.Hook{ID: "", Name: "test"}); err == nil {
		t.Error("expected error for empty hook ID")
	}
	if err := hs.AddHook(ctx, &store.Hook{ID: "h1", Name: ""}); err == nil {
		t.Error("expected error for empty hook Name")
	}

	// Test adding valid hook
	h := &store.Hook{
		ID:          "hook-123",
		Name:        "test-hook",
		Scope:       "global",
		Event:       "pre_tool_use",
		HandlerType: "command",
		Config:      `{"command":"echo test"}`,
		Matcher:     "exec",
		TimeoutMS:   5000,
		OnTimeout:   "block",
		Priority:    10,
		Enabled:     1,
	}

	if err := hs.AddHook(ctx, h); err != nil {
		t.Fatalf("failed to AddHook: %v", err)
	}

	// Get hook
	gotH, err := hs.GetHook(ctx, h.ID)
	if err != nil {
		t.Fatalf("failed to GetHook: %v", err)
	}
	if gotH.ID != h.ID || gotH.Name != h.Name || gotH.Scope != h.Scope || gotH.Event != h.Event || gotH.HandlerType != h.HandlerType || gotH.Config != h.Config || gotH.Matcher != h.Matcher || gotH.TimeoutMS != h.TimeoutMS || gotH.OnTimeout != h.OnTimeout || gotH.Priority != h.Priority || gotH.Enabled != h.Enabled {
		t.Errorf("hook fields mismatch. got: %+v, want: %+v", gotH, h)
	}

	// List hooks
	list, err := hs.ListHooks(ctx)
	if err != nil {
		t.Fatalf("failed to ListHooks: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 hook, got %d", len(list))
	}

	// List by scope and event
	listSE, err := hs.ListHooksByScopeAndEvent(ctx, "global", "pre_tool_use")
	if err != nil {
		t.Fatalf("failed to ListHooksByScopeAndEvent: %v", err)
	}
	if len(listSE) != 1 {
		t.Errorf("expected 1 hook for global/pre_tool_use, got %d", len(listSE))
	}

	// Update hook
	h.Priority = 20
	h.Config = `{"command":"updated"}`
	if err := hs.UpdateHook(ctx, h); err != nil {
		t.Fatalf("failed to UpdateHook: %v", err)
	}
	gotH2, _ := hs.GetHook(ctx, h.ID)
	if gotH2.Priority != 20 || gotH2.Config != `{"command":"updated"}` {
		t.Error("update hook fields not saved correctly")
	}

	// Toggle hook
	if err := hs.ToggleHook(ctx, h.ID, false); err != nil {
		t.Fatalf("failed to ToggleHook: %v", err)
	}
	gotH3, _ := hs.GetHook(ctx, h.ID)
	if gotH3.Enabled != 0 {
		t.Error("expected hook to be disabled")
	}

	// Log executions
	exec := &store.HookExecution{
		HookID:      h.ID,
		Event:       "pre_tool_use",
		HandlerType: "command",
		Decision:    "allow",
		DurationMS:  120,
		Error:       "",
	}
	if err := es.AppendExecution(ctx, exec); err != nil {
		t.Fatalf("failed to AppendExecution: %v", err)
	}

	execList, err := es.ListExecutions(ctx)
	if err != nil {
		t.Fatalf("failed to ListExecutions: %v", err)
	}
	if len(execList) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execList))
	}
	if execList[0].HookID != h.ID || execList[0].Decision != "allow" {
		t.Errorf("execution details mismatch: %+v", execList[0])
	}

	// Delete hook & assert execution stays (with hook_id NULL/empty)
	if err := hs.RemoveHook(ctx, h.ID); err != nil {
		t.Fatalf("failed to RemoveHook: %v", err)
	}
	_, err = hs.GetHook(ctx, h.ID)
	if err == nil {
		t.Error("expected error getting deleted hook")
	}

	execList2, err := es.ListExecutions(ctx)
	if err != nil {
		t.Fatalf("failed to ListExecutions after delete: %v", err)
	}
	if len(execList2) != 1 {
		t.Fatalf("expected execution to survive hook deletion, got %d", len(execList2))
	}
	if execList2[0].HookID != "" {
		t.Errorf("expected HookID to be empty/null, got %q", execList2[0].HookID)
	}
}
