// Package builtin holds the builtin instance-hook definitions embedded in the
// binary and materialized into instance_hooks rows by
// bootstrap.SyncBuiltinHooks on every server start (design.md D15). v1 ships
// ZERO builtins — the pipeline exists so a security release can add one by
// appending a Definition here, building, and restarting.
//
// The update story is system-skills equivalence: edit a definition in this
// repo, bump its Version, build, restart — live. Content changes never need
// migrations, and the store's version-guard upsert never downgrades a newer
// stored row during a rolling deploy.
//
// This package is data only: it describes WHAT ships, never how rows are
// persisted (the sync in internal/bootstrap owns that).
package builtin

import (
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Definition is one shipped builtin instance hook.
type Definition struct {
	// Key is the stable identifier, immutable across versions. Together with
	// source='builtin' it is the identity the version-guard upsert conflicts
	// on; a removed key deletes its row on the next sync.
	Key string
	// Version gates content updates: bump it whenever the hook's definition
	// changes. The store applies an update only when the stored version is
	// older; re-shipping the same version is an idempotent no-op and a newer
	// stored row is never downgraded (D15).
	Version int
	// Hook is the definition body: Name, Event, Matcher, HandlerType, Config,
	// TimeoutMS, OnFailure, and Enabled=true. ID, Source, and Status are set
	// by the sync, not here.
	Hook domain.InstanceHook
}

// All returns every builtin instance hook shipped with this binary, in
// definition order (which becomes the materialized list order). v1 ships an
// empty set.
func All() []Definition {
	return nil
}
