package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
)

func TestRegistry_BuiltinProviders(t *testing.T) {
	reg := providers.NewRegistry()

	expectedTypes := []string{
		providers.TypeAnthropic,
		providers.TypeAnthropicCompatible,
		providers.TypeGemini,
		providers.TypeOpenAI,
		providers.TypeOpenAICompatible,
		providers.TypeOpenRouter,
		providers.TypeTypeSafe,
	}

	list := reg.List()
	if len(list) != len(expectedTypes) {
		t.Fatalf("reg.List() length = %d, want %d", len(list), len(expectedTypes))
	}

	for i, name := range expectedTypes {
		if list[i] != name {
			t.Errorf("reg.List()[%d] = %q, want %q", i, list[i], name)
		}
		if !reg.IsValidType(name) {
			t.Errorf("reg.IsValidType(%q) = false, want true", name)
		}

		p, err := reg.Get(name)
		if err != nil {
			t.Errorf("reg.Get(%q) error: %v", name, err)
		}
		if p.Type() != name {
			t.Errorf("provider Type() = %q, want %q", p.Type(), name)
		}
	}

	// Unknown type
	_, err := reg.Get("nonexistent-type")
	if err == nil {
		t.Errorf("reg.Get(nonexistent) expected error, got nil")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("reg.Get(nonexistent) error = %v, want ErrInvalid sentinel wrapped", err)
	}
}

func TestProvider_RequiresAPIKey(t *testing.T) {
	reg := providers.NewRegistry()

	tests := []struct {
		providerType string
		want         bool
	}{
		{providers.TypeOpenAI, true},
		{providers.TypeAnthropic, true},
		{providers.TypeGemini, true},
		{providers.TypeOpenRouter, true},
		{providers.TypeOpenAICompatible, false},
		{providers.TypeAnthropicCompatible, false},
		{providers.TypeTypeSafe, true},
	}

	for _, tt := range tests {
		p, err := reg.Get(tt.providerType)
		if err != nil {
			t.Errorf("reg.Get(%q) error: %v", tt.providerType, err)
			continue
		}
		if got := p.RequiresAPIKey(); got != tt.want {
			t.Errorf("%s.RequiresAPIKey() = %v, want %v", tt.providerType, got, tt.want)
		}
	}
}

func TestRegistry_Panics(t *testing.T) {
	reg := providers.NewRegistry()

	// Duplicate registration
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic on duplicate registration")
			}
		}()
		reg.Register(providers.NewOpenAIProvider())
	}()

	// Nil registration
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic on nil registration")
			}
		}()
		reg.Register(nil)
	}()
}

func TestOpenAIProvider_Verify(t *testing.T) {
	ctx := context.Background()

	t.Run("success with default path and auth header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer sk-test-openai" {
				t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("User-Agent") != "onclaw" {
				t.Errorf("unexpected User-Agent header: %s", r.Header.Get("User-Agent"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "gpt-4o"}]}`))
		}))
		defer server.Close()

		p := providers.NewOpenAIProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenAI,
			BaseURL: server.URL + "/v1",
			APIKey:  "sk-test-openai",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("auth error response parsed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": {"message": "Incorrect API key provided", "type": "invalid_request_error"}}`))
		}))
		defer server.Close()

		p := providers.NewOpenAIProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenAI,
			BaseURL: server.URL + "/v1",
			APIKey:  "sk-bad-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on 401, got nil")
		}
		if expected := "provider error (401): Incorrect API key provided"; err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("missing API key fails fast", func(t *testing.T) {
		p := providers.NewOpenAIProvider()
		err := p.Verify(ctx, providers.Credential{
			Type:   providers.TypeOpenAI,
			APIKey: "",
		})
		if err == nil {
			t.Fatalf("Verify expected error on empty key, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("error = %v, want ErrInvalid sentinel wrapped", err)
		}
	})

	t.Run("invalid base_url scheme", func(t *testing.T) {
		p := providers.NewOpenAIProvider()
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenAI,
			BaseURL: "ftp://openai.example.com",
			APIKey:  "sk-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on ftp scheme, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("error = %v, want ErrInvalid sentinel wrapped", err)
		}
	})

	t.Run("unreachable transport error", func(t *testing.T) {
		p := providers.NewOpenAIProvider()
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenAI,
			BaseURL: "http://127.0.0.1:54321", // closed port
			APIKey:  "sk-key",
		})
		if err == nil {
			t.Fatalf("Verify expected transport error, got nil")
		}
	})
}

