package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/agenticopenai"
	"github.com/cloudwego/eino/components/model"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type agenticOpenAIAdapter struct{}

func (a *agenticOpenAIAdapter) Build(ctx context.Context, p *store.Profile, modelName string, apiKey string) (model.AgenticModel, error) {
	if p.Enabled == 0 {
		return nil, fmt.Errorf("profile %q is disabled", p.Name)
	}

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

	if !IsKeyless(p.ProviderType) && apiKey == "" {
		return nil, fmt.Errorf("profile %q: API key is required for non-Ollama providers", p.Name)
	}

	config := &agenticopenai.ChatConfig{
		BaseURL: baseURL,
		Model:   modelName,
		APIKey:  apiKey,
	}

	if p.Settings != "" {
		var settings map[string]interface{}
		if err := json.Unmarshal([]byte(p.Settings), &settings); err != nil {
			return nil, fmt.Errorf("profile %q: invalid Settings JSON: %w", p.Name, err)
		}

		if temp, ok := settings["temperature"].(float64); ok {
			temp32 := float32(temp)
			config.Temperature = &temp32
		}
		if maxTokens, ok := settings["max_tokens"].(float64); ok {
			maxTokensInt := int(maxTokens)
			// For OpenAI agentic chat, MaxCompletionTokens is used instead of MaxTokens
			config.MaxCompletionTokens = &maxTokensInt
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

		if reasoningBudget > 0 {
			config.MaxCompletionTokens = &reasoningBudget
		}

		if reasoningEffort != "" {
			if config.ExtraFields == nil {
				config.ExtraFields = make(map[string]any)
			}
			if reasoningEffort == "on" {
				config.ExtraFields["reasoning_effort"] = "medium"
			} else if reasoningEffort == "off" {
				// skip or delete
			} else {
				switch reasoningEffort {
				case "low", "medium", "high":
					config.ExtraFields["reasoning_effort"] = reasoningEffort
				default:
					return nil, fmt.Errorf("openai provider does not support reasoning effort %q", reasoningEffort)
				}
			}
		}
	}

	client, err := agenticopenai.NewChatModel(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create agentic OpenAI client for profile %q: %w", p.Name, err)
	}

	return client, nil
}

func NewAgenticOpenAIAdapter() Adapter {
	return &agenticOpenAIAdapter{}
}

var _ Adapter = &agenticOpenAIAdapter{}
