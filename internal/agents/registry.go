package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/buddies/internal/store"
)

// DefaultMaxTokens is the per-turn output cap applied when the agent pins
// none (the claude connector requires a value).
const DefaultMaxTokens = 4096

// ModelOverride carries one turn's model and reasoning-effort overrides over
// the agent's configuration (design D1: the session row's columns, resolved
// in Send). Empty fields inherit — the agent's model runs, and no effort
// parameter is set at all (provider default, never a silent substitute).
type ModelOverride struct {
	Model  string
	Effort string
}

// modelRegistry builds the eino chat model an agent's turn runs on, from the
// agent's configured provider in the store. It is deliberately unexported:
// eino types must not cross the package boundary (see doc.go).
type modelRegistry interface {
	// Build returns the chat model for the agent's provider and model with
	// the override applied; a typed error (ErrProviderNotConfigured,
	// ErrProviderUnsupported, ErrModelNotListed, ErrEffortUnsupported) when
	// the provider cannot serve the agent or the override is not valid for
	// it.
	Build(ctx context.Context, agent store.Agent, override ModelOverride) (model.BaseModel[*schema.AgenticMessage], error)
	// Models lists the catalog's model ids for a provider type, sorted.
	Models(providerType string) []string
}

// storeModelRegistry is the store-backed modelRegistry.
type storeModelRegistry struct {
	store    store.Store
	resolver *CapabilityResolver
}

func newModelRegistry(s store.Store) modelRegistry {
	return &storeModelRegistry{
		store:    s,
		resolver: NewCapabilityResolver(s),
	}
}

func (r *storeModelRegistry) Build(ctx context.Context, agent store.Agent, override ModelOverride) (model.BaseModel[*schema.AgenticMessage], error) {
	if agent.ProviderID == "" {
		return nil, fmt.Errorf("%w: agent %q has no provider selected", ErrProviderNotConfigured, agent.Name)
	}

	provider, err := r.store.Providers().ByID(ctx, agent.ProviderID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: provider %q of agent %q does not exist", ErrProviderNotConfigured, agent.ProviderID, agent.Name)
		}
		return nil, fmt.Errorf("load provider %q: %w", agent.ProviderID, err)
	}
	if !provider.Enabled {
		return nil, fmt.Errorf("%w: provider %q is disabled", ErrProviderNotConfigured, provider.Name)
	}
	if RequiresAPIKey(provider.Type) && provider.APIKey == "" {
		return nil, fmt.Errorf("%w: provider %q has no API key", ErrProviderNotConfigured, provider.Name)
	}
	if !IsKnownProviderType(provider.Type) {
		return nil, fmt.Errorf("%w: %q", ErrProviderUnsupported, provider.Type)
	}

	// Effective model: the override replaces the agent's configured model.
	// Validated against the provider's enabled stored models (design D6:
	// replacing the free-form allowance; an unknown or disabled model fails
	// the turn with a clear per-turn error naming the provider).
	modelName := agent.Model
	if override.Model != "" {
		modelName = override.Model
	}
	if err := r.checkModelEnabled(ctx, provider, modelName); err != nil {
		return nil, err
	}
	if err := r.checkEffort(ctx, agent.ProviderID, provider.Type, modelName, override.Effort); err != nil {
		return nil, err
	}

	maxTokens := agent.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	return buildModel(ctx, provider.Type, *provider, modelName, maxTokens, override.Effort)
}

// checkModelEnabled rejects a model selection (from agent configuration or a
// session override) that is not in the provider's enabled stored rows (design D6).
// An unlisted or disabled model fails with a clear per-turn error naming the
// provider, replacing the free-form allowance.
func (r *storeModelRegistry) checkModelEnabled(ctx context.Context, provider *store.Provider, modelName string) error {
	provName := provider.Name
	if provName == "" {
		provName = provider.ID
	}
	if modelName == "" {
		return fmt.Errorf("%w: no model specified for provider %q", ErrModelNotListed, provName)
	}
	if r.store == nil {
		return nil
	}
	rows, err := r.store.ProviderModels().ByProvider(ctx, provider.ID)
	if err != nil {
		return fmt.Errorf("list models for provider %q: %w", provName, err)
	}
	for _, row := range rows {
		if row.ModelID == modelName {
			if !row.Enabled {
				return fmt.Errorf("%w: model %q is disabled on provider %q: %w", ErrModelNotListed, modelName, provName, ErrModelDisabled)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: model %q is not in the provider %q model listing", ErrModelNotListed, modelName, provName)
}

// checkEffort rejects a reasoning effort the effective model's capability
// set does not include (spec agent-runtime: rejected, not silently dropped
// or sent anyway). An empty effort inherits the provider default, and an
// unknown provider type is left to buildModel's own rejection.
func checkEffort(providerType, model, effort string) error {
	if effort == "" || !IsKnownProviderType(providerType) {
		return nil
	}
	for _, supported := range Efforts(providerType, model) {
		if supported == effort {
			return nil
		}
	}
	return fmt.Errorf("%w: model %q does not support reasoning effort %q", ErrEffortUnsupported, model, effort)
}

func (r *storeModelRegistry) checkEffort(ctx context.Context, providerID, providerType, model, effort string) error {
	if effort == "" {
		return nil
	}
	resolver := r.resolver
	if resolver == nil {
		resolver = NewCapabilityResolver(r.store)
	}
	caps, err := resolver.Resolve(ctx, providerID, model)
	if err != nil && !errors.Is(err, ErrModelDisabled) {
		return checkEffort(providerType, model, effort)
	}
	for _, supported := range caps.Efforts {
		if supported == effort {
			return nil
		}
	}
	return fmt.Errorf("%w: model %q does not support reasoning effort %q", ErrEffortUnsupported, model, effort)
}

func (r *storeModelRegistry) Models(providerType string) []string {
	return CatalogModels(providerType)
}
