package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents/tools"
	"github.com/oniharnantyo/buddies/internal/store"
)

// storeBrowserConfigSource adapts the store's tool_settings table into the
// tools package's BrowserConfigSource seam (spec add-browser-tool D9): the
// browser config lives under the tool key "browser" as opaque JSON owned by
// the feature reading it. The adapter lives inside internal/agents so the
// tools package stays free of store imports.
type storeBrowserConfigSource struct {
	settings store.ToolSettingStore
}

// browserConfigJSON is the stored document's shape. Pointer fields separate
// a key that is missing (keep the default) from one stored explicitly (even
// at its zero value), so a saved headless:false survives the decode. The
// services package decodes the same document with the same rule
// (services/settings_browser.go) — keep the two in step.
type browserConfigJSON struct {
	DefaultProfile       *string `json:"defaultProfile"`
	Headless             *bool   `json:"headless"`
	IdleTimeoutSeconds   *int    `json:"idleTimeoutSeconds"`
	ActionTimeoutSeconds *int    `json:"actionTimeoutSeconds"`
	AllowPrivate         *bool   `json:"allowPrivate"`
}

// BrowserConfig implements tools.BrowserConfigSource. A missing row — or
// stored keys left out — falls back to the defaults (spec add-browser-tool
// D9); an explicitly stored value always wins.
func (s *storeBrowserConfigSource) BrowserConfig(ctx context.Context) (tools.BrowserConfig, error) {
	raw, ok, err := s.settings.Get(ctx, tools.NameBrowser)
	if err != nil {
		return tools.BrowserConfig{}, fmt.Errorf("read browser settings: %w", err)
	}
	if !ok {
		return tools.DefaultBrowserConfig(), nil
	}
	var stored browserConfigJSON
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return tools.BrowserConfig{}, fmt.Errorf("decode browser settings: %w", err)
	}
	cfg := tools.DefaultBrowserConfig()
	if stored.DefaultProfile != nil {
		cfg.DefaultProfile = *stored.DefaultProfile
	}
	if stored.Headless != nil {
		cfg.Headless = *stored.Headless
	}
	if stored.IdleTimeoutSeconds != nil {
		cfg.IdleTimeoutSeconds = *stored.IdleTimeoutSeconds
	}
	if stored.ActionTimeoutSeconds != nil {
		cfg.ActionTimeoutSeconds = *stored.ActionTimeoutSeconds
	}
	if stored.AllowPrivate != nil {
		cfg.AllowPrivate = *stored.AllowPrivate
	}
	return cfg, nil
}
