//go:build !exclude_openai
// +build !exclude_openai

package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/libs/acl/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// openaiCompatAdapter implements Adapter for OpenAI-compatible providers.
type openaiCompatAdapter struct{}

// Build creates a real streaming ChatModel from the profile configuration.
func (a *openaiCompatAdapter) Build(ctx context.Context, p *store.Profile, modelName string, apiKey string) (model.ToolCallingChatModel, error) {
	// Check if profile is disabled
	if p.Enabled == 0 {
		return nil, fmt.Errorf("profile %q is disabled", p.Name)
	}

	// Validate required fields and apply defaults
	baseURL := p.APIBase
	if baseURL == "" {
		if p.ProviderType == "ollama" {
			baseURL = "http://localhost:11434/v1"
		} else if p.ProviderType == "openai" {
			baseURL = "https://api.openai.com/v1"
		} else {
			return nil, fmt.Errorf("profile %q: APIBase is required", p.Name)
		}
	}
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	// Keyless providers (e.g. local Ollama) may run without an API key;
	// remote providers require one.
	if !IsKeyless(p.ProviderType) && apiKey == "" {
		return nil, fmt.Errorf("profile %q: API key is required for non-Ollama providers", p.Name)
	}

	// Build OpenAI client config
	config := &openai.Config{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  apiKey,
	}

	// Parse Settings JSON if present
	if p.Settings != "" {
		var settings map[string]interface{}
		if err := json.Unmarshal([]byte(p.Settings), &settings); err != nil {
			return nil, fmt.Errorf("profile %q: invalid Settings JSON: %w", p.Name, err)
		}

		// Apply common settings to config
		// Temperature, MaxTokens, etc. can be parsed here
		if temp, ok := settings["temperature"].(float64); ok {
			temp32 := float32(temp)
			config.Temperature = &temp32
		}
		if maxTokens, ok := settings["max_tokens"].(float64); ok {
			maxTokensInt := int(maxTokens)
			config.MaxTokens = &maxTokensInt
		}
		if topP, ok := settings["top_p"].(float64); ok {
			topP32 := float32(topP)
			config.TopP = &topP32
		}
		if stop, ok := settings["stop"].([]interface{}); ok {
			stopStrings := make([]string, 0, len(stop))
			for _, s := range stop {
				if str, ok := s.(string); ok {
					stopStrings = append(stopStrings, str)
				}
			}
			config.Stop = stopStrings
		}
		var reasoningEffort string
		if effortVal, ok := settings["reasoning_effort"].(string); ok {
			reasoningEffort = effortVal
		}
		var reasoningBudget int
		if budgetVal, ok := settings["reasoning_budget_tokens"].(float64); ok {
			reasoningBudget = int(budgetVal)
		} else if budgetVal, ok := settings["reasoning_budget_tokens"].(int); ok {
			reasoningBudget = budgetVal
		}

		if reasoningEffort != "" || reasoningBudget > 0 {
			provType := strings.ToLower(p.ProviderType)
			if provType == "openai" || provType == "openai-compatible" {
				if reasoningBudget > 0 {
					config.MaxCompletionTokens = &reasoningBudget
				}
				if reasoningEffort != "" {
					if reasoningEffort == "on" || reasoningEffort == "off" {
						if reasoningEffort == "on" {
							config.ReasoningEffort = openai.ReasoningEffortLevel("medium")
						} else {
							config.ReasoningEffort = openai.ReasoningEffortLevel("")
						}
					} else {
						switch reasoningEffort {
						case "low", "medium", "high", "minimal", "xhigh", "max", "none":
							config.ReasoningEffort = openai.ReasoningEffortLevel(reasoningEffort)
						default:
							return nil, fmt.Errorf("openai provider does not support reasoning effort %q", reasoningEffort)
						}
					}
				}
			} else if provType == "anthropic" {
				if config.ExtraFields == nil {
					config.ExtraFields = make(map[string]any)
				}
				if reasoningEffort == "off" {
					config.ExtraFields["thinking"] = map[string]any{
						"type": "disabled",
					}
				} else {
					budget := reasoningBudget
					if budget == 0 {
						budget = 1024 // default fallback
					}
					config.ExtraFields["thinking"] = map[string]any{
						"type": "enabled",
						"budget_tokens": budget,
					}
					config.MaxCompletionTokens = &budget
				}
			} else if provType == "google" {
				if config.ExtraFields == nil {
					config.ExtraFields = make(map[string]any)
				}
				if reasoningEffort == "off" {
					config.ExtraFields["thinking_config"] = map[string]any{
						"thinking_budget": 0,
					}
				} else {
					budget := reasoningBudget
					if budget == 0 {
						budget = 1024 // default fallback
					}
					config.ExtraFields["thinking_config"] = map[string]any{
						"thinking_budget": budget,
					}
				}
			} else if provType == "ollama" {
				// Ollama does not natively support reasoning_effort API parameter; skip applying without error.
			} else {
				return nil, fmt.Errorf("provider type %q does not support reasoning configuration", p.ProviderType)
			}
		}
	}

	// Create the OpenAI client
	client, err := openai.NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAI client for profile %q: %w", p.Name, err)
	}

	// The Client struct directly implements model.ToolCallingChatModel
	// It has Generate() and Stream() methods
	return client, nil
}

// NewOpenAICompatAdapter creates an OpenAI-compatible adapter factory.
func NewOpenAICompatAdapter() Adapter {
	return &openaiCompatAdapter{}
}

// Verify interface compliance
var _ Adapter = &openaiCompatAdapter{}
