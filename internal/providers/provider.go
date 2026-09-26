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
	// CatalogHint carries the user-selected community-catalog provider id for
	// compatible gateway types (openai-compatible/anthropic-compatible); the
	// canonical provider types map on their own and ignore it.
	CatalogHint string `json:"catalog_provider,omitempty"`
}

// Provider defines the capability interface for AI providers.
type Provider interface {
	Type() string
	RequiresBaseURL() bool
	// RequiresAPIKey reports whether the provider type requires an API key.
	// Keyless-capable types (the -compatible gateways) omit the auth header
	// when no key is configured, so they return false.
	RequiresAPIKey() bool
	CanonicalOrigin() string
	RequiresMaxTokens() bool
	ValidEfforts() []string
	SupportsTemperature() bool
	Verify(ctx context.Context, cred Credential) error
	ListModels(ctx context.Context, cred Credential) ([]domain.Model, error)
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

// StripVersionPath removes one trailing version path segment ("/v1", "/api/v1",
// "/v1beta") from a base_url. base_url stores the full API base including the
// version path; SDKs that append their own version path (Anthropic, Gemini)
// receive the base without it.
func StripVersionPath(base string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return trimmed
	}
	last := trimmed[idx+1:]
	if len(last) < 2 || last[0] != 'v' {
		return trimmed
	}
	rest := last[1:]
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return trimmed
	}
	if i < len(rest) {
		switch rest[i:] {
		case "beta", "alpha", "beta1", "alpha1":
		default:
			return trimmed
		}
	}
	return trimmed[:idx]
}

// OpenRouterDefaultEndpoint is the base URL used when an OpenRouter provider
// has no explicit base_url configured.
const OpenRouterDefaultEndpoint = "https://openrouter.ai/api/v1"

// catalogHostHints maps known compatible-gateway hosts to the community
// catalog provider id their API matches. Only hosts verified present in the
// models.dev catalog are listed — a stale id would resolve unknown anyway,
// but dropping it keeps the suggestion list honest. The user's explicit
// catalog_provider selection always wins over these suggestions.
var catalogHostHints = map[string]string{
	"api.z.ai":         "zai-coding-plan",
	"api.zhipuai.cn":   "zhipuai-coding-plan",
	"openrouter.ai":    "openrouter",
	"api.deepseek.com": "deepseek",
	"api.mistral.ai":   "mistral",
	"api.groq.com":     "groq",
	"api.fireworks.ai": "fireworks-ai",
}

// SuggestCatalogProvider returns the community-catalog provider id matching
// the host of baseURL, or empty when the host is unknown. Matching ignores
// scheme, port, path, and case: it keys on the hostname alone.
func SuggestCatalogProvider(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return catalogHostHints[strings.ToLower(u.Hostname())]
}
