package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/buddies/internal/store"
	"github.com/stretchr/testify/require"
)

func TestBuildModel_MapsEveryKnownType(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		baseURL      string
	}{
		{name: "anthropic", providerType: ProviderAnthropic},
		{name: "openai", providerType: ProviderOpenAI},
		{name: "gemini", providerType: ProviderGemini},
		{name: "openrouter", providerType: ProviderOpenRouter},
		{name: "openai-compatible with user base", providerType: ProviderOpenAICompatible, baseURL: "https://relay.example.internal/v1"},
		{name: "anthropic-compatible with user base", providerType: ProviderAnthropicCompatible, baseURL: "https://relay.example.internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := buildModel(context.Background(), tt.providerType, store.Provider{
				Name:    "p",
				Type:    tt.providerType,
				APIKey:  "k",
				BaseURL: tt.baseURL,
			}, "some-model", DefaultMaxTokens, "")
			require.NoError(t, err)
			require.NotNil(t, m)
		})
	}
}

func TestBuildModel_OpenRouterBlankBaseGetsFixedBase(t *testing.T) {
	// Constructs without error even with a blank base: the fixed OpenRouter
	// root is substituted instead of the OpenAI SDK default.
	m, err := buildModel(context.Background(), ProviderOpenRouter, store.Provider{
		Name: "p", Type: ProviderOpenRouter, APIKey: "k",
	}, "some-model", DefaultMaxTokens, "")
	require.NoError(t, err)
	require.NotNil(t, m)
}

func TestBuildModel_UnknownTypeUnsupported(t *testing.T) {
	_, err := buildModel(context.Background(), "mystery", store.Provider{Name: "p", Type: "mystery", APIKey: "k"}, "some-model", DefaultMaxTokens, "")
	require.ErrorIs(t, err, ErrProviderUnsupported)
}

func TestBuildAgenticModel_MapsEveryKnownType(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		baseURL      string
		effort       string
	}{
		{name: "anthropic", providerType: ProviderAnthropic, effort: EffortLow},
		{name: "openai", providerType: ProviderOpenAI, effort: EffortHigh},
		{name: "gemini", providerType: ProviderGemini, effort: EffortMedium},
		{name: "openrouter", providerType: ProviderOpenRouter},
		{name: "openai-compatible with user base", providerType: ProviderOpenAICompatible, baseURL: "https://relay.example.internal/v1"},
		{name: "anthropic-compatible with user base", providerType: ProviderAnthropicCompatible, baseURL: "https://relay.example.internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := buildAgenticModel(context.Background(), tt.providerType, store.Provider{
				Name:    "p",
				Type:    tt.providerType,
				APIKey:  "k",
				BaseURL: tt.baseURL,
			}, "some-model", DefaultMaxTokens, tt.effort)
			require.NoError(t, err)
			require.NotNil(t, m)
		})
	}
}

func TestBuildAgenticModel_OpenRouterBlankBaseGetsFixedBase(t *testing.T) {
	m, err := buildAgenticModel(context.Background(), ProviderOpenRouter, store.Provider{
		Name: "p", Type: ProviderOpenRouter, APIKey: "k",
	}, "some-model", DefaultMaxTokens, "")
	require.NoError(t, err)
	require.NotNil(t, m)
}

func TestBuildAgenticModel_UnknownTypeUnsupported(t *testing.T) {
	_, err := buildAgenticModel(context.Background(), "mystery", store.Provider{Name: "p", Type: "mystery", APIKey: "k"}, "some-model", DefaultMaxTokens, "")
	require.ErrorIs(t, err, ErrProviderUnsupported)
}

