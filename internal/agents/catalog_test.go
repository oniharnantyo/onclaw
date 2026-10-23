package agents

import (
	"testing"

	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

func TestIsKnownProviderType(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		known        bool
	}{
		{name: "anthropic", providerType: ProviderAnthropic, known: true},
		{name: "openai", providerType: ProviderOpenAI, known: true},
		{name: "gemini", providerType: ProviderGemini, known: true},
		{name: "openrouter", providerType: ProviderOpenRouter, known: true},
		{name: "openai-compatible", providerType: ProviderOpenAICompatible, known: true},
		{name: "anthropic-compatible", providerType: ProviderAnthropicCompatible, known: true},
		{name: "unknown type", providerType: "mystery", known: false},
		{name: "blank type", providerType: "", known: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.known, IsKnownProviderType(tt.providerType))
		})
	}
}

func TestDefaultBaseURL(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		baseURL      string
	}{
		{name: "anthropic", providerType: ProviderAnthropic, baseURL: "https://api.anthropic.com"},
		{name: "openai", providerType: ProviderOpenAI, baseURL: "https://api.openai.com/v1"},
		{name: "gemini", providerType: ProviderGemini, baseURL: "https://generativelanguage.googleapis.com"},
		{name: "openrouter fixed base", providerType: ProviderOpenRouter, baseURL: "https://openrouter.ai/api/v1"},
		{name: "openai-compatible has no default", providerType: ProviderOpenAICompatible, baseURL: ""},
		{name: "anthropic-compatible has no default", providerType: ProviderAnthropicCompatible, baseURL: ""},
		{name: "unknown type", providerType: "mystery", baseURL: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.baseURL, DefaultBaseURL(tt.providerType))
		})
	}
}

func TestCatalogModels_CompatibleTypesResolveEmpty(t *testing.T) {
	// The live-listed types are not in the static catalog; their listings
	// come from the provider's live endpoint and land in the stored model
	// rows instead.
	require.Nil(t, CatalogModels(ProviderOpenRouter))
	require.Nil(t, CatalogModels(ProviderOpenAICompatible))
	require.Nil(t, CatalogModels(ProviderAnthropicCompatible))
}

// TestEfforts_CatalogModels covers the effort capability of every canonical
// catalog model (design D2): reasoning models expose their native set in
// ascending order, non-reasoning models none, and an unlisted model on a
// canonical provider resolves to the empty set.
func TestEfforts_CatalogModels(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		model        string
		want         []string
	}{
		{name: "claude opus", providerType: ProviderAnthropic, model: "claude-opus-4-1", want: []string{"low", "medium", "high"}},
		{name: "claude sonnet", providerType: ProviderAnthropic, model: "claude-sonnet-4-5", want: []string{"low", "medium", "high"}},
		{name: "claude haiku", providerType: ProviderAnthropic, model: "claude-haiku-4-5", want: []string{"low", "medium", "high"}},
		{name: "gpt-5 carries minimal", providerType: ProviderOpenAI, model: "gpt-5", want: []string{"minimal", "low", "medium", "high"}},
		{name: "gpt-4o is not a reasoning model", providerType: ProviderOpenAI, model: "gpt-4o", want: nil},
		{name: "gpt-4o-mini is not a reasoning model", providerType: ProviderOpenAI, model: "gpt-4o-mini", want: nil},
		{name: "gemini pro", providerType: ProviderGemini, model: "gemini-2.5-pro", want: []string{"low", "medium", "high"}},
		{name: "gemini flash", providerType: ProviderGemini, model: "gemini-2.5-flash", want: []string{"low", "medium", "high"}},
		{name: "unlisted model on a canonical provider", providerType: ProviderAnthropic, model: "claude-unknown", want: nil},
		{name: "unknown provider type", providerType: "quantum", model: "anything", want: nil},
		{name: "blank model", providerType: ProviderOpenAI, model: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Efforts(tt.providerType, tt.model))
		})
	}
}

