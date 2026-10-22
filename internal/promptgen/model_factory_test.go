package promptgen

import (
	"context"
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

func TestDefaultModelFactory_BuildsEveryKnownType(t *testing.T) {
	tests := []struct {
		name      string
		provider  store.Provider
		modelName string
	}{
		{
			name:      "anthropic",
			provider:  store.Provider{Type: providerAnthropic, APIKey: "sk-ant-test"},
			modelName: "claude-sonnet-4-5",
		},
		{
			name:      "openai",
			provider:  store.Provider{Type: providerOpenAI, APIKey: "sk-test-openai"},
			modelName: "gpt-4o",
		},
		{
			name:      "gemini",
			provider:  store.Provider{Type: providerGemini, APIKey: "gemini-key"},
			modelName: "gemini-2.5-pro",
		},
		{
			name:      "openrouter",
			provider:  store.Provider{Type: providerOpenRouter, APIKey: "sk-or-test"},
			modelName: "anthropic/claude-3.5-sonnet",
		},
		{
			name:      "openai-compatible with user base",
			provider:  store.Provider{Type: providerOpenAICompatible, APIKey: "sk-test", BaseURL: "https://custom-ai.example/v1"},
			modelName: "local-model",
		},
		{
			name:      "anthropic-compatible with user base",
			provider:  store.Provider{Type: providerAnthropicCompatible, APIKey: "sk-test", BaseURL: "https://custom-claude.example"},
			modelName: "claude-custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewDefaultModelFactory().Build(context.Background(), tt.provider, tt.modelName)
			require.NoError(t, err)
			require.NotNil(t, m)
		})
	}

	// The reconciled list must cover every provider type the runtime knows,
	// so a type added in internal/agents/catalog.go cannot silently fall to
	// the unsupported branch here.
	require.Len(t, knownProviderTypes, 6)
}

func TestDefaultModelFactory_OpenRouterBlankBaseBuilds(t *testing.T) {
	// Constructs without error even with a blank base: the fixed OpenRouter
	// root is substituted instead of the OpenAI SDK default.
	m, err := NewDefaultModelFactory().Build(context.Background(), store.Provider{
		Type: providerOpenRouter, APIKey: "k",
	}, "some-model")
	require.NoError(t, err)
	require.NotNil(t, m)
}

func TestDefaultModelFactory_UnknownTypeUnsupported(t *testing.T) {
	_, err := NewDefaultModelFactory().Build(context.Background(), store.Provider{Type: "mystery", APIKey: "k"}, "some-model")
	require.ErrorIs(t, err, errProviderUnsupported)
}
