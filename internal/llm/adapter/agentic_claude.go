package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/cloudwego/eino-ext/components/model/agenticclaude"
	"github.com/cloudwego/eino/components/model"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type agenticClaudeAdapter struct{}

func (a *agenticClaudeAdapter) Build(ctx context.Context, p *store.Profile, modelName string, apiKey string) (model.AgenticModel, error) {
	if p.Enabled == 0 {
		return nil, fmt.Errorf("profile %q is disabled", p.Name)
	}
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("profile %q: API key is required", p.Name)
	}

	config := &agenticclaude.Config{
		APIKey: apiKey,
		Model:  modelName,
	}

	// Apply default MaxTokens for Claude as it requires one.
	defaultMaxTokens := 4096
	config.MaxTokens = defaultMaxTokens

	if p.Settings != "" {
		var settings map[string]interface{}
		if err := json.Unmarshal([]byte(p.Settings), &settings); err != nil {
			return nil, fmt.Errorf("profile %q: invalid Settings JSON: %w", p.Name, err)
		}

		if temp, ok := settings["temperature"].(float64); ok {
			temp32 := float32(temp)
			// Wait, config.Temperature in Claude config? Let's check if it exists or uses options.
			// Let's look at Config doc we fetched. Config had StopSequences, Thinking, CustomHeaders, ExtraFields, CacheControl.
			// Temperature is not in Config struct, it is set via options or ExtraFields.
			// Let's set temperature in ExtraFields if present.
			if config.ExtraFields == nil {
				config.ExtraFields = make(map[string]any)
			}
			config.ExtraFields["temperature"] = temp32
		}
		if maxTokens, ok := settings["max_tokens"].(float64); ok {
			maxTokensInt := int(maxTokens)
			config.MaxTokens = maxTokensInt
		}
		if topP, ok := settings["top_p"].(float64); ok {
			topP32 := float32(topP)
			if config.ExtraFields == nil {
				config.ExtraFields = make(map[string]any)
			}
			config.ExtraFields["top_p"] = topP32
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
				disabled := anthropic.NewThinkingConfigDisabledParam()
				union := anthropic.ThinkingConfigParamUnion{
					OfDisabled: &disabled,
				}
				config.Thinking = &union
			} else {
				budget := reasoningBudget
				if budget == 0 {
					budget = 1024 // default fallback
				}
				union := anthropic.ThinkingConfigParamOfEnabled(int64(budget))
				config.Thinking = &union
				config.MaxTokens = budget
			}
		}
	}

	client, err := agenticclaude.New(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create agentic Claude client for profile %q: %w", p.Name, err)
	}

	return client, nil
}

func NewAgenticClaudeAdapter() Adapter {
	return &agenticClaudeAdapter{}
}

var _ Adapter = &agenticClaudeAdapter{}
