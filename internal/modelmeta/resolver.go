package modelmeta

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type CacheKey string

const OpenaiModelsCacheKey CacheKey = "openai_models_cache"

type ModelCache struct {
	OpenaiResponse *openaiModelsResponse
}

func applyBaseURLDefault(baseURL, providerType string) string {
	if baseURL != "" {
		return baseURL
	}
	providerType = strings.ToLower(providerType)
	switch providerType {
	case "ollama":
		return "http://localhost:11434/v1"
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	}
	return baseURL
}

// Enumerate returns the list of model IDs available from the provider API.
func Enumerate(ctx context.Context, providerType, baseURL, apiKey string) ([]string, error) {
	providerType = strings.ToLower(providerType)
	baseURL = applyBaseURLDefault(baseURL, providerType)
	if providerType == "ollama" {
		resp, err := FetchOllamaModels(ctx, baseURL)
		if err != nil {
			return nil, err
		}
		var models []string
		for _, m := range resp.Models {
			models = append(models, m.Name)
		}
		return models, nil
	}

	// openai, anthropic, openai-compatible
	resp, err := FetchOpenAIModels(ctx, baseURL, apiKey, providerType)
	if err != nil {
		return nil, err
	}
	var models []string
	for _, m := range resp.Data {
		models = append(models, m.ID)
	}
	return models, nil
}

// GetCatalog loads and parses the models.dev cache catalog.
func GetCatalog() (*ApiJSON, error) {
	data, err := LoadOrRefreshCatalog("https://models.dev/api.json")
	if err != nil {
		return nil, err
	}
	var catalog ApiJSON
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	return &catalog, nil
}

func getNativeContextWindow(ctx context.Context, modelID string, providerType string, baseURL string, apiKey string) int {
	providerType = strings.ToLower(providerType)
	baseURL = applyBaseURLDefault(baseURL, providerType)
	if providerType == "ollama" {
		resp, err := FetchOllamaShow(ctx, baseURL, modelID)
		if err == nil && resp != nil && resp.ModelInfo != nil {
			for key, val := range resp.ModelInfo {
				if strings.HasSuffix(key, ".context_length") {
					if num, ok := val.(float64); ok {
						return int(num)
					}
				}
			}
		}
	} else {
		// Try to read context_length field from /v1/models list if present
		resp, err := FetchOpenAIModels(ctx, baseURL, apiKey, providerType)
		if err == nil && resp != nil {
			for _, m := range resp.Data {
				if m.ID == modelID && m.ContextLength > 0 {
					return m.ContextLength
				}
			}
		}
	}
	return 0
}

func mapProviderType(providerType string) string {
	return strings.ToLower(providerType)
}

// Resolve resolves the metadata (context window, thinking, modalities) for a model.
func Resolve(ctx context.Context, modelID string, providerType string, baseURL string, apiKey string, catalog *ApiJSON) store.ModelMetadata {
	providerType = strings.ToLower(providerType)
	baseURL = applyBaseURLDefault(baseURL, providerType)
	var meta store.ModelMetadata
	meta.InputModalities = []string{"text"} // floor

	// 1. Try provider-native source
	nativeCW := getNativeContextWindow(ctx, modelID, providerType, baseURL, apiKey)
	if nativeCW > 0 {
		meta.ContextWindow = nativeCW
	}

	// 2. Try models.dev catalog
	var catalogFound bool
	var catalogMeta ModelObj
	if catalog != nil && catalog.Providers != nil {
		provID := mapProviderType(providerType)
		if prov, exists := catalog.Providers[provID]; exists {
			if m, exists := prov.Models[modelID]; exists {
				catalogMeta = m
				catalogFound = true
			}
		}

		// Global search across all providers on a provider-id miss or model-id miss
		if !catalogFound {
			for _, prov := range catalog.Providers {
				if m, exists := prov.Models[modelID]; exists {
					catalogMeta = m
					catalogFound = true
					break
				}
			}
		}
	}

	if catalogFound {
		if meta.ContextWindow == 0 {
			meta.ContextWindow = catalogMeta.Limit.Context
		}
		meta.Thinking = catalogMeta.Reasoning
		if len(catalogMeta.ReasoningOptions) > 0 {
			meta.ReasoningOptions = make([]store.ReasoningOption, len(catalogMeta.ReasoningOptions))
			for i, opt := range catalogMeta.ReasoningOptions {
				meta.ReasoningOptions[i] = store.ReasoningOption{
					Type:   opt.Type,
					Values: opt.Values,
					Min:    opt.Min,
					Max:    opt.Max,
				}
			}
		}
		if len(catalogMeta.Modalities.Input) > 0 {
			meta.InputModalities = catalogMeta.Modalities.Input
		}
	}

	// 3. Fallback check (floor)
	if len(meta.InputModalities) == 0 {
		meta.InputModalities = []string{"text"}
	}

	return meta
}
