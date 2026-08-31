package providers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

const (
	openRouterCanonicalOrigin = "https://openrouter.ai"
	openRouterProbePath       = "/api/v1/key"
)

// OpenRouterProvider implements connection verification for OpenRouter.
// OpenRouter probes the key-info endpoint (/api/v1/key) because its models endpoint is public.
type OpenRouterProvider struct {
	client *http.Client
}

// NewOpenRouterProvider creates a new OpenRouter provider instance.
func NewOpenRouterProvider(client ...*http.Client) *OpenRouterProvider {
	var c *http.Client
	if len(client) > 0 {
		c = client[0]
	}
	return &OpenRouterProvider{client: httpClient(c)}
}

func (p *OpenRouterProvider) Type() string {
	return TypeOpenRouter
}

func (p *OpenRouterProvider) RequiresBaseURL() bool {
	return false
}

func (p *OpenRouterProvider) CanonicalOrigin() string {
	return openRouterCanonicalOrigin
}

func (p *OpenRouterProvider) Verify(ctx context.Context, cred Credential) error {
	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey == "" {
		return fmt.Errorf("%w: API key is required for openrouter provider", domain.ErrInvalid)
	}

	probeURL, err := buildProbeURL(cred.BaseURL, openRouterCanonicalOrigin, openRouterProbePath, false)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "onclaw")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseProviderError(resp)
	}
	resp.Body.Close()

	return nil
}
