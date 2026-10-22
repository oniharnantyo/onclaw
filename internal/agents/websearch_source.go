package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents/tools"
	"github.com/oniharnantyo/buddies/internal/store"
)

// storeSearchConfigSource adapts the store's tool_settings table into the
// tools package's SearchConfigSource seam (design D8): the web_search chain
// config lives under the tool key "web_search" as opaque JSON owned by the
// feature reading it. The adapter lives inside internal/agents so the tools
// package stays free of store imports.
type storeSearchConfigSource struct {
	settings store.ToolSettingStore
}

// WebSearchConfig implements tools.SearchConfigSource. A missing row is the
// unconfigured state: an empty config, which the chain builder turns into the
// not-configured error result at first invocation (spec agent-tools "No
// provider configured").
func (s *storeSearchConfigSource) WebSearchConfig(ctx context.Context) (tools.WebSearchConfig, error) {
	raw, ok, err := s.settings.Get(ctx, tools.NameWebSearch)
	if err != nil {
		return tools.WebSearchConfig{}, fmt.Errorf("read web_search settings: %w", err)
	}
	if !ok {
		return tools.WebSearchConfig{}, nil
	}
	var cfg tools.WebSearchConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return tools.WebSearchConfig{}, fmt.Errorf("decode web_search settings: %w", err)
	}
	return cfg, nil
}
