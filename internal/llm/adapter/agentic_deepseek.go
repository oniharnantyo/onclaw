package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/agenticdeepseek"
	"github.com/cloudwego/eino/components/model"

	"github.com/oniharnantyo/onclaw/internal/store"
)

type agenticDeepSeekAdapter struct{}

func (a *agenticDeepSeekAdapter) Build(ctx context.Context, p *store.Profile, modelName string, apiKey string) (model.AgenticModel, error) {
	if p.Enabled == 0 {
		return nil, fmt.Errorf("profile %q is disabled", p.Name)
	}
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("profile %q: API key is required", p.Name)
	}

	config := &agenticdeepseek.Config{
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
	}

	client, err := agenticdeepseek.New(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create agentic DeepSeek client for profile %q: %w", p.Name, err)
	}

	return client, nil
}

func NewAgenticDeepSeekAdapter() Adapter {
	return &agenticDeepSeekAdapter{}
}

var _ Adapter = &agenticDeepSeekAdapter{}
