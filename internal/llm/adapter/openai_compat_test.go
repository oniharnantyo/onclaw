package adapter

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestOpenAICompatAdapter_Build_Success(t *testing.T) {
	ctx := context.Background()
	adapter := NewOpenAICompatAdapter()

	tests := []struct {
		name    string
		profile *store.Profile
		apiKey  string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid OpenAI profile",
			profile: &store.Profile{
				Name:         "openai-test",
				ProviderType: "openai",
				APIBase:      "https://api.openai.com/v1",
				Model:        "gpt-4",
				Enabled:      1,
			},
			apiKey:  "sk-test-key",
			wantErr: false,
		},
		{
			name: "valid Ollama profile without API key",
			profile: &store.Profile{
				Name:         "ollama-test",
				ProviderType: "ollama",
				APIBase:      "http://localhost:11434/v1",
				Model:        "llama2",
				Enabled:      1,
			},
			apiKey:  "",
			wantErr: false,
		},
		{
			name: "valid custom OpenAI-compatible",
			profile: &store.Profile{
				Name:         "custom-test",
				ProviderType: "openai-compatible",
				APIBase:      "https://api.example.com/v1",
				Model:        "custom-model",
				Enabled:      1,
			},
			apiKey:  "custom-key",
			wantErr: false,
		},
		{
			name: "disabled profile",
			profile: &store.Profile{
				Name:         "disabled-test",
				ProviderType: "openai",
				APIBase:      "https://api.openai.com/v1",
				Model:        "gpt-4",
				Enabled:      0,
			},
			apiKey:  "sk-test-key",
			wantErr: true,
			errMsg:  `profile "disabled-test" is disabled`,
		},
		{
			name: "missing APIBase",
			profile: &store.Profile{
				Name:         "no-base-test",
				ProviderType: "openai-compatible",
				APIBase:      "",
				Model:        "gpt-4",
				Enabled:      1,
			},
			apiKey:  "sk-test-key",
			wantErr: true,
			errMsg:  `profile "no-base-test": APIBase is required`,
		},
		{
			name: "missing Model",
			profile: &store.Profile{
				Name:         "no-model-test",
				ProviderType: "openai",
				APIBase:      "https://api.openai.com/v1",
				Model:        "",
				Enabled:      1,
			},
			apiKey:  "sk-test-key",
			wantErr: true,
			errMsg:  `profile "no-model-test": Model is required`,
		},
		{
			name: "non-Ollama without API key",
			profile: &store.Profile{
				Name:         "no-key-test",
				ProviderType: "openai",
				APIBase:      "https://api.openai.com/v1",
				Model:        "gpt-4",
				Enabled:      1,
			},
			apiKey:  "",
			wantErr: true,
			errMsg:  `profile "no-key-test": API key is required for non-Ollama providers`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatModel, err := adapter.Build(ctx, tt.profile, tt.apiKey)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Build() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Build() error = %q, want %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Build() unexpected error: %v", err)
				return
			}

			if chatModel == nil {
				t.Errorf("Build() returned nil ChatModel")
				return
			}

			// Verify it implements the interface
			if _, ok := chatModel.(model.ToolCallingChatModel); !ok {
				t.Errorf("Build() returned type that doesn't implement model.ToolCallingChatModel")
			}
		})
	}
}

func TestOpenAICompatAdapter_Build_WithSettings(t *testing.T) {
	ctx := context.Background()
	adapter := NewOpenAICompatAdapter()

	// Create settings JSON
	settings := map[string]interface{}{
		"temperature": 0.7,
		"max_tokens":  1000,
		"top_p":       0.9,
		"stop":        []string{"\n", "User:"},
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("failed to marshal settings: %v", err)
	}

	profile := &store.Profile{
		Name:         "settings-test",
		ProviderType: "openai",
		APIBase:      "https://api.openai.com/v1",
		Model:        "gpt-4",
		Settings:     string(settingsJSON),
		Enabled:      1,
	}

	chatModel, err := adapter.Build(ctx, profile, "sk-test-key")
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}

	if chatModel == nil {
		t.Errorf("Build() returned nil ChatModel")
	}

	// Verify it implements the interface
	if _, ok := chatModel.(model.ToolCallingChatModel); !ok {
		t.Errorf("Build() returned type that doesn't implement model.ToolCallingChatModel")
	}
}

func TestOpenAICompatAdapter_Build_InvalidSettingsJSON(t *testing.T) {
	ctx := context.Background()
	adapter := NewOpenAICompatAdapter()

	profile := &store.Profile{
		Name:         "invalid-settings-test",
		ProviderType: "openai",
		APIBase:      "https://api.openai.com/v1",
		Model:        "gpt-4",
		Settings:     "{invalid json}",
		Enabled:      1,
	}

	_, err := adapter.Build(ctx, profile, "sk-test-key")
	if err == nil {
		t.Errorf("Build() expected error for invalid Settings JSON, got nil")
		return
	}

	// Error should mention the invalid JSON
	if err.Error() != `profile "invalid-settings-test": invalid Settings JSON` &&
		err.Error() != `profile "invalid-settings-test": invalid Settings JSON: invalid character 'i' looking for beginning of object key string` {
		t.Logf("Build() error = %q (expected mention of invalid JSON)", err.Error())
	}
}