// TestEfforts_HeuristicFamilies covers the openrouter-family id heuristic
// (design D2): one hit per family, vendor-prefixed ids matching on their
// final segment, and unknown ids resolving to the empty set that hides the
// picker.
func TestEfforts_HeuristicFamilies(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  []string
	}{
		{name: "openrouter gpt-5 with vendor prefix", model: "openai/gpt-5-mini", want: []string{"minimal", "low", "medium", "high"}},
		{name: "openrouter o-series", model: "openai/o4-mini", want: []string{"low", "medium", "high"}},
		{name: "openrouter claude", model: "anthropic/claude-sonnet-4.5", want: []string{"low", "medium", "high"}},
		{name: "openrouter gemini", model: "google/gemini-2.5-flash", want: []string{"low", "medium", "high"}},
		{name: "bare gpt-5 id", model: "gpt-5", want: []string{"minimal", "low", "medium", "high"}},
		{name: "bare claude id", model: "claude-3-5-sonnet", want: []string{"low", "medium", "high"}},
		{name: "unknown id hides the picker", model: "mistralai/mistral-large", want: nil},
		{name: "word-like o id is not the o-series", model: "org/official-model", want: nil},
		{name: "blank id", model: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The heuristic serves the live-listed types only; every entry
			// must resolve the same on each of them.
			for _, providerType := range []string{
				ProviderOpenRouter, ProviderOpenAICompatible, ProviderAnthropicCompatible,
			} {
				require.Equal(t, tt.want, Efforts(providerType, tt.model), "provider type %s", providerType)
			}
		})
	}
}

// TestModelInputCapabilities pins the catalog's multimodal input flags the
// ModelCapability DTO and the attachment degradation read.
func TestModelInputCapabilities(t *testing.T) {
	require.True(t, SupportsImageInput(ProviderAnthropic, "claude-sonnet-4-5"))
	require.True(t, SupportsPDFInput(ProviderAnthropic, "claude-sonnet-4-5"))
	require.True(t, SupportsImageInput(ProviderOpenAI, "gpt-4o"))
	require.False(t, SupportsPDFInput(ProviderOpenAI, "gpt-4o"))
	require.True(t, SupportsPDFInput(ProviderGemini, "gemini-2.5-pro"))
	require.False(t, SupportsImageInput(ProviderAnthropic, "claude-unknown"), "unlisted models degrade to text-only")
	require.False(t, SupportsPDFInput("quantum", "anything"))
}

func TestSupportsResponsesAPI(t *testing.T) {
	tests := []struct {
		name          string
		provider      store.Provider
		wantSupported bool
		wantReasonSub string
	}{
		{
			name: "native openai default base",
			provider: store.Provider{
				Type:    ProviderOpenAI,
				BaseURL: "https://api.openai.com/v1",
			},
			wantSupported: true,
		},
		{
			name: "compatible-with-responses custom proxy",
			provider: store.Provider{
				Type:    ProviderOpenAICompatible,
				BaseURL: "https://responses-proxy.company.internal/v1",
				Name:    "Internal Responses Proxy",
			},
			wantSupported: true,
		},
		{
			name: "compatible-without: LM Studio default port",
			provider: store.Provider{
				Type:    ProviderOpenAICompatible,
				BaseURL: "http://localhost:1234/v1",
				Name:    "LM Studio Local",
			},
			wantSupported: false,
			wantReasonSub: "LM Studio only supports /v1/chat/completions",
		},
		{
			name: "compatible-without: Poolside",
			provider: store.Provider{
				Type:    ProviderOpenAICompatible,
				BaseURL: "https://api.poolside.ai/v1",
				Name:    "Poolside Laguna",
			},
			wantSupported: false,
			wantReasonSub: "Poolside only supports /v1/chat/completions",
		},
		{
			name: "compatible-without: Ollama local default port",
			provider: store.Provider{
				Type:    ProviderOpenAICompatible,
				BaseURL: "http://localhost:11434/v1",
				Name:    "Ollama Server",
			},
			wantSupported: false,
			wantReasonSub: "Ollama lacks native /v1/responses support",
		},
		{
			name: "openrouter lacking responses",
			provider: store.Provider{
				Type:    ProviderOpenRouter,
				BaseURL: "https://openrouter.ai/api/v1",
			},
			wantSupported: false,
			wantReasonSub: "OpenRouter only supports /v1/chat/completions",
		},
		{
			name: "explicit without-responses flag",
			provider: store.Provider{
				Type:    ProviderOpenAICompatible,
				BaseURL: "http://proxy.local/v1?responses=false",
			},
			wantSupported: false,
			wantReasonSub: "endpoint does not implement required /v1/responses API",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			supported, reason := SupportsResponsesAPI(tt.provider)
			require.Equal(t, tt.wantSupported, supported)
			if !tt.wantSupported {
				require.Contains(t, reason, tt.wantReasonSub)
			}
		})
	}
}

