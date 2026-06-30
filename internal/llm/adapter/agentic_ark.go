package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/agenticark"
	"github.com/cloudwego/eino/components/model"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model/responses"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type agenticArkAdapter struct{}

func (a *agenticArkAdapter) Build(ctx context.Context, p *store.Profile, modelName string, apiKey string) (model.AgenticModel, error) {
	if p.Enabled == 0 {
		return nil, fmt.Errorf("profile %q is disabled", p.Name)
	}
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("profile %q: API key is required", p.Name)
	}

	config := &agenticark.Config{
		APIKey: apiKey,
		Model:  modelName,
	}
	if p.APIBase != "" {
		config.BaseURL = p.APIBase
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

		var reasoningEffort string
		if effortVal, ok := settings["reasoning_effort"].(string); ok {
			reasoningEffort = effortVal
		}
		if reasoningEffort != "" {
			if reasoningEffort == "off" {
				thinkingType := responses.ThinkingType_disabled
				config.Thinking = &responses.ResponsesThinking{
					Type: &thinkingType,
				}
			} else {
				thinkingType := responses.ThinkingType_enabled
				config.Thinking = &responses.ResponsesThinking{
					Type: &thinkingType,
				}

				// Map reasoning effort level
				var effort responses.ReasoningEffort_Enum
				switch reasoningEffort {
				case "low":
					effort = responses.ReasoningEffort_low
				case "medium", "on":
					effort = responses.ReasoningEffort_medium
				case "high":
					effort = responses.ReasoningEffort_high
				case "minimal":
					effort = responses.ReasoningEffort_minimal
				default:
					effort = responses.ReasoningEffort_medium
				}
				config.Reasoning = &responses.ResponsesReasoning{
					Effort: effort,
				}
			}
		}
	}

	client, err := agenticark.New(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create agentic Ark client for profile %q: %w", p.Name, err)
	}

	return client, nil
}

func NewAgenticArkAdapter() Adapter {
	return &agenticArkAdapter{}
}

var _ Adapter = &agenticArkAdapter{}
