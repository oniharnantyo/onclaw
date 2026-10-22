package store

import "context"

// ToolSettingStore persists per-tool configuration as key–JSON rows
// (design D8). The settings value is an opaque JSON document owned by the
// feature reading it — today the web_search provider chain; absence of a
// row means the tool has no stored configuration.
type ToolSettingStore interface {
	// Set create-or-replaces the stored settings of the named tool,
	// atomically.
	Set(ctx context.Context, tool string, settings string) error
	// Get returns the stored settings of the named tool; ok is false when
	// the tool has no stored row.
	Get(ctx context.Context, tool string) (settings string, ok bool, err error)
}
