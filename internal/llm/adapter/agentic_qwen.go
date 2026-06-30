package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/agenticqwen"
	"github.com/cloudwego/eino/components/model"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type agenticQwenAdapter struct{}

func (a *agenticQwenAdapter) Build(ctx context.Context, p *store.Profile, modelName string, apiKey string) (model.AgenticModel, error) {
	if p.Enabled == 0 {
		return nil, fmt.Errorf("profile %q is disabled", p.Name)
	}
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("profile %q: API key is required", p.Name)
	}

	baseURL := p.APIBase
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}

	config := &agenticqwen.Config{
		APIKey:  apiKey,
		Model:   modelName,
		BaseURL: baseURL,
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
			var enableThinking bool
			if reasoningEffort == "on" {
				enableThinking = true
			} else if reasoningEffort == "off" {
				enableThinking = false
			} else {
				enableThinking = true
			}
			config.EnableThinking = &enableThinking
		}
	}

	client, err := agenticqwen.New(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create agentic Qwen client for profile %q: %w", p.Name, err)
	}

	return client, nil
}

func NewAgenticQwenAdapter() Adapter {
	return &agenticQwenAdapter{}
}

var _ Adapter = &agenticQwenAdapter{}
