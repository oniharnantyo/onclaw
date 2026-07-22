package adapter

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/model"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// Adapter defines the contract for constructing a model.ToolCallingChatModel.
type Adapter interface {
	Build(ctx context.Context, p *store.Profile, model string, apiKey string) (model.AgenticModel, error)
}

// AdapterFactory creates an Adapter.
type AdapterFactory func() Adapter

// IsKeyless reports whether the provider type does not require an API key.
// Local servers such as Ollama serve requests without authentication, so an
// empty key is acceptable for them.
func IsKeyless(providerType string) bool {
	return providerType == "ollama"
}

// PromptCachingEnabled reports whether prompt/context caching should be enabled
// for the profile (design Layer D). Caching is additive: when absent it only
// means the verbatim prefix is re-billed, never incorrect, so it defaults ON
// and is disabled by setting "prompt_caching": false in the profile Settings.
// A malformed Settings blob falls back to enabled rather than failing the build.
func PromptCachingEnabled(p *store.Profile) bool {
	if p.Settings == "" {
		return true
	}
	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(p.Settings), &settings); err != nil {
		return true
	}
	if v, ok := settings["prompt_caching"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
}
