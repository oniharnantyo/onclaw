package agents

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/buddies/internal/store"
	"google.golang.org/genai"
)

// Thinking-budget tiers per reasoning effort (design D2): the normalized
// effort options map onto each provider's native token budgets. Tunable
// constants — changing them alters no spec (the efforts are the contract,
// not the budgets).
var (
	claudeThinkingBudgets = map[string]int64{
		EffortLow:    4096,
		EffortMedium: 16384,
		EffortHigh:   32768,
	}
	geminiThinkingBudgets = map[string]int32{
		EffortLow:    512,
		EffortMedium: 2048,
		EffortHigh:   8192,
	}
)

// buildModel dispatches on the provider type to the matching eino
// connector. Anthropic (claude) is the verified end-to-end path (design D5);
// the OpenAI and Gemini adapters register and construct but are not required
// to pass runtime verification.
//
// Effort (design D2) maps to each connector's native reasoning parameter,
// applied at construction: every pinned connector defaults its per-call
// option (openai.WithReasoningEffort / claude.WithThinkingConfig /
// gemini.WithThinkingConfig) from these same config fields, so a turn model
// built once carries the knob on every model call — delegated children
// included — with no per-call plumbing. An empty effort leaves the field
// zero and the provider request omits the parameter entirely (the provider
// default), never a silent substitute.
//
// Base URL conventions across the connectors: the OpenAI connector
// (go-openai) treats BaseURL as an OpenAI-style root that already includes
// the version suffix — request paths are appended directly, and the default
// is https://api.openai.com/v1 — so openrouter and openai-compatible bases
// look like https://host/v1. The claude connector (anthropic-sdk-go) treats
// BaseURL as the origin and appends /v1 itself — the default is
// https://api.anthropic.com — so anthropic-compatible bases look like
// https://host with no suffix.
func buildModel(ctx context.Context, providerType string, provider store.Provider, modelName string, maxTokens int, effort string) (model.BaseChatModel, error) {
	switch providerType {
	case ProviderAnthropic, ProviderAnthropicCompatible:
		return buildClaudeModel(ctx, provider, modelName, maxTokens, effort)
	case ProviderOpenAI, ProviderOpenRouter, ProviderOpenAICompatible:
		// OpenRouter's root is fixed: when the provider leaves the base
		// blank, substitute it rather than falling through to the OpenAI
		// SDK default (api.openai.com).
		if providerType == ProviderOpenRouter && provider.BaseURL == "" {
			provider.BaseURL = DefaultBaseURL(ProviderOpenRouter)
		}
		return buildOpenAIModel(ctx, provider, modelName, effort)
	case ProviderGemini:
		return buildGeminiModel(ctx, provider, modelName, effort)
	default:
		return nil, fmt.Errorf("%w: %q", ErrProviderUnsupported, providerType)
	}
}

// buildClaudeModel wires the eino-ext claude connector: the verified
// provider path. MaxTokens is required by the connector; MaxTokens carries
// the agent's cap or DefaultMaxTokens, raised above the thinking budget when
// an effort is set (the Anthropic API requires budget_tokens < max_tokens).
func buildClaudeModel(ctx context.Context, provider store.Provider, modelName string, maxTokens int, effort string) (model.BaseChatModel, error) {
	cfg := &claude.Config{
		APIKey:    provider.APIKey,
		Model:     modelName,
		MaxTokens: maxTokens,
	}
	if budget, ok := claudeThinkingBudgets[effort]; ok {
		if int64(maxTokens) <= budget {
			cfg.MaxTokens = int(budget) + DefaultMaxTokens
		}
		cfg.ThinkingConfig = &anthropic.ThinkingConfigParamUnion{
			OfEnabled: &anthropic.ThinkingConfigEnabledParam{
				Type:         "enabled",
				BudgetTokens: budget,
			},
		}
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
}

// buildOpenAIModel wires the eino-ext openai connector.
func buildOpenAIModel(ctx context.Context, provider store.Provider, modelName string, effort string) (model.BaseChatModel, error) {
	cfg := &openai.ChatModelConfig{
		APIKey:  provider.APIKey,
		BaseURL: provider.BaseURL,
		Model:   modelName,
	}
	if effort != "" {
		// Validated against the model's effort set upstream; the value rides
		// the native reasoning_effort request field verbatim (including the
		// gpt-5 family's "minimal").
		cfg.ReasoningEffort = openai.ReasoningEffortLevel(effort)
	}
	m, err := openai.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build openai model %q: %w", modelName, err)
	}
	return m, nil
}

// buildGeminiModel wires the eino-ext gemini connector, which takes a
// pre-built genai client.
func buildGeminiModel(ctx context.Context, provider store.Provider, modelName string, effort string) (model.BaseChatModel, error) {
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
	cfg := &gemini.Config{Client: client, Model: modelName}
	if budget, ok := geminiThinkingBudgets[effort]; ok {
		cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: genai.Ptr(budget)}
	}
	m, err := gemini.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build gemini model %q: %w", modelName, err)
	}
	return m, nil
}