func TestAnthropicProvider_Verify(t *testing.T) {
	ctx := context.Background()

	t.Run("success with x-api-key and version headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("x-api-key") != "anthropic-secret-key" {
				t.Errorf("unexpected x-api-key: %s", r.Header.Get("x-api-key"))
			}
			if r.Header.Get("anthropic-version") != "2023-06-01" {
				t.Errorf("unexpected anthropic-version: %s", r.Header.Get("anthropic-version"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "claude-3-5-sonnet-20241022"}]}`))
		}))
		defer server.Close()

		p := providers.NewAnthropicProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeAnthropic,
			BaseURL: server.URL + "/v1",
			APIKey:  "anthropic-secret-key",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("auth error response parsed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type": "error", "error": {"type": "authentication_error", "message": "invalid x-api-key"}}`))
		}))
		defer server.Close()

		p := providers.NewAnthropicProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeAnthropic,
			BaseURL: server.URL + "/v1",
			APIKey:  "bad-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on 401, got nil")
		}
		if expected := "provider error (401): invalid x-api-key"; err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		p := providers.NewAnthropicProvider()
		err := p.Verify(ctx, providers.Credential{
			Type:   providers.TypeAnthropic,
			APIKey: "",
		})
		if err == nil {
			t.Fatalf("Verify expected error on empty key, got nil")
		}
	})
}

func TestGeminiProvider_Verify(t *testing.T) {
	ctx := context.Background()

	t.Run("success with x-goog-api-key header and /v1beta/models path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1beta/models" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("x-goog-api-key") != "gemini-test-key" {
				t.Errorf("unexpected x-goog-api-key: %s", r.Header.Get("x-goog-api-key"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "models/gemini-1.5-pro"}]}`))
		}))
		defer server.Close()

		p := providers.NewGeminiProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeGemini,
			BaseURL: server.URL + "/v1beta",
			APIKey:  "gemini-test-key",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("invalid key error parsed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": {"code": 400, "message": "API key not valid. Please pass a valid API key.", "status": "INVALID_ARGUMENT"}}`))
		}))
		defer server.Close()

		p := providers.NewGeminiProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeGemini,
			BaseURL: server.URL + "/v1beta",
			APIKey:  "bad-gemini-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on 400, got nil")
		}
		if expected := "provider error (400): API key not valid. Please pass a valid API key."; err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		p := providers.NewGeminiProvider()
		err := p.Verify(ctx, providers.Credential{
			Type:   providers.TypeGemini,
			APIKey: "",
		})
		if err == nil {
			t.Fatalf("Verify expected error on empty key, got nil")
		}
	})
}

