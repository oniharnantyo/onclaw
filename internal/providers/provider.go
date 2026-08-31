// Package providers defines provider capabilities, the provider registry, and connection verifiers for AI providers.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Standard catalog provider type constants.
const (
	TypeOpenAI              = "openai"
	TypeAnthropic           = "anthropic"
	TypeGemini              = "gemini"
	TypeOpenRouter          = "openrouter"
	TypeOpenAICompatible    = "openai-compatible"
	TypeAnthropicCompatible = "anthropic-compatible"
)

// KnownTypes contains the slice of all six standard catalog provider types.
var KnownTypes = []string{
	TypeOpenAI,
	TypeAnthropic,
	TypeGemini,
	TypeOpenRouter,
	TypeOpenAICompatible,
	TypeAnthropicCompatible,
}

// DefaultTimeout is the default HTTP client timeout for provider probes.
const DefaultTimeout = 15 * time.Second

// Credential contains the configuration and credentials needed to verify or connect to a provider.
type Credential struct {
	Type    string `json:"type"`
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

// Provider defines the capability interface for AI providers.
type Provider interface {
	Type() string
	RequiresBaseURL() bool
	CanonicalOrigin() string
	Verify(ctx context.Context, cred Credential) error
}

// Registry manages registered provider implementations.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates a new registry initialized with the six built-in providers using default HTTP clients.
func NewRegistry() *Registry {
	return NewRegistryWithClient(nil)
}

// NewRegistryWithClient creates a new registry initialized with the six built-in providers using the specified HTTP client.
func NewRegistryWithClient(client *http.Client) *Registry {
	r := &Registry{
		providers: make(map[string]Provider),
	}
	r.Register(NewOpenAIProvider(client))
	r.Register(NewAnthropicProvider(client))
	r.Register(NewGeminiProvider(client))
	r.Register(NewOpenRouterProvider(client))
	r.Register(NewOpenAICompatibleProvider(client))
	r.Register(NewAnthropicCompatibleProvider(client))
	return r
}

// Register registers a provider. It panics if p is nil, has an empty type, or if a provider
// with the same type is already registered.
func (r *Registry) Register(p Provider) {
	if p == nil {
		panic("cannot register nil provider")
	}
	t := p.Type()
	if t == "" {
		panic("cannot register provider with empty type")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[t]; exists {
		panic(fmt.Sprintf("provider %q already registered", t))
	}
	r.providers[t] = p
}

// Get retrieves a registered provider by type. Returns domain.ErrInvalid if the provider type is unknown.
func (r *Registry) Get(providerType string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.providers[providerType]
	if !exists {
		return nil, fmt.Errorf("%w: unknown provider type %q (valid types: %s)", domain.ErrInvalid, providerType, strings.Join(KnownTypes, ", "))
	}
	return p, nil
}

// List returns a sorted list of registered provider type names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsValidType reports whether the specified provider type is registered.
func (r *Registry) IsValidType(providerType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.providers[providerType]
	return exists
}

// httpClient returns the provided client or a default client with timeout.
func httpClient(custom *http.Client) *http.Client {
	if custom != nil {
		return custom
	}
	return &http.Client{
		Timeout: DefaultTimeout,
	}
}

// buildProbeURL constructs the probe URL from baseURL, canonicalOrigin, and probePath.
// It trims any trailing slashes from the origin/base and ensures clean path concatenation.
func buildProbeURL(baseURL, canonicalOrigin, probePath string, requiresBaseURL bool) (string, error) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		if requiresBaseURL {
			return "", fmt.Errorf("%w: base_url is required for this provider type", domain.ErrInvalid)
		}
		raw = canonicalOrigin
	}
	if raw == "" {
		return "", fmt.Errorf("%w: no base URL or canonical origin specified", domain.ErrInvalid)
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%w: invalid base_url %q", domain.ErrInvalid, raw)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: base_url scheme must be http or https", domain.ErrInvalid)
	}

	trimmedBase := strings.TrimRight(raw, "/")
	cleanPath := probePath
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	return trimmedBase + cleanPath, nil
}

// parseProviderError reads and formats an error message from a non-2xx HTTP response.
func parseProviderError(resp *http.Response) error {
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return fmt.Errorf("provider returned HTTP %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	bodyStr := strings.TrimSpace(string(bodyBytes))
	if bodyStr == "" {
		return fmt.Errorf("provider returned HTTP %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	// Try unmarshaling standard JSON error structures
	var parsed map[string]any
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		// 1. OpenAI / OpenRouter / Gemini / Anthropic nested error: {"error": {"message": "..."}}
		if errVal, exists := parsed["error"]; exists {
			if errMap, ok := errVal.(map[string]any); ok {
				if msg, ok := errMap["message"].(string); ok && strings.TrimSpace(msg) != "" {
					return fmt.Errorf("provider error (%d): %s", resp.StatusCode, strings.TrimSpace(msg))
				}
			} else if errStr, ok := errVal.(string); ok && strings.TrimSpace(errStr) != "" {
				return fmt.Errorf("provider error (%d): %s", resp.StatusCode, strings.TrimSpace(errStr))
			}
		}

		// 2. Direct message: {"message": "..."}
		if msg, ok := parsed["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return fmt.Errorf("provider error (%d): %s", resp.StatusCode, strings.TrimSpace(msg))
		}

		// 3. Detail: {"detail": "..."}
		if detail, ok := parsed["detail"].(string); ok && strings.TrimSpace(detail) != "" {
			return fmt.Errorf("provider error (%d): %s", resp.StatusCode, strings.TrimSpace(detail))
		}
	}

	// Truncate raw response text if too long
	if len(bodyStr) > 200 {
		bodyStr = bodyStr[:200] + "..."
	}
	return fmt.Errorf("provider error (%d): %s", resp.StatusCode, bodyStr)
}
