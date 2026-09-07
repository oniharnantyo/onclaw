package promptgen

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/gemini"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"google.golang.org/genai"
)

// ModelFactory is a function that creates a ChatModel for a given provider configuration and model name.
type ModelFactory func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseChatModel, error)

// AgentPromptGeneratorModelFactory builds the Eino ChatModel used only for
// prompt generation: it bakes in the structured-output schema and fixed
// generation params. The agent runtime will get its own factory to build a
// plain BaseChatModel for conversations.
func AgentPromptGeneratorModelFactory(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseChatModel, error) {
	temp := GenerationTemperature
	maxTokens := GenerationMaxTokens

	switch strings.TrimSpace(providerType) {
	case providers.TypeOpenAI:
		cfg := &openai.ChatModelConfig{
			APIKey:         cred.APIKey,
			Model:          modelName,
			Temperature:    &temp,
			ResponseFormat: structuredOutputFormat(),
		}
		if cred.BaseURL != "" {
			cfg.BaseURL = cred.BaseURL
		}
		return openai.NewChatModel(ctx, cfg)

	case providers.TypeAnthropic:
		cfg := &claude.Config{
			APIKey:         cred.APIKey,
			Model:          modelName,
			MaxTokens:      maxTokens,
			Temperature:    &temp,
			ResponseFormat: &claude.ResponseFormat{Schema: promptsJSONSchema()},
		}
		if cred.BaseURL != "" {
			stripped := providers.StripVersionPath(cred.BaseURL)
			cfg.BaseURL = &stripped
		}
		return claude.NewChatModel(ctx, cfg)

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
		cfg := &gemini.Config{
			Client:      client,
			Model:       modelName,
			Temperature: &temp,
		}
		return gemini.NewChatModel(ctx, cfg)

	case providers.TypeOpenRouter:
		baseURL := cred.BaseURL
		if baseURL == "" {
			baseURL = "https://openrouter.ai/api/v1"
		}
		cfg := &openai.ChatModelConfig{
			APIKey:         cred.APIKey,
			BaseURL:        baseURL,
			Model:          modelName,
			Temperature:    &temp,
			ResponseFormat: structuredOutputFormat(),
		}
		return openai.NewChatModel(ctx, cfg)

	case providers.TypeOpenAICompatible:
		cfg := &openai.ChatModelConfig{
			APIKey:         cred.APIKey,
			BaseURL:        cred.BaseURL,
			Model:          modelName,
			Temperature:    &temp,
			ResponseFormat: structuredOutputFormat(),
		}
		return openai.NewChatModel(ctx, cfg)

	case providers.TypeAnthropicCompatible:
		cfg := &claude.Config{
			APIKey:         cred.APIKey,
			Model:          modelName,
			MaxTokens:      maxTokens,
			Temperature:    &temp,
			ResponseFormat: &claude.ResponseFormat{Schema: promptsJSONSchema()},
		}
		if cred.BaseURL != "" {
			stripped := providers.StripVersionPath(cred.BaseURL)
			cfg.BaseURL = &stripped
		}
		return claude.NewChatModel(ctx, cfg)

	default:
		return nil, fmt.Errorf("%w: unsupported provider type %q", domain.ErrInvalid, providerType)
	}
}