func TestOpenRouterProvider_Verify(t *testing.T) {
	ctx := context.Background()

	t.Run("success probing /api/v1/key endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/key" {
				t.Errorf("unexpected path for OpenRouter probe: %s, want /api/v1/key", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer sk-or-v1-secret" {
				t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": {"label": "my key", "limit": 100}}`))
		}))
		defer server.Close()

		p := providers.NewOpenRouterProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenRouter,
			BaseURL: server.URL + "/api/v1",
			APIKey:  "sk-or-v1-secret",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("auth error response parsed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": {"message": "Invalid API key"}}`))
		}))
		defer server.Close()

		p := providers.NewOpenRouterProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenRouter,
			BaseURL: server.URL + "/api/v1",
			APIKey:  "bad-openrouter-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on 401, got nil")
		}
		if expected := "provider error (401): Invalid API key"; err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		p := providers.NewOpenRouterProvider()
		err := p.Verify(ctx, providers.Credential{
			Type:   providers.TypeOpenRouter,
			APIKey: "",
		})
		if err == nil {
			t.Fatalf("Verify expected error on empty key, got nil")
		}
	})
}

func TestOpenAICompatibleProvider_Verify(t *testing.T) {
	ctx := context.Background()

	t.Run("success with required base_url and key", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer custom-key" {
				t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": []}`))
		}))
		defer server.Close()

		p := providers.NewOpenAICompatibleProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenAICompatible,
			BaseURL: server.URL + "/v1",
			APIKey:  "custom-key",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("success without key (e.g. unauthenticated local server)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("expected no Authorization header, got: %s", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": []}`))
		}))
		defer server.Close()

		p := providers.NewOpenAICompatibleProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenAICompatible,
			BaseURL: server.URL + "/v1",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("missing base_url fails", func(t *testing.T) {
		p := providers.NewOpenAICompatibleProvider()
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenAICompatible,
			BaseURL: "",
			APIKey:  "some-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on missing base_url, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("error = %v, want ErrInvalid sentinel wrapped", err)
		}
	})

	t.Run("server error with detail parsed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"detail": "Model engine failed to load"}`))
		}))
		defer server.Close()

		p := providers.NewOpenAICompatibleProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeOpenAICompatible,
			BaseURL: server.URL + "/v1",
		})
		if err == nil {
			t.Fatalf("Verify expected error on 500, got nil")
		}
		if expected := "provider error (500): Model engine failed to load"; err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})
}

func TestAnthropicCompatibleProvider_Verify(t *testing.T) {
	ctx := context.Background()

	t.Run("success with required base_url and key", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("x-api-key") != "custom-anthropic-key" {
				t.Errorf("unexpected x-api-key: %s", r.Header.Get("x-api-key"))
			}
			if r.Header.Get("anthropic-version") != "2023-06-01" {
				t.Errorf("unexpected anthropic-version: %s", r.Header.Get("anthropic-version"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": []}`))
		}))
		defer server.Close()

		p := providers.NewAnthropicCompatibleProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeAnthropicCompatible,
			BaseURL: server.URL + "/v1",
			APIKey:  "custom-anthropic-key",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("missing base_url fails", func(t *testing.T) {
		p := providers.NewAnthropicCompatibleProvider()
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeAnthropicCompatible,
			BaseURL: "",
			APIKey:  "some-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on missing base_url, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("error = %v, want ErrInvalid sentinel wrapped", err)
		}
	})

	t.Run("plain text 404 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not Found: custom proxy endpoint"))
		}))
		defer server.Close()

		p := providers.NewAnthropicCompatibleProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeAnthropicCompatible,
			BaseURL: server.URL + "/v1",
		})
		if err == nil {
			t.Fatalf("Verify expected error on 404, got nil")
		}
		if expected := "provider error (404): Not Found: custom proxy endpoint"; err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})
}

