package agents_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
)

func TestDefaultAgenticModelFactory_AllProviders(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		providerType string
		cred         providers.Credential
		modelName    string
		wantErr      bool
		errCheck     func(error) bool
	}{
		{
			name:         "openai default endpoint",
			providerType: providers.TypeOpenAI,
			cred: providers.Credential{
				APIKey: "sk-test",
			},
			modelName: "gpt-4o",
			wantErr:   false,
		},
		{
			name:         "openai custom baseURL",
			providerType: providers.TypeOpenAI,
			cred: providers.Credential{
				APIKey:  "sk-test",
				BaseURL: "https://custom.openai.com/v1",
			},
			modelName: "gpt-4o",
			wantErr:   false,
		},
		{
			name:         "openrouter default baseURL",
			providerType: providers.TypeOpenRouter,
			cred: providers.Credential{
				APIKey: "sk-or-test",
			},
			modelName: "meta-llama/llama-3-70b-instruct",
			wantErr:   false,
		},
		{
			name:         "openrouter custom baseURL",
			providerType: providers.TypeOpenRouter,
			cred: providers.Credential{
				APIKey:  "sk-or-test",
				BaseURL: "https://openrouter.ai/api/v1",
			},
			modelName: "meta-llama/llama-3-70b-instruct",
			wantErr:   false,
		},
		{
			name:         "openai-compatible custom baseURL",
			providerType: providers.TypeOpenAICompatible,
			cred: providers.Credential{
				APIKey:  "sk-test",
				BaseURL: "https://my-gateway.example.com/v1",
			},
			modelName: "deepseek-chat",
			wantErr:   false,
		},
		{
			name:         "anthropic default endpoint",
			providerType: providers.TypeAnthropic,
			cred: providers.Credential{
				APIKey: "sk-ant-test",
			},
			modelName: "claude-3-5-sonnet-20241022",
			wantErr:   false,
		},
		{
			name:         "anthropic baseURL with stripped /v1",
			providerType: providers.TypeAnthropic,
			cred: providers.Credential{
				APIKey:  "sk-ant-test",
				BaseURL: "https://api.anthropic.com/v1",
			},
			modelName: "claude-3-5-sonnet-20241022",
			wantErr:   false,
		},
		{
			name:         "anthropic-compatible with stripped /v1",
			providerType: providers.TypeAnthropicCompatible,
			cred: providers.Credential{
				APIKey:  "sk-ant-compat",
				BaseURL: "https://proxy.example.com/api/v1",
			},
			modelName: "claude-3-5-sonnet-20241022",
			wantErr:   false,
		},
		{
			name:         "gemini default endpoint",
			providerType: providers.TypeGemini,
			cred: providers.Credential{
				APIKey: "AIza-test",
			},
			modelName: "gemini-1.5-pro",
			wantErr:   false,
		},
		{
			name:         "gemini baseURL with stripped /v1beta",
			providerType: providers.TypeGemini,
			cred: providers.Credential{
				APIKey:  "AIza-test",
				BaseURL: "https://generativelanguage.googleapis.com/v1beta",
			},
			modelName: "gemini-1.5-pro",
			wantErr:   false,
		},
		{
			name:         "unsupported provider type",
			providerType: "unsupported-provider",
			cred: providers.Credential{
				APIKey: "test",
			},
			modelName: "test-model",
			wantErr:   true,
			errCheck: func(err error) bool {
				return errors.Is(err, domain.ErrInvalid)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := agents.DefaultAgenticModelFactory(ctx, tt.providerType, tt.cred, tt.modelName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DefaultAgenticModelFactory() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errCheck != nil && !tt.errCheck(err) {
					t.Errorf("error %v did not match expected check", err)
				}
				return
			}
			if m == nil {
				t.Fatalf("expected non-nil model for %s", tt.name)
			}
		})
	}
}
