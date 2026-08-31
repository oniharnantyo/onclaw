package providers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

const (
	anthropicCanonicalOrigin = "https://api.anthropic.com"
	anthropicProbePath       = "/v1/models"
	anthropicVersionHeader   = "2023-06-01"
)

// AnthropicProvider implements connection verification for Anthropic.
type AnthropicProvider struct {
	client *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider instance.
func NewAnthropicProvider(client ...*http.Client) *AnthropicProvider {
	var c *http.Client
	if len(client) > 0 {
		c = client[0]
	}
	return &AnthropicProvider{client: httpClient(c)}
}

func (p *AnthropicProvider) Type() string {
	return TypeAnthropic
}

func (p *AnthropicProvider) RequiresBaseURL() bool {
	return false
}

func (p *AnthropicProvider) CanonicalOrigin() string {
	return anthropicCanonicalOrigin
}

func (p *AnthropicProvider) Verify(ctx context.Context, cred Credential) error {
	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey == "" {
		return fmt.Errorf("%w: API key is required for anthropic provider", domain.ErrInvalid)
	}

	probeURL, err := buildProbeURL(cred.BaseURL, anthropicCanonicalOrigin, anthropicProbePath, false)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersionHeader)
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

// AnthropicCompatibleProvider implements connection verification for generic Anthropic-compatible APIs.
type AnthropicCompatibleProvider struct {
	client *http.Client
}

// NewAnthropicCompatibleProvider creates a new Anthropic-compatible provider instance.
func NewAnthropicCompatibleProvider(client ...*http.Client) *AnthropicCompatibleProvider {
	var c *http.Client
	if len(client) > 0 {
		c = client[0]
	}
	return &AnthropicCompatibleProvider{client: httpClient(c)}
}

func (p *AnthropicCompatibleProvider) Type() string {
	return TypeAnthropicCompatible
}

func (p *AnthropicCompatibleProvider) RequiresBaseURL() bool {
	return true
}

func (p *AnthropicCompatibleProvider) CanonicalOrigin() string {
	return ""
}

func (p *AnthropicCompatibleProvider) Verify(ctx context.Context, cred Credential) error {
	probeURL, err := buildProbeURL(cred.BaseURL, "", anthropicProbePath, true)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	req.Header.Set("anthropic-version", anthropicVersionHeader)
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
