package bootstrap

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/oniharnantyo/onclaw/internal/agents/hooks/builtin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// builtinHookNameNamespace is the UUID namespace builtin hook IDs derive
// from. instance_hooks.id is a uuid column (migration 000026), so the sync
// mints uuid5(namespace, key): every pod and every start computes the same
// primary key for the same shipped key.
const builtinHookNameNamespace = "urn:onclaw:builtin-instance-hook:"

// SyncBuiltinHooks materializes the builtin instance-hook definitions shipped
// with this binary into source=builtin instance_hooks rows (design.md D15):
// a version-guard upsert per definition (idempotent, race-safe under
// concurrent server starts, never downgrades a newer stored row), then a
// deletion pass removing builtin rows this binary no longer ships.
// hook_executions survive deletions via ON DELETE SET NULL plus the
// denormalized hook name. Managed rows are never touched.
func SyncBuiltinHooks(ctx context.Context, st store.Store) error {
	return syncBuiltinHookDefs(ctx, st, builtin.All())
}

// syncBuiltinHookDefs is SyncBuiltinHooks over an explicit definition set;
// the seam exists so tests can exercise upgrade/removal behavior against
// definitions without shipping them in the binary.
func syncBuiltinHookDefs(ctx context.Context, st store.Store, defs []builtin.Definition) error {
	hooks := st.Hooks()

	keys := make([]string, 0, len(defs))
	for _, def := range defs {
		hook := def.Hook
		hook.Key = def.Key
		hook.Version = def.Version
		hook.Source = domain.HookSourceBuiltin
		hook.ID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(builtinHookNameNamespace+def.Key)).String()
		if err := hooks.UpsertBuiltinHook(ctx, &hook); err != nil {
			return fmt.Errorf("upsert builtin instance hook %q (v%d): %w", def.Key, def.Version, err)
		}
		keys = append(keys, def.Key)
	}

	// Only after every shipped key is in place: builtin rows whose key this
	// binary no longer ships are deleted. An empty registry therefore removes
	// every builtin row, which is correct — v1 ships none.
	if err := hooks.DeleteMissingBuiltinHooks(ctx, keys); err != nil {
		return fmt.Errorf("delete missing builtin instance hooks: %w", err)
	}
	return nil
}
