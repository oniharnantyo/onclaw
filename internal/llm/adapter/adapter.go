package adapter

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Adapter defines the contract for constructing a model.ToolCallingChatModel.
type Adapter interface {
	Build(ctx context.Context, p *store.Profile, model string, apiKey string) (model.ToolCallingChatModel, error)
}

// AdapterFactory creates an Adapter.
type AdapterFactory func() Adapter

// IsKeyless reports whether the provider type does not require an API key.
// Local servers such as Ollama serve requests without authentication, so an
// empty key is acceptable for them.
func IsKeyless(providerType string) bool {
	return providerType == "ollama"
}
