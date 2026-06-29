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
		name      string
		profile   *store.Profile
		modelName string
		apiKey    string
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid OpenAI profile",
			profile: &store.Profile{
				Name:         "openai-test",
				ProviderType: "openai",
				APIBase:      "https://api.openai.com/v1",
				Enabled:      1,
			},
			modelName: "gpt-4",
			apiKey:    "sk-test-key",
			wantErr:   false,
		},
		{
			name: "valid Ollama profile without API key",
			profile: &store.Profile{
				Name:         "ollama-test",
				ProviderType: "ollama",
				APIBase:      "http://localhost:11434/v1",
				Enabled:      1,
			},
			modelName: "llama2",
			apiKey:    "",
			wantErr:   false,
		},
		{
			name: "valid custom OpenAI-compatible",
			profile: &store.Profile{
				Name:         "custom-test",
				ProviderType: "openai-compatible",
				APIBase:      "https://api.example.com/v1",
				Enabled:      1,
			},
			modelName: "custom-model",
			apiKey:    "custom-key",
			wantErr:   false,
		},
		{
			name: "disabled profile",
			profile: &store.Profile{
				Name:         "disabled-test",
				ProviderType: "openai",
				APIBase:      "https://api.openai.com/v1",
				Enabled:      0,
			},
			modelName: "gpt-4",
			apiKey:    "sk-test-key",
			wantErr:   true,
			errMsg:    `profile "disabled-test" is disabled`,
		},
		{
			name: "missing APIBase",
			profile: &store.Profile{
				Name:         "no-base-test",
				ProviderType: "openai-compatible",
				APIBase:      "",
				Enabled:      1,
			},
			modelName: "gpt-4",
			apiKey:    "sk-test-key",
			wantErr:   true,
			errMsg:    `profile "no-base-test": APIBase is required`,
		},
		{
			name: "missing Model",
			profile: &store.Profile{
				Name:         "no-model-test",
				ProviderType: "openai",
				APIBase:      "https://api.openai.com/v1",
				Enabled:      1,
			},
			modelName: "",
			apiKey:    "sk-test-key",
			wantErr:   true,
			errMsg:    `model name is required`,
		},
		{
			name: "non-Ollama without API key",
			profile: &store.Profile{
				Name:         "no-key-test",
				ProviderType: "openai",
				APIBase:      "https://api.openai.com/v1",
				Enabled:      1,
			},
			modelName: "gpt-4",
			apiKey:    "",
			wantErr:   true,
			errMsg:    `profile "no-key-test": API key is required for non-Ollama providers`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatModel, err := adapter.Build(ctx, tt.profile, tt.modelName, tt.apiKey)

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
		Settings:     string(settingsJSON),
		Enabled:      1,
	}

	chatModel, err := adapter.Build(ctx, profile, "gpt-4", "sk-test-key")
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
		Settings:     "{invalid json}",
		Enabled:      1,
	}

	_, err := adapter.Build(ctx, profile, "gpt-4", "sk-test-key")
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
				Enabled:      1,
			}

			chatModel, err := adapter.Build(ctx, profile, tt.model, tt.apiKey)

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
				Enabled:      1,
			}

			_, err := adapter.Build(ctx, profile, tt.model, "")

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
		Settings:     string(settingsJSON),
		Enabled:      1,
	}

	chatModel, err := adapter.Build(ctx, profile, "o1-preview", "sk-test-key")
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
		Settings:     string(settingsJSON),
		Enabled:      1,
	}

	chatModelOllama, err := adapter.Build(ctx, profileOllama, "llama3", "")
	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	if chatModelOllama == nil {
		t.Errorf("Build() returned nil ChatModel")
	}
}

