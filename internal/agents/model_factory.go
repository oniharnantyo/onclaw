package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/agenticclaude"
	"github.com/cloudwego/eino-ext/components/model/agenticgemini"
	"github.com/cloudwego/eino-ext/components/model/agenticopenai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"google.golang.org/genai"
)

// AcceptsFileBlocks reports whether the underlying connector for the provider type
// accepts schema.UserInputFile content blocks natively. Anthropic and Gemini
// models support file/document blocks; OpenAI-family connectors (openai, openrouter,
// openai-compatible) do not support file blocks and require them to be degraded or
// collapsed.
func AcceptsFileBlocks(providerType string) bool {
	switch strings.TrimSpace(providerType) {
	case providers.TypeAnthropic, providers.TypeAnthropicCompatible, providers.TypeGemini:
		return true
	case providers.TypeOpenAI, providers.TypeOpenRouter, providers.TypeOpenAICompatible:
		return false
	default:
		return false
	}
}

// AgenticModelFactory builds the Eino agentic model used for agent conversations and executions.
type AgenticModelFactory func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseModel[*schema.AgenticMessage], error)

// DefaultAgenticModelFactory creates an agentic model for the 6 supported provider types.
func DefaultAgenticModelFactory(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseModel[*schema.AgenticMessage], error) {
	switch strings.TrimSpace(providerType) {
	case providers.TypeOpenAI:
		cfg := &agenticopenai.ChatConfig{
			APIKey: cred.APIKey,
			Model:  modelName,
		}
		if cred.BaseURL != "" {
			cfg.BaseURL = cred.BaseURL
		}
		return agenticopenai.NewChatModel(ctx, cfg)

	case providers.TypeAnthropic:
		cfg := &agenticclaude.Config{
			APIKey:    cred.APIKey,
			Model:     modelName,
			MaxTokens: 4096,
		}
		if cred.BaseURL != "" {
			cfg.BaseURL = providers.StripVersionPath(cred.BaseURL)
		}
		return agenticclaude.New(ctx, cfg)

	case providers.TypeGemini:
		clientCfg := &genai.ClientConfig{
			APIKey:  cred.APIKey,
			Backend: genai.BackendGeminiAPI,
		}
		if cred.BaseURL != "" {
			clientCfg.HTTPOptions = genai.HTTPOptions{
				BaseURL: providers.StripVersionPath(cred.BaseURL),
			}
		}
		client, err := genai.NewClient(ctx, clientCfg)
		if err != nil {
			return nil, fmt.Errorf("create gemini client: %w", err)
		}
		cfg := &agenticgemini.Config{
			Client: client,
			Model:  modelName,
		}
		return agenticgemini.New(ctx, cfg)

	case providers.TypeOpenRouter:
		baseURL := cred.BaseURL
		if baseURL == "" {
			baseURL = "https://openrouter.ai/api/v1"
		}
		cfg := &agenticopenai.ChatConfig{
			APIKey:  cred.APIKey,
			BaseURL: baseURL,
			Model:   modelName,
		}
		return agenticopenai.NewChatModel(ctx, cfg)

	case providers.TypeOpenAICompatible:
		cfg := &agenticopenai.ChatConfig{
			APIKey:  cred.APIKey,
			BaseURL: cred.BaseURL,
			Model:   modelName,
		}
		return agenticopenai.NewChatModel(ctx, cfg)

	case providers.TypeAnthropicCompatible:
		cfg := &agenticclaude.Config{
			APIKey:    cred.APIKey,
			Model:     modelName,
			MaxTokens: 4096,
		}
		if cred.BaseURL != "" {
			cfg.BaseURL = providers.StripVersionPath(cred.BaseURL)
		}
		return agenticclaude.New(ctx, cfg)

	default:
		return nil, fmt.Errorf("%w: unsupported provider type %q", domain.ErrInvalid, providerType)
	}
}