func TestCheckTurnCapability(t *testing.T) {
	tests := []struct {
		name       string
		provider   store.Provider
		model      string
		wantErrIs  error
		wantErrSub string
	}{
		{
			name: "native openai passes",
			provider: store.Provider{
				Type:    ProviderOpenAI,
				APIKey:  "sk-test",
				BaseURL: "https://api.openai.com/v1",
				Name:    "OpenAI",
			},
			model: "gpt-5",
		},
		{
			name: "compatible-with-responses passes",
			provider: store.Provider{
				Type:    ProviderOpenAICompatible,
				BaseURL: "https://responses.internal.ai/v1",
				Name:    "Responses Service",
			},
			model: "custom-model",
		},
		{
			name: "compatible-without LM Studio fails",
			provider: store.Provider{
				Type:    ProviderOpenAICompatible,
				BaseURL: "http://localhost:1234/v1",
				Name:    "LM Studio",
			},
			model:      "qwen2.5-coder",
			wantErrIs:  ErrEndpointIncompatible,
			wantErrSub: "LM Studio only supports /v1/chat/completions",
		},
		{
			name: "compatible-without Ollama fails",
			provider: store.Provider{
				Type:    ProviderOpenAICompatible,
				BaseURL: "http://localhost:11434/v1",
				Name:    "Ollama",
			},
			model:      "llama3",
			wantErrIs:  ErrEndpointIncompatible,
			wantErrSub: "Ollama lacks native /v1/responses support",
		},
		{
			name: "anthropic passes without /v1/responses",
			provider: store.Provider{
				Type:   ProviderAnthropic,
				APIKey: "sk-ant-test",
				Name:   "Anthropic",
			},
			model: "claude-sonnet-4-5",
		},
		{
			name: "gemini passes without /v1/responses",
			provider: store.Provider{
				Type:   ProviderGemini,
				APIKey: "ai-test",
				Name:   "Gemini",
			},
			model: "gemini-2.5-pro",
		},
		{
			name: "missing api key rejected",
			provider: store.Provider{
				Type: ProviderOpenAI,
				Name: "OpenAI No Key",
			},
			wantErrIs: ErrProviderNotConfigured,
		},
		{
			name: "unknown provider type rejected",
			provider: store.Provider{
				Type: "unknown-vendor",
				Name: "Mystery",
			},
			wantErrIs: ErrProviderUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckTurnCapability(tt.provider, tt.model)
			if tt.wantErrIs != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.wantErrIs)
				if tt.wantErrSub != "" {
					require.Contains(t, err.Error(), tt.wantErrSub)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOllamaGatingGranularity(t *testing.T) {
	ollamaProvider := store.Provider{
		Type:    ProviderOpenAICompatible,
		BaseURL: "http://localhost:11434/v1",
		Name:    "Ollama Local",
	}

	for _, model := range []string{"llama3.2", "llama3.2-vision", "llava", "qwen2.5"} {
		err := CheckTurnCapability(ollamaProvider, model)
		require.ErrorIs(t, err, ErrEndpointIncompatible)
		require.Contains(t, err.Error(), "Ollama lacks native /v1/responses support")
	}
}

func TestCatalogProviderModels_CarriesEfforts(t *testing.T) {
	claudeModels := CatalogProviderModels("p1", ProviderAnthropic)
	require.NotEmpty(t, claudeModels)
	for _, m := range claudeModels {
		require.Equal(t, []string{"low", "medium", "high"}, m.Efforts)
	}

	openaiModels := CatalogProviderModels("p2", ProviderOpenAI)
	require.NotEmpty(t, openaiModels)
	for _, m := range openaiModels {
		if m.ModelID == "gpt-5" {
			require.Equal(t, []string{"minimal", "low", "medium", "high"}, m.Efforts)
		} else {
			require.Empty(t, m.Efforts)
		}
	}

	geminiModels := CatalogProviderModels("p3", ProviderGemini)
	require.NotEmpty(t, geminiModels)
	for _, m := range geminiModels {
		require.True(t, m.VideoInput, "Gemini models declare video input capability")
	}
}