func TestOpenAICompatAdapter_Build_ReasoningMapping(t *testing.T) {
	ctx := context.Background()
	ad := NewOpenAICompatAdapter()

	// 1. OpenAI with custom effort enums (minimal, xhigh, max)
	for _, effort := range []string{"minimal", "xhigh", "max"} {
		settings := map[string]interface{}{
			"reasoning_effort": effort,
		}
		settingsJSON, _ := json.Marshal(settings)
		p := &store.Profile{
			Name:         "openai-effort-" + effort,
			ProviderType: "openai",
			APIBase:      "https://api.openai.com/v1",
			Settings:     string(settingsJSON),
			Enabled:      1,
		}
		chatModel, err := ad.Build(ctx, p, "gpt-4", "sk-test-key")
		if err != nil {
			t.Errorf("Build() unexpected error for effort %s: %v", effort, err)
		}
		if chatModel == nil {
			t.Errorf("Build() returned nil ChatModel for effort %s", effort)
		}
	}

	// 2. OpenAI with budget tokens (should succeed)
	settingsBudget := map[string]interface{}{
		"reasoning_budget_tokens": 1024,
	}
	settingsBudgetJSON, _ := json.Marshal(settingsBudget)
	pBudget := &store.Profile{
		Name:         "openai-budget",
		ProviderType: "openai",
		APIBase:      "https://api.openai.com/v1",
		Settings:     string(settingsBudgetJSON),
		Enabled:      1,
	}
	chatModelOpenAIBudget, err := ad.Build(ctx, pBudget, "gpt-4", "sk-test-key")
	if err != nil {
		t.Errorf("expected no error for budget tokens on openai provider, got %v", err)
	}
	if chatModelOpenAIBudget == nil {
		t.Errorf("expected chatModel to not be nil")
	}

	// 3. Anthropic with budget tokens
	settingsAnthropic := map[string]interface{}{
		"reasoning_budget_tokens": 2048,
	}
	settingsAnthropicJSON, _ := json.Marshal(settingsAnthropic)
	pAnthropic := &store.Profile{
		Name:         "anthropic-budget",
		ProviderType: "anthropic",
		APIBase:      "https://api.anthropic.com/v1",
		Settings:     string(settingsAnthropicJSON),
		Enabled:      1,
	}
	// Note: anthropic provider uses OpenAICompatAdapter under the hood here for testing mapping
	// wait, anthropic is registered with NewStubAdapter in defaults.go, but openaiCompatAdapter Build method is called directly on the ad instance in this unit test.
	chatModelAnthropic, err := ad.Build(ctx, pAnthropic, "claude-3-7-sonnet", "sk-test-key")
	if err != nil {
		t.Fatalf("Build() unexpected error for anthropic: %v", err)
	}
	if chatModelAnthropic == nil {
		t.Errorf("Build() returned nil ChatModel for anthropic")
	}

	// 4. Google with budget tokens
	settingsGoogle := map[string]interface{}{
		"reasoning_budget_tokens": 4096,
	}
	settingsGoogleJSON, _ := json.Marshal(settingsGoogle)
	pGoogle := &store.Profile{
		Name:         "google-budget",
		ProviderType: "google",
		APIBase:      "https://generativelanguage.googleapis.com/v1beta/openai",
		Settings:     string(settingsGoogleJSON),
		Enabled:      1,
	}
	chatModelGoogle, err := ad.Build(ctx, pGoogle, "gemini-2.0-flash-thinking", "sk-test-key")
	if err != nil {
		t.Fatalf("Build() unexpected error for google: %v", err)
	}
	if chatModelGoogle == nil {
		t.Errorf("Build() returned nil ChatModel for google")
	}

	// 5. Unknown provider type (should fail)
	settingsOllama := map[string]interface{}{
		"reasoning_effort": "medium",
	}
	settingsOllamaJSON, _ := json.Marshal(settingsOllama)
	pUnknown := &store.Profile{
		Name:         "unknown-reasoning",
		ProviderType: "unknown-provider",
		APIBase:      "http://localhost/v1",
		Settings:     string(settingsOllamaJSON),
		Enabled:      1,
	}
	_, err = ad.Build(ctx, pUnknown, "model", "sk-test-key")
	if err == nil {
		t.Errorf("expected error for unknown provider type, got nil")
	}
}

