package builtin_test

import (
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/hooks/builtin"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestDefinitionsAreShipShape is the release guard for the embedded builtin
// registry: the moment a definition lands with a duplicate key, a bad
// version, or an invalid hook body, this test fails instead of the next
// server startup. It validates exactly what the sync will materialize —
// instance-level rules with key/version/source applied.
func TestDefinitionsAreShipShape(t *testing.T) {
	seen := make(map[string]int, len(builtin.All()))
	for _, def := range builtin.All() {
		if def.Key == "" {
			t.Fatalf("builtin definition with empty key (version %d)", def.Version)
		}
		if prev, dup := seen[def.Key]; dup {
			t.Fatalf("duplicate builtin key %q (versions %d and %d); keys must be unique", def.Key, prev, def.Version)
		}
		seen[def.Key] = def.Version

		if def.Version < 1 {
			t.Fatalf("builtin %q: version %d must be >= 1", def.Key, def.Version)
		}
		if def.Hook.ID != "" || def.Hook.Source != "" || def.Hook.Status != "" {
			t.Fatalf("builtin %q: ID, Source, and Status are set by the sync, not by the definition", def.Key)
		}
		if !def.Hook.Enabled {
			t.Fatalf("builtin %q: shipped definitions must be enabled", def.Key)
		}

		// The definition body against the instance-level rules...
		if err := domain.ValidateHook(domain.HookLevelInstance, &def.Hook.HookBase, "", nil); err != nil {
			t.Fatalf("builtin %q v%d: %v", def.Key, def.Version, err)
		}
		// ...and the full materialized row the sync will hand the store.
		materialized := def.Hook
		materialized.Key = def.Key
		materialized.Version = def.Version
		materialized.Source = domain.HookSourceBuiltin
		if err := materialized.Validate(nil); err != nil {
			t.Fatalf("builtin %q v%d: %v", def.Key, def.Version, err)
		}
	}
}
