package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
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
			BaseURL: server.URL,
		})
		if err == nil {
			t.Fatalf("Verify expected error on 404, got nil")
		}
		if expected := "provider error (404): Not Found: custom proxy endpoint"; err.Error() != expected {
			t.Errorf("error = %q, want %q", err.Error(), expected)
		}
	})
}
