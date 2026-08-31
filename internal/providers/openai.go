package providers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

const (
	openAICanonicalOrigin = "https://api.openai.com"
	openAIProbePath       = "/v1/models"
)

// OpenAIProvider implements connection verification for OpenAI.
type OpenAIProvider struct {
	client *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider instance.
func NewOpenAIProvider(client ...*http.Client) *OpenAIProvider {
	var c *http.Client
	if len(client) > 0 {
		c = client[0]
	}
	return &OpenAIProvider{client: httpClient(c)}
}

func (p *OpenAIProvider) Type() string {
	return TypeOpenAI
}

func (p *OpenAIProvider) RequiresBaseURL() bool {
	return false
}

func (p *OpenAIProvider) CanonicalOrigin() string {
	return openAICanonicalOrigin
}

func (p *OpenAIProvider) Verify(ctx context.Context, cred Credential) error {
	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey == "" {
		return fmt.Errorf("%w: API key is required for openai provider", domain.ErrInvalid)
	}

	probeURL, err := buildProbeURL(cred.BaseURL, openAICanonicalOrigin, openAIProbePath, false)
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

// OpenAICompatibleProvider implements connection verification for generic OpenAI-compatible APIs.
type OpenAICompatibleProvider struct {
	client *http.Client
}

// NewOpenAICompatibleProvider creates a new OpenAI-compatible provider instance.
func NewOpenAICompatibleProvider(client ...*http.Client) *OpenAICompatibleProvider {
	var c *http.Client
	if len(client) > 0 {
		c = client[0]
	}
	return &OpenAICompatibleProvider{client: httpClient(c)}
}

func (p *OpenAICompatibleProvider) Type() string {
	return TypeOpenAICompatible
}

func (p *OpenAICompatibleProvider) RequiresBaseURL() bool {
	return true
}

func (p *OpenAICompatibleProvider) CanonicalOrigin() string {
	return ""
}

func (p *OpenAICompatibleProvider) Verify(ctx context.Context, cred Credential) error {
	probeURL, err := buildProbeURL(cred.BaseURL, "", openAIProbePath, true)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
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