func TestProvider_CapabilityFloors(t *testing.T) {
	reg := providers.NewRegistry()

	tests := []struct {
		providerType        string
		requiresMaxTokens   bool
		expectedEfforts     []string
		supportsTemperature bool
	}{
		{
			providerType:        providers.TypeOpenAI,
			requiresMaxTokens:   false,
			expectedEfforts:     []string{"low", "medium", "high"},
			supportsTemperature: true,
		},
		{
			providerType:        providers.TypeOpenAICompatible,
			requiresMaxTokens:   false,
			expectedEfforts:     []string{"low", "medium", "high"},
			supportsTemperature: true,
		},
		{
			providerType:        providers.TypeAnthropic,
			requiresMaxTokens:   true,
			expectedEfforts:     []string{},
			supportsTemperature: true,
		},
		{
			providerType:        providers.TypeAnthropicCompatible,
			requiresMaxTokens:   true,
			expectedEfforts:     []string{},
			supportsTemperature: true,
		},
		{
			providerType:        providers.TypeGemini,
			requiresMaxTokens:   false,
			expectedEfforts:     []string{},
			supportsTemperature: true,
		},
		{
			providerType:        providers.TypeOpenRouter,
			requiresMaxTokens:   false,
			expectedEfforts:     []string{},
			supportsTemperature: true,
		},
		{
			providerType:        providers.TypeTypeSafe,
			requiresMaxTokens:   false,
			expectedEfforts:     []string{},
			supportsTemperature: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.providerType, func(t *testing.T) {
			p, err := reg.Get(tt.providerType)
			if err != nil {
				t.Fatalf("reg.Get(%q) failed: %v", tt.providerType, err)
			}

			if got := p.RequiresMaxTokens(); got != tt.requiresMaxTokens {
				t.Errorf("%s RequiresMaxTokens() = %v, want %v", tt.providerType, got, tt.requiresMaxTokens)
			}

			efforts := p.ValidEfforts()
			if len(efforts) != len(tt.expectedEfforts) {
				t.Errorf("%s ValidEfforts() length = %d, want %d", tt.providerType, len(efforts), len(tt.expectedEfforts))
			} else {
				for i := range efforts {
					if efforts[i] != tt.expectedEfforts[i] {
						t.Errorf("%s ValidEfforts()[%d] = %q, want %q", tt.providerType, i, efforts[i], tt.expectedEfforts[i])
					}
				}
			}

			if got := p.SupportsTemperature(); got != tt.supportsTemperature {
				t.Errorf("%s SupportsTemperature() = %v, want %v", tt.providerType, got, tt.supportsTemperature)
			}
		})
	}
}

func TestOpenAIProvider_ListModels(t *testing.T) {
	ctx := context.Background()

	t.Run("success parsing /v1/models data", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer sk-test" {
				t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "gpt-4o", "name": "GPT-4o"}, {"id": "o1"}]}`))
		}))
		defer server.Close()

		p := providers.NewOpenAIProvider(server.Client())
		models, err := p.ListModels(ctx, providers.Credential{
			Type:    providers.TypeOpenAI,
			BaseURL: server.URL + "/v1",
			APIKey:  "sk-test",
		})
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		if len(models) != 2 {
			t.Fatalf("models len = %d, want 2", len(models))
		}
		if models[0].ID != "gpt-4o" || models[0].Name != "GPT-4o" {
			t.Errorf("models[0] = %+v, want gpt-4o", models[0])
		}
		if models[1].ID != "o1" || models[1].Name != "o1" {
			t.Errorf("models[1] = %+v, want o1 (fallback name to id)", models[1])
		}
	})

	t.Run("missing key", func(t *testing.T) {
		p := providers.NewOpenAIProvider()
		_, err := p.ListModels(ctx, providers.Credential{Type: providers.TypeOpenAI})
		if err == nil {
			t.Fatalf("expected error on missing key")
		}
	})
}

func TestOpenAICompatibleProvider_ListModels(t *testing.T) {
	ctx := context.Background()

	t.Run("success with custom base URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "llama-3-70b"}]}`))
		}))
		defer server.Close()

		p := providers.NewOpenAICompatibleProvider(server.Client())
		models, err := p.ListModels(ctx, providers.Credential{
			Type:    providers.TypeOpenAICompatible,
			BaseURL: server.URL + "/v1",
		})
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		if len(models) != 1 || models[0].ID != "llama-3-70b" {
			t.Errorf("models = %+v, want 1 item llama-3-70b", models)
		}
	})

	t.Run("missing base_url", func(t *testing.T) {
		p := providers.NewOpenAICompatibleProvider()
		_, err := p.ListModels(ctx, providers.Credential{Type: providers.TypeOpenAICompatible})
		if err == nil {
			t.Fatalf("expected error on missing base_url")
		}
	})
}