// captureChatServer returns a test endpoint that records one request's JSON
// body and replies with a minimal successful streaming completion for the
// provider type (SSE — the shape the turn path runs with). The openai-style
// path also matches the compatible types.
func captureChatServer(t *testing.T, providerType string, body *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, body))

		w.Header().Set("Content-Type", "text/event-stream")
		switch providerType {
		case ProviderAnthropic, ProviderAnthropicCompatible:
			_, _ = w.Write([]byte("event: message_start\n" +
				`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n" +
				"event: content_block_start\n" +
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
				"event: content_block_delta\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\n" +
				"event: content_block_stop\n" +
				`data: {"type":"content_block_stop","index":0}` + "\n\n" +
				"event: message_delta\n" +
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` + "\n\n" +
				"event: message_stop\n" +
				`data: {"type":"message_stop"}` + "\n\n"))
		case ProviderGemini:
			_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}` + "\n\n"))
		default:
			_, _ = w.Write([]byte(`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}` + "\n\n" +
				"data: [DONE]\n\n"))
		}
	}))
}

// drainStream generates one trivial completion through the built model's
// streaming path (the path turns run with) so the provider request lands on
// the capture server.
func drainStream(t *testing.T, m model.BaseModel[*schema.AgenticMessage]) {
	t.Helper()
	msg := &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeUser,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.UserInputText{Text: "hi"}),
		},
	}
	sr, err := m.Stream(context.Background(), []*schema.AgenticMessage{msg})
	require.NoError(t, err)
	for {
		if _, err := sr.Recv(); errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			require.NoError(t, err)
		}
	}
}

// TestBuildModel_EffortMapsToNativeKnob covers design D2's mapping end to
// end: per provider and effort, the provider request carries the native
// reasoning parameter — reasoning_effort for OpenAI, the thinking budget for
// Claude, the thinking budget for Gemini — and unset it carries none
// (parameter omitted entirely, provider default).
func TestBuildModel_EffortMapsToNativeKnob(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		effort       string
		assert       func(t *testing.T, body map[string]any)
	}{
		{
			name:         "openai low sets reasoning_effort",
			providerType: ProviderOpenAI,
			effort:       EffortLow,
			assert: func(t *testing.T, body map[string]any) {
				require.Equal(t, "low", body["reasoning_effort"])
			},
		},
		{
			name:         "openai high sets reasoning_effort",
			providerType: ProviderOpenAI,
			effort:       EffortHigh,
			assert: func(t *testing.T, body map[string]any) {
				require.Equal(t, "high", body["reasoning_effort"])
			},
		},
		{
			name:         "openai minimal rides verbatim (gpt-5 family)",
			providerType: ProviderOpenAI,
			effort:       EffortMinimal,
			assert: func(t *testing.T, body map[string]any) {
				require.Equal(t, "minimal", body["reasoning_effort"])
			},
		},
		{
			name:         "openai unset omits reasoning_effort entirely",
			providerType: ProviderOpenAI,
			effort:       "",
			assert: func(t *testing.T, body map[string]any) {
				require.NotContains(t, body, "reasoning_effort")
			},
		},
		{
			name:         "claude low sets the 4k thinking budget",
			providerType: ProviderAnthropic,
			effort:       EffortLow,
			assert: func(t *testing.T, body map[string]any) {
				thinking, ok := body["thinking"].(map[string]any)
				require.True(t, ok, "the request must carry the thinking config")
				require.Equal(t, "enabled", thinking["type"])
				require.Equal(t, float64(4096), thinking["budget_tokens"])
				require.Greater(t, body["max_tokens"], thinking["budget_tokens"],
					"max_tokens must exceed the budget (Anthropic API requirement)")
			},
		},
		{
			name:         "claude medium sets the 16k thinking budget",
			providerType: ProviderAnthropic,
			effort:       EffortMedium,
			assert: func(t *testing.T, body map[string]any) {
				thinking := body["thinking"].(map[string]any)
				require.Equal(t, float64(16384), thinking["budget_tokens"])
			},
		},
		{
			name:         "claude high sets the 32k budget and raises max_tokens",
			providerType: ProviderAnthropic,
			effort:       EffortHigh,
			assert: func(t *testing.T, body map[string]any) {
				thinking := body["thinking"].(map[string]any)
				require.Equal(t, float64(32768), thinking["budget_tokens"])
				require.Greater(t, body["max_tokens"], thinking["budget_tokens"],
					"the default cap must be raised above the budget")
			},
		},
		{
			name:         "claude unset omits thinking entirely",
			providerType: ProviderAnthropic,
			effort:       "",
			assert: func(t *testing.T, body map[string]any) {
				require.NotContains(t, body, "thinking")
			},
		},
		{
			name:         "gemini low sets the 512 thinking budget",
			providerType: ProviderGemini,
			effort:       EffortLow,
			assert: func(t *testing.T, body map[string]any) {
				cfg := body["generationConfig"].(map[string]any)
				thinking := cfg["thinkingConfig"].(map[string]any)
				require.Equal(t, float64(512), thinking["thinkingBudget"])
			},
		},
		{
			name:         "gemini medium sets the 2048 thinking budget",
			providerType: ProviderGemini,
			effort:       EffortMedium,
			assert: func(t *testing.T, body map[string]any) {
				cfg := body["generationConfig"].(map[string]any)
				thinking := cfg["thinkingConfig"].(map[string]any)
				require.Equal(t, float64(2048), thinking["thinkingBudget"])
			},
		},
		{
			name:         "gemini high sets the 8192 thinking budget",
			providerType: ProviderGemini,
			effort:       EffortHigh,
			assert: func(t *testing.T, body map[string]any) {
				cfg := body["generationConfig"].(map[string]any)
				thinking := cfg["thinkingConfig"].(map[string]any)
				require.Equal(t, float64(8192), thinking["thinkingBudget"])
			},
		},
		{
			name:         "gemini unset omits thinkingConfig entirely",
			providerType: ProviderGemini,
			effort:       "",
			assert: func(t *testing.T, body map[string]any) {
				cfg, ok := body["generationConfig"].(map[string]any)
				if ok {
					require.NotContains(t, cfg, "thinkingConfig")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]any
			server := captureChatServer(t, tt.providerType, &body)
			defer server.Close()

			m, err := buildModel(context.Background(), tt.providerType, store.Provider{
				Name: "p", Type: tt.providerType, APIKey: "k", BaseURL: server.URL,
			}, "some-model", DefaultMaxTokens, tt.effort)
			require.NoError(t, err)

			drainStream(t, m)
			tt.assert(t, body)
		})
	}
}
