package tools_test

import (
	"context"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
)

func TestToolCategoryAndConfigRegistry(t *testing.T) {
	// 1. Verify all registered tools return non-empty Category
	registeredTools := tools.GetRegistry()
	if len(registeredTools) == 0 {
		t.Error("expected at least one registered tool, got 0")
	}

	for _, tool := range registeredTools {
		cat := tool.Category()
		if cat == "" {
			t.Errorf("tool %q returned empty category", tool.Name())
		}
	}

	// 2. Verify ConfigRegistry
	cat := "dummy_category"
	schema := `{"type": "object"}`

	loaded := false
	saved := false

	loadFn := func(ctx context.Context, cfg string) error {
		loaded = true
		return nil
	}
	saveFn := func(ctx context.Context) (string, error) {
		saved = true
		return "{}", nil
	}

	tools.RegisterConfig(cat, schema, loadFn, saveFn)

	if !tools.IsConfigurable(cat) {
		t.Errorf("expected category %q to be configurable", cat)
	}

	if tools.IsConfigurable("non_existent") {
		t.Error("expected unknown category to not be configurable")
	}

	cats := tools.ConfigurableCategories()
	found := false
	for _, c := range cats {
		if c == cat {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected category %q in configurable categories list", cat)
	}

	entry, ok := tools.GetConfigEntry(cat)
	if !ok {
		t.Fatalf("failed to get config entry for %q", cat)
	}
	if entry.JSONSchema != schema {
		t.Errorf("expected schema %q, got %q", schema, entry.JSONSchema)
	}

	ctx := context.Background()
	if err := entry.Load(ctx, "{}"); err != nil {
		t.Fatalf("unexpected error on Load: %v", err)
	}
	if !loaded {
		t.Error("load function was not executed")
	}

	cfg, err := entry.Save(ctx)
	if err != nil {
		t.Fatalf("unexpected error on Save: %v", err)
	}
	if cfg != "{}" {
		t.Errorf("expected %q, got %q", "{}", cfg)
	}
	if !saved {
		t.Error("save function was not executed")
	}
}