func TestAnthropicProvider_ListModels(t *testing.T) {
	ctx := context.Background()

	t.Run("success parsing /v1/models with display_name", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-api-key") != "ant-key" {
				t.Errorf("unexpected x-api-key: %s", r.Header.Get("x-api-key"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "claude-3-5-sonnet-20241022", "display_name": "Claude 3.5 Sonnet"}]}`))
		}))
		defer server.Close()

		p := providers.NewAnthropicProvider(server.Client())
		models, err := p.ListModels(ctx, providers.Credential{
			Type:    providers.TypeAnthropic,
			BaseURL: server.URL + "/v1",
			APIKey:  "ant-key",
		})
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("models len = %d, want 1", len(models))
		}
		if models[0].ID != "claude-3-5-sonnet-20241022" || models[0].Name != "Claude 3.5 Sonnet" {
			t.Errorf("models[0] = %+v", models[0])
		}
	})

	t.Run("missing key", func(t *testing.T) {
		p := providers.NewAnthropicProvider()
		_, err := p.ListModels(ctx, providers.Credential{Type: providers.TypeAnthropic})
		if err == nil {
			t.Fatalf("expected error on missing key")
		}
	})
}

func TestAnthropicCompatibleProvider_ListModels(t *testing.T) {
	ctx := context.Background()

	t.Run("success with custom base URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "custom-claude", "name": "Custom Claude"}]}`))
		}))
		defer server.Close()

		p := providers.NewAnthropicCompatibleProvider(server.Client())
		models, err := p.ListModels(ctx, providers.Credential{
			Type:    providers.TypeAnthropicCompatible,
			BaseURL: server.URL + "/v1",
		})
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		if len(models) != 1 || models[0].ID != "custom-claude" {
			t.Errorf("models = %+v", models)
		}
	})
}

func TestGeminiProvider_ListModels(t *testing.T) {
	ctx := context.Background()

	t.Run("success stripping models/ prefix and using displayName", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-goog-api-key") != "gem-key" {
				t.Errorf("unexpected key header: %s", r.Header.Get("x-goog-api-key"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "models/gemini-1.5-pro", "displayName": "Gemini 1.5 Pro"}]}`))
		}))
		defer server.Close()

		p := providers.NewGeminiProvider(server.Client())
		models, err := p.ListModels(ctx, providers.Credential{
			Type:    providers.TypeGemini,
			BaseURL: server.URL + "/v1beta",
			APIKey:  "gem-key",
		})
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("models len = %d, want 1", len(models))
		}
		if models[0].ID != "gemini-1.5-pro" || models[0].Name != "Gemini 1.5 Pro" {
			t.Errorf("models[0] = %+v, want ID: gemini-1.5-pro, Name: Gemini 1.5 Pro", models[0])
		}
	})

	t.Run("missing key", func(t *testing.T) {
		p := providers.NewGeminiProvider()
		_, err := p.ListModels(ctx, providers.Credential{Type: providers.TypeGemini})
		if err == nil {
			t.Fatalf("expected error on missing key")
		}
	})
}

func TestOpenRouterProvider_ListModels(t *testing.T) {
	ctx := context.Background()

	t.Run("success parsing /api/v1/models", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/models" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "openai/gpt-4o", "name": "OpenAI: GPT-4o"}]}`))
		}))
		defer server.Close()

		p := providers.NewOpenRouterProvider(server.Client())
		models, err := p.ListModels(ctx, providers.Credential{
			Type:    providers.TypeOpenRouter,
			BaseURL: server.URL + "/api/v1",
			APIKey:  "sk-or",
		})
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("models len = %d, want 1", len(models))
		}
		if models[0].ID != "openai/gpt-4o" || models[0].Name != "OpenAI: GPT-4o" {
			t.Errorf("models[0] = %+v", models[0])
		}
	})
}