func TestNewOpenAICompatAdapter(t *testing.T) {
	adapter := NewOpenAICompatAdapter()
	if adapter == nil {
		t.Errorf("NewOpenAICompatAdapter() returned nil")
		return
	}

	// Verify it implements the Adapter interface
	if _, ok := adapter.(Adapter); !ok {
		t.Errorf("NewOpenAICompatAdapter() returned type that doesn't implement Adapter")
	}
}

func TestOpenAICompatAdapter_InterfaceCompliance(t *testing.T) {
	// Compile-time check that openaiCompatAdapter implements Adapter
	var _ Adapter = &openaiCompatAdapter{}
}

func TestOpenAICompatAdapter_OllamaWithoutAPIKey(t *testing.T) {
	ctx := context.Background()
	adapter := NewOpenAICompatAdapter()

	tests := []struct {
		name    string
		baseURL string
		model   string
		apiKey  string
		wantErr bool
	}{
		{
			name:    "Ollama localhost without key",
			baseURL: "http://localhost:11434/v1",
			model:   "llama2",
			apiKey:  "",
			wantErr: false,
		},
		{
			name:    "Ollama remote without key",
			baseURL: "http://192.168.1.100:11434/v1",
			model:   "mistral",
			apiKey:  "",
			wantErr: false,
		},
		{
			name:    "Ollama with key (should still work)",
			baseURL: "http://localhost:11434/v1",
			model:   "llama2",
			apiKey:  "optional-key",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := &store.Profile{
				Name:         "ollama-test",
				ProviderType: "ollama",
				APIBase:      tt.baseURL,
				Model:        tt.model,
				Enabled:      1,
			}

			chatModel, err := adapter.Build(ctx, profile, tt.apiKey)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Build() expected error, got nil")
					return
				}
				return
			}

			if err != nil {
				t.Errorf("Build() unexpected error: %v", err)
				return
			}

			if chatModel == nil {
				t.Errorf("Build() returned nil ChatModel")
			}
		})
	}
}

func TestOpenAICompatAdapter_NonOllamaRequiresAPIKey(t *testing.T) {
	ctx := context.Background()
	adapter := NewOpenAICompatAdapter()

	tests := []struct {
		name         string
		providerType string
		baseURL      string
		model        string
		wantErr      bool
	}{
		{
			name:         "OpenAI requires key",
			providerType: "openai",
			baseURL:      "https://api.openai.com/v1",
			model:        "gpt-4",
			wantErr:      true,
		},
		{
			name:         "OpenAI-compatible requires key",
			providerType: "openai-compatible",
			baseURL:      "https://api.example.com/v1",
			model:        "custom-model",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := &store.Profile{
				Name:         "no-key-test",
				ProviderType: tt.providerType,
				APIBase:      tt.baseURL,
				Model:        tt.model,
				Enabled:      1,
			}

			_, err := adapter.Build(ctx, profile, "")

			if !tt.wantErr {
				if err != nil {
					t.Errorf("Build() unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("Build() expected error for missing API key, got nil")
				return
			}

			expectedMsg := `profile "no-key-test": API key is required for non-Ollama providers`
			if err.Error() != expectedMsg {
				t.Errorf("Build() error = %q, want %q", err.Error(), expectedMsg)
			}
		})
	}
}

func TestOpenAICompatAdapter_Build_WithReasoningEffort(t *testing.T) {
	ctx := context.Background()
	adapter := NewOpenAICompatAdapter()

	// 1. Valid reasoning effort on openai provider
	settings := map[string]interface{}{
		"reasoning_effort": "high",
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("failed to marshal settings: %v", err)
	}

	profile := &store.Profile{
		Name:         "reasoning-test",
		ProviderType: "openai",
		APIBase:      "https://api.openai.com/v1",
		Model:        "o1-preview",
		Settings:     string(settingsJSON),
		Enabled:      1,
	}

	chatModel, err := adapter.Build(ctx, profile, "sk-test-key")
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	if chatModel == nil {
		t.Errorf("Build() returned nil ChatModel")
	}

	// 2. Unsupported provider type (e.g. ollama) should skip applying ReasoningEffort
	profileOllama := &store.Profile{
		Name:         "ollama-reasoning-test",
		ProviderType: "ollama",
		APIBase:      "http://localhost:11434/v1",
		Model:        "llama3",
		Settings:     string(settingsJSON),
		Enabled:      1,
	}

	chatModelOllama, err := adapter.Build(ctx, profileOllama, "")
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	if chatModelOllama == nil {
		t.Errorf("Build() returned nil ChatModel")
	}
}
