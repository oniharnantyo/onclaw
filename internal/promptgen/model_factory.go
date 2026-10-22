package promptgen

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/buddies/internal/store"
	"google.golang.org/genai"
)

// Provider type names, kept in sync with internal/agents/catalog.go — the
// source of truth. promptgen deliberately does not import the runtime package:
// generation needs only these names, and staying decoupled keeps the package
// buildable while the runtime churns.
const (
	providerAnthropic           = "anthropic"
	providerOpenAI              = "openai"
	providerGemini              = "gemini"
	providerOpenRouter          = "openrouter"
	providerOpenAICompatible    = "openai-compatible"
	providerAnthropicCompatible = "anthropic-compatible"
)

// knownProviderTypes mirrors internal/agents.KnownProviderTypes; test-only
// consumers iterate it.
var knownProviderTypes = []string{
	providerAnthropic,
	providerOpenAI,
	providerGemini,
	providerOpenRouter,
	providerOpenAICompatible,
	providerAnthropicCompatible,
}

// errProviderUnsupported mirrors internal/agents.ErrProviderUnsupported; the
// services layer maps it onto the runtime sentinel when wiring generations.
var errProviderUnsupported = errors.New("provider type not supported")

// defaultModelFactory is the production ModelFactory: it builds the eino chat
// model for one generation from the agent's own provider configuration.
type defaultModelFactory struct{}

// NewDefaultModelFactory returns the production ModelFactory: it switches on
// the provider type and builds the matching eino-ext model with the hardcoded
// generation parameters (GenerationTemperature; GenerationMaxTokens where the
// adapter requires an explicit cap — Anthropic's API contract needs one). The
// schema bindings mirror the runtime connectors' base-URL conventions: the
// OpenAI connector takes versioned roots (https://host/v1), the claude
// connector takes origins (https://host), Gemini takes the bare origin.
func NewDefaultModelFactory() ModelFactory { return defaultModelFactory{} }

// Build implements ModelFactory.
func (defaultModelFactory) Build(ctx context.Context, provider store.Provider, modelName string) (model.BaseChatModel, error) {
	temp := GenerationTemperature
	maxTokens := GenerationMaxTokens

	switch strings.TrimSpace(provider.Type) {
	case providerOpenAI:
		cfg := &openai.ChatModelConfig{
			APIKey:         provider.APIKey,
			Model:          modelName,
			Temperature:    &temp,
			ResponseFormat: structuredOutputFormat(),
		}
		if provider.BaseURL != "" {
			cfg.BaseURL = provider.BaseURL
		}
		m, err := openai.NewChatModel(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("build openai model %q: %w", modelName, err)
		}
		return m, nil

	case providerAnthropic:
		cfg := &claude.Config{
			APIKey:         provider.APIKey,
			Model:          modelName,
			MaxTokens:      maxTokens,
			Temperature:    &temp,
			ResponseFormat: &claude.ResponseFormat{Schema: promptsJSONSchema()},
		}
		if provider.BaseURL != "" {
			baseURL := provider.BaseURL
			cfg.BaseURL = &baseURL
		}
		m, err := claude.NewChatModel(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("build claude model %q: %w", modelName, err)
		}
		return m, nil

	case providerGemini:
		clientCfg := &genai.ClientConfig{
			APIKey:  provider.APIKey,
			Backend: genai.BackendGeminiAPI,
		}
		if provider.BaseURL != "" {
			clientCfg.HTTPOptions = genai.HTTPOptions{BaseURL: provider.BaseURL}
		}
		client, err := genai.NewClient(ctx, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("build gemini client: %w", err)
		}
		m, err := gemini.NewChatModel(ctx, &gemini.Config{
			Client:      client,
			Model:       modelName,
			Temperature: &temp,
		})
		if err != nil {
			return nil, fmt.Errorf("build gemini model %q: %w", modelName, err)
		}
		return m, nil

	case providerOpenRouter:
		// OpenRouter's root is fixed: when the provider leaves the base
		// blank, substitute it rather than falling through to the OpenAI
		// SDK default (api.openai.com). Same root the runtime applies.
		baseURL := provider.BaseURL
		if baseURL == "" {
			baseURL = "https://openrouter.ai/api/v1"
		}
		m, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:         provider.APIKey,
			BaseURL:        baseURL,
			Model:          modelName,
			Temperature:    &temp,
			ResponseFormat: structuredOutputFormat(),
		})
		if err != nil {
			return nil, fmt.Errorf("build openrouter model %q: %w", modelName, err)
		}
		return m, nil

	case providerOpenAICompatible:
		m, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:         provider.APIKey,
			BaseURL:        provider.BaseURL,
			Model:          modelName,
			Temperature:    &temp,
			ResponseFormat: structuredOutputFormat(),
		})
		if err != nil {
			return nil, fmt.Errorf("build openai-compatible model %q: %w", modelName, err)
		}
		return m, nil

	case providerAnthropicCompatible:
		cfg := &claude.Config{
			APIKey:         provider.APIKey,
			Model:          modelName,
			MaxTokens:      maxTokens,
			Temperature:    &temp,
			ResponseFormat: &claude.ResponseFormat{Schema: promptsJSONSchema()},
		}
		if provider.BaseURL != "" {
			baseURL := provider.BaseURL
			cfg.BaseURL = &baseURL
		}
		m, err := claude.NewChatModel(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("build anthropic-compatible model %q: %w", modelName, err)
		}
		return m, nil

	default:
		return nil, fmt.Errorf("%w: unsupported provider type %q", errProviderUnsupported, provider.Type)
	}
}