func TestSuggestCatalogProvider(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"z.ai", "https://api.z.ai/api/paas/v4", "zai-coding-plan"},
		{"zhipuai", "https://api.zhipuai.cn/api/paas/v4", "zhipuai-coding-plan"},
		{"openrouter", "https://openrouter.ai/api/v1", "openrouter"},
		{"deepseek", "https://api.deepseek.com/v1", "deepseek"},
		{"mistral", "https://api.mistral.ai/v1", "mistral"},
		{"groq", "https://api.groq.com/openai/v1", "groq"},
		{"fireworks", "https://api.fireworks.ai/inference/v1", "fireworks-ai"},
		// Matching keys on the hostname alone: scheme, port, path, and case
		// are all ignored.
		{"host case and port", "http://API.Z.AI:8443/v1", "zai-coding-plan"},
		{"trailing slash", "https://api.deepseek.com/", "deepseek"},
		{"unknown host", "https://llm.corp.example/internal/v1", ""},
		{"empty", "", ""},
		{"not a url", "not a url", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providers.SuggestCatalogProvider(tt.baseURL); got != tt.want {
				t.Errorf("SuggestCatalogProvider(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestIsDecisionType(t *testing.T) {
	tests := []struct {
		providerType string
		want         bool
	}{
		{providers.TypeTypeSafe, true},
		{providers.TypeOpenAI, false},
		{providers.TypeAnthropic, false},
		{providers.TypeGemini, false},
		{providers.TypeOpenRouter, false},
		{providers.TypeOpenAICompatible, false},
		{providers.TypeAnthropicCompatible, false},
		{"", false},
		{"unknown-type", false},
	}

	for _, tt := range tests {
		if got := providers.IsDecisionType(tt.providerType); got != tt.want {
			t.Errorf("IsDecisionType(%q) = %v, want %v", tt.providerType, got, tt.want)
		}
	}
}

func TestTypeSafeProvider_Verify(t *testing.T) {
	ctx := context.Background()

	t.Run("canonical origin constant includes the resource path", func(t *testing.T) {
		p := providers.NewTypeSafeProvider(nil)
		if got := p.CanonicalOrigin(); got != providers.TypesafeDefaultEndpoint {
			t.Errorf("CanonicalOrigin() = %q, want %q", got, providers.TypesafeDefaultEndpoint)
		}
		if got := providers.TypesafeDefaultEndpoint; got != "https://api.typesafe.ai/v1/systemone" {
			t.Errorf("TypesafeDefaultEndpoint = %q, want the canonical systemone origin", got)
		}
	})

	t.Run("success with base_url override, auth header, and noul probe body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Exact path — a dangling "/" here 307-redirects to an http://
			// downgrade on the real endpoint and the redirect chain surfaces
			// as a spurious provider 405.
			if r.URL.Path != "/v1/systemone" {
				t.Errorf("unexpected path: %q, want exactly /v1/systemone", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Errorf("unexpected method: %s, want POST", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer ts-test-key" {
				t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
			}
			// docs.typesafe.ai: questions is a MAP keyed by question id —
			// an array of named objects is a 422 (dict_type violation).
			var body struct {
				State     string `json:"state"`
				Model     string `json:"model"`
				Questions map[string]struct {
					Type         string `json:"type"`
					Instructions string `json:"instructions"`
				} `json:"questions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("probe body is not valid JSON: %v", err)
			}
			if body.State == "" {
				t.Errorf("probe state is empty")
			}
			if body.Model == "" {
				t.Errorf("probe model is empty")
			}
			if len(body.Questions) != 1 {
				t.Fatalf("probe carries %d questions, want 1", len(body.Questions))
			}
			q, ok := body.Questions["needs_memory"]
			if !ok {
				t.Fatalf("probe question keyed %q missing, want needs_memory", "needs_memory")
			}
			if q.Type != "noul" {
				t.Errorf("probe question type = %q, want noul", q.Type)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"answers": {"needs_memory": {"noul": 0.83}}, "usage": {"input": 3, "output": 1}}`))
		}))
		defer server.Close()

		p := providers.NewTypeSafeProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeTypeSafe,
			BaseURL: server.URL + "/v1/systemone",
			APIKey:  "ts-test-key",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("success with a bare-number answer value", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"answers": {"needs_memory": 0.9}}`))
		}))
		defer server.Close()

		p := providers.NewTypeSafeProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeTypeSafe,
			BaseURL: server.URL + "/v1/systemone",
			APIKey:  "ts-test-key",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("a trailing-slash base_url override is probed at the exact endpoint path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The stored base_url IS the full endpoint — the probe must hit
			// it exactly, never with an appended "/" (the real endpoint
			// 307s that to an http:// downgrade and the redirect chain
			// surfaces as a spurious provider 405).
			if r.URL.Path != "/v1/systemone" {
				t.Errorf("unexpected path: %q, want exactly /v1/systemone", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"answers": {"needs_memory": {"noul": 0.5}}}`))
		}))
		defer server.Close()

		p := providers.NewTypeSafeProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeTypeSafe,
			BaseURL: server.URL + "/v1/systemone/",
			APIKey:  "ts-test-key",
		})
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
	})

	t.Run("200 without an answers body fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"usage": {"input": 3, "output": 1}}`))
		}))
		defer server.Close()

		p := providers.NewTypeSafeProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeTypeSafe,
			BaseURL: server.URL + "/v1/systemone",
			APIKey:  "ts-test-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on a 200 without answers, got nil")
		}
		if expected := "typesafe verify response does not contain an answers body"; !strings.Contains(err.Error(), expected) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), expected)
		}
	})

	t.Run("200 with a non-JSON body fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json at all`))
		}))
		defer server.Close()

		p := providers.NewTypeSafeProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeTypeSafe,
			BaseURL: server.URL + "/v1/systemone",
			APIKey:  "ts-test-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on a non-JSON 200, got nil")
		}
	})

	t.Run("auth error response parsed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": {"message": "Invalid API key"}}`))
		}))
		defer server.Close()

		p := providers.NewTypeSafeProvider(server.Client())
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeTypeSafe,
			BaseURL: server.URL + "/v1/systemone",
			APIKey:  "ts-bad-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on 401, got nil")
		}
		if expected := "provider error (401): Invalid API key"; err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})

	t.Run("missing API key fails fast", func(t *testing.T) {
		p := providers.NewTypeSafeProvider(nil)
		err := p.Verify(ctx, providers.Credential{
			Type:   providers.TypeTypeSafe,
			APIKey: "",
		})
		if err == nil {
			t.Fatalf("Verify expected error on empty key, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("error = %v, want ErrInvalid sentinel wrapped", err)
		}
	})

	t.Run("invalid base_url scheme", func(t *testing.T) {
		p := providers.NewTypeSafeProvider(nil)
		err := p.Verify(ctx, providers.Credential{
			Type:    providers.TypeTypeSafe,
			BaseURL: "ftp://typesafe.example.com/v1/systemone",
			APIKey:  "ts-test-key",
		})
		if err == nil {
			t.Fatalf("Verify expected error on ftp scheme, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("error = %v, want ErrInvalid sentinel wrapped", err)
		}
	})
}

func TestTypeSafeProvider_ListModels(t *testing.T) {
	ctx := context.Background()

	p := providers.NewTypeSafeProvider(nil)
	models, err := p.ListModels(ctx, providers.Credential{Type: providers.TypeTypeSafe})
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("ListModels = %+v, want no models (decision providers never surface model choices)", models)
	}
}
