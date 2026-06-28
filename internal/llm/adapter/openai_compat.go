//go:build !exclude_openai
// +build !exclude_openai

package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino-ext/libs/acl/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// openaiCompatAdapter implements Adapter for OpenAI-compatible providers.
type openaiCompatAdapter struct{}

// Build creates a real streaming ChatModel from the profile configuration.
func (a *openaiCompatAdapter) Build(ctx context.Context, p *store.Profile, apiKey string) (model.ToolCallingChatModel, error) {
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
	if p.Model == "" {
		return nil, fmt.Errorf("profile %q: Model is required", p.Name)
	}

	// Keyless providers (e.g. local Ollama) may run without an API key;
	// remote providers require one.
	if !IsKeyless(p.ProviderType) && apiKey == "" {
		return nil, fmt.Errorf("profile %q: API key is required for non-Ollama providers", p.Name)
	}

	// Build OpenAI client config
	config := &openai.Config{
		BaseURL: baseURL,
		Model:   p.Model,
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
		if effort, ok := settings["reasoning_effort"].(string); ok {
			if p.ProviderType == "openai" || p.ProviderType == "openai-compatible" {
				switch effort {
				case "low":
					config.ReasoningEffort = openai.ReasoningEffortLevel("low")
				case "medium":
					config.ReasoningEffort = openai.ReasoningEffortLevel("medium")
				case "high":
					config.ReasoningEffort = openai.ReasoningEffortLevel("high")
				}
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
