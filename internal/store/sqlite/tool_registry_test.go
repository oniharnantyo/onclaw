package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestToolRegistryStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Since setupTestDB already runs sqlite.Migrate(), the tool_registry should be seeded.
	tr := sqlite.NewToolRegistryStore(db)

	list, err := tr.ListTools(ctx)
	if err != nil {
		t.Fatalf("failed to list tools: %v", err)
	}

	// Verify seeding works (should contain at least shell, read_file, write_file, list_dir)
	expectedTools := map[string]string{
		"shell":      "Shell",
		"read_file":  "Filesystem",
		"write_file": "Filesystem",
		"list_dir":   "Filesystem",
	}

	for name, cat := range expectedTools {
		found := false
		for _, tool := range list {
			if tool.Name == name {
				found = true
				if tool.Category != cat {
					t.Errorf("expected tool %q category to be %q, got %q", name, cat, tool.Category)
				}
				if tool.Enabled != 1 {
					t.Errorf("expected tool %q to be enabled by default, got %d", name, tool.Enabled)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected tool %q to be seeded", name)
		}
	}

	// Test GetTool
	shellTool, err := tr.GetTool(ctx, "shell")
	if err != nil {
		t.Fatalf("failed to get tool 'shell': %v", err)
	}
	if shellTool.Name != "shell" || shellTool.Category != "Shell" || shellTool.Enabled != 1 {
		t.Errorf("unexpected tool values: %+v", shellTool)
	}

	// Test GetTool on nonexistent
	_, err = tr.GetTool(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error getting nonexistent tool, got nil")
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	// Test ToggleTool off
	err = tr.ToggleTool(ctx, "shell", false)
	if err != nil {
		t.Fatalf("failed to toggle tool off: %v", err)
	}

	shellTool, err = tr.GetTool(ctx, "shell")
	if err != nil {
		t.Fatalf("failed to get tool 'shell': %v", err)
	}
	if shellTool.Enabled != 0 {
		t.Errorf("expected shell tool to be disabled, got %d", shellTool.Enabled)
	}

	// Test ToggleTool nonexistent
	err = tr.ToggleTool(ctx, "nonexistent", true)
	if err == nil {
		t.Error("expected error toggling nonexistent tool, got nil")
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	// Test idempotency of sqlite.Migrate and seeding
	// First let's verify migrating again doesn't reset user disabled toggle
	err = sqlite.Migrate(db)
	if err != nil {
		t.Fatalf("failed to run sqlite.Migrate: %v", err)
	}

	shellTool, err = tr.GetTool(ctx, "shell")
	if err != nil {
		t.Fatalf("failed to get tool 'shell' after migration: %v", err)
	}
	if shellTool.Enabled != 0 {
		t.Errorf("expected shell tool to remain disabled after migration, got %d", shellTool.Enabled)
	}

	// Test UpsertTool
	newTool := &store.ToolRegistry{
		Name:     "custom_tool",
		Category: "Custom",
		Enabled:  1,
	}
	err = tr.UpsertTool(ctx, newTool)
	if err != nil {
		t.Fatalf("failed to upsert tool: %v", err)
	}

	gotTool, err := tr.GetTool(ctx, "custom_tool")
	if err != nil {
		t.Fatalf("failed to get upserted tool: %v", err)
	}
	if gotTool.Name != newTool.Name || gotTool.Category != newTool.Category || gotTool.Enabled != newTool.Enabled {
		t.Errorf("upserted tool mismatch, got: %+v", gotTool)
	}

	// Test UpsertTool empty name
	err = tr.UpsertTool(ctx, &store.ToolRegistry{Name: ""})
	if err == nil {
		t.Error("expected error upserting empty name, got nil")
	}
}

func TestToolGroupConfigStore(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	tc := sqlite.NewToolGroupConfigStore(db)

	// Test GetConfig on nonexistent category
	_, err := tc.GetConfig(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error getting config for nonexistent category, got nil")
	} else if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	// Test PutConfig and GetConfig (json config round-trip)
	category := "Filesystem"
	configJSON := `{"max_file_size": 1048576}`

	err = tc.PutConfig(ctx, category, configJSON)
	if err != nil {
		t.Fatalf("failed to put config: %v", err)
	}

	gotCfg, err := tc.GetConfig(ctx, category)
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	if gotCfg.Category != category {
		t.Errorf("expected category %q, got %q", category, gotCfg.Category)
	}
	if gotCfg.Config != configJSON {
		t.Errorf("expected config %q, got %q", configJSON, gotCfg.Config)
	}
	if gotCfg.CreatedAt == "" || gotCfg.UpdatedAt == "" {
		t.Error("expected CreatedAt and UpdatedAt to be set")
	}

	// Test PutConfig empty category
	err = tc.PutConfig(ctx, "", "{}")
	if err == nil {
		t.Error("expected error putting config with empty category, got nil")
	}
}
