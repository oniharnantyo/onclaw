package agents

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	agenticclaude "github.com/cloudwego/eino-ext/components/model/agenticclaude"
	agenticgemini "github.com/cloudwego/eino-ext/components/model/agenticgemini"
	agenticopenai "github.com/cloudwego/eino-ext/components/model/agenticopenai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
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

// buildAgenticModel dispatches on the provider type to the matching eino-ext
// agentic model component (agenticclaude, agenticopenai, agenticgemini),
// returning a model.BaseModel[*schema.AgenticMessage].
func buildAgenticModel(ctx context.Context, providerType string, provider store.Provider, modelName string, maxTokens int, effort string) (model.BaseModel[*schema.AgenticMessage], error) {
	switch providerType {
	case ProviderAnthropic, ProviderAnthropicCompatible:
		return buildAgenticClaudeModel(ctx, provider, modelName, maxTokens, effort)
	case ProviderOpenAI, ProviderOpenRouter, ProviderOpenAICompatible:
		// OpenRouter's root is fixed: when the provider leaves the base
		// blank, substitute it rather than falling through to the OpenAI
		// SDK default (api.openai.com).
		if providerType == ProviderOpenRouter && provider.BaseURL == "" {
			provider.BaseURL = DefaultBaseURL(ProviderOpenRouter)
		}
		return buildAgenticOpenAIModel(ctx, provider, modelName, effort)
	case ProviderGemini:
		return buildAgenticGeminiModel(ctx, provider, modelName, effort)
	default:
		return nil, fmt.Errorf("%w: %q", ErrProviderUnsupported, providerType)
	}
}

// buildAgenticClaudeModel wires the eino-ext agenticclaude connector.
func buildAgenticClaudeModel(ctx context.Context, provider store.Provider, modelName string, maxTokens int, effort string) (model.BaseModel[*schema.AgenticMessage], error) {
	cfg := &agenticclaude.Config{
		APIKey:    provider.APIKey,
		Model:     modelName,
		MaxTokens: maxTokens,
	}
	if budget, ok := claudeThinkingBudgets[effort]; ok {
		if int64(maxTokens) <= budget {
			cfg.MaxTokens = int(budget) + DefaultMaxTokens
		}
		cfg.Thinking = &anthropic.ThinkingConfigParamUnion{
			OfEnabled: &anthropic.ThinkingConfigEnabledParam{
				Type:         "enabled",
				BudgetTokens: budget,
			},
		}
	}
	if provider.BaseURL != "" {
		cfg.BaseURL = provider.BaseURL
	}
	m, err := agenticclaude.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build agentic claude model %q: %w", modelName, err)
	}
	return m, nil
}

// buildAgenticOpenAIModel wires the eino-ext agenticopenai connector.
func buildAgenticOpenAIModel(ctx context.Context, provider store.Provider, modelName string, effort string) (model.BaseModel[*schema.AgenticMessage], error) {
	cfg := &agenticopenai.ChatConfig{
		APIKey:  provider.APIKey,
		BaseURL: provider.BaseURL,
		Model:   modelName,
	}
	if effort != "" {
		cfg.ExtraFields = map[string]any{
			"reasoning_effort": effort,
		}
	}
	m, err := agenticopenai.NewChatModel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build agentic openai model %q: %w", modelName, err)
	}
	return m, nil
}

// buildAgenticGeminiModel wires the eino-ext agenticgemini connector.
func buildAgenticGeminiModel(ctx context.Context, provider store.Provider, modelName string, effort string) (model.BaseModel[*schema.AgenticMessage], error) {
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
	cfg := &agenticgemini.Config{Client: client, Model: modelName}
	if budget, ok := geminiThinkingBudgets[effort]; ok {
		cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: genai.Ptr(budget)}
	}
	m, err := agenticgemini.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build agentic gemini model %q: %w", modelName, err)
	}
	return m, nil
}

// buildModel dispatches to buildAgenticModel.
func buildModel(ctx context.Context, providerType string, provider store.Provider, modelName string, maxTokens int, effort string) (model.BaseModel[*schema.AgenticMessage], error) {
	return buildAgenticModel(ctx, providerType, provider, modelName, maxTokens, effort)
}
