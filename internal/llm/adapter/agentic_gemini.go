package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/agenticgemini"
	"github.com/cloudwego/eino/components/model"
	"google.golang.org/genai"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type agenticGeminiAdapter struct{}

func (a *agenticGeminiAdapter) Build(ctx context.Context, p *store.Profile, modelName string, apiKey string) (model.AgenticModel, error) {
	if p.Enabled == 0 {
		return nil, fmt.Errorf("profile %q is disabled", p.Name)
	}
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("profile %q: API key is required", p.Name)
	}

	genaiClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Google GenAI client: %w", err)
	}

	config := &agenticgemini.Config{
		Client: genaiClient,
		Model:  modelName,
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
			config.MaxTokens = &maxTokensInt
		}
		if topP, ok := settings["top_p"].(float64); ok {
			topP32 := float32(topP)
			config.TopP = &topP32
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
			if reasoningEffort == "off" {
				config.ThinkingConfig = &genai.ThinkingConfig{
					IncludeThoughts: false,
				}
			} else {
				budget := int32(reasoningBudget)
				if budget == 0 {
					budget = 1024 // default fallback
				}
				config.ThinkingConfig = &genai.ThinkingConfig{
					IncludeThoughts: true,
					ThinkingBudget:  &budget,
				}
			}
		}
	}

	client, err := agenticgemini.New(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create agentic Gemini client for profile %q: %w", p.Name, err)
	}

	return client, nil
}

func NewAgenticGeminiAdapter() Adapter {
	return &agenticGeminiAdapter{}
}

var _ Adapter = &agenticGeminiAdapter{}
