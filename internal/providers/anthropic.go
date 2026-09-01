package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

const (
	anthropicCanonicalOrigin = "https://api.anthropic.com/v1"
	anthropicProbePath       = "/models"
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

func (p *AnthropicProvider) RequiresMaxTokens() bool {
	return true
}

func (p *AnthropicProvider) ValidEfforts() []string {
	return []string{}
}

func (p *AnthropicProvider) SupportsTemperature() bool {
	return true
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

func (p *AnthropicProvider) ListModels(ctx context.Context, cred Credential) ([]domain.Model, error) {
	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%w: API key is required for anthropic provider", domain.ErrInvalid)
	}

	probeURL, err := buildProbeURL(cred.BaseURL, anthropicCanonicalOrigin, anthropicProbePath, false)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersionHeader)
	req.Header.Set("User-Agent", "onclaw")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseProviderError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return parseAnthropicModels(body, p.ValidEfforts(), p.SupportsTemperature())
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

func (p *AnthropicCompatibleProvider) RequiresMaxTokens() bool {
	return true
}

func (p *AnthropicCompatibleProvider) ValidEfforts() []string {
	return []string{}
}

func (p *AnthropicCompatibleProvider) SupportsTemperature() bool {
	return true
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

func (p *AnthropicCompatibleProvider) ListModels(ctx context.Context, cred Credential) ([]domain.Model, error) {
	probeURL, err := buildProbeURL(cred.BaseURL, "", anthropicProbePath, true)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	req.Header.Set("anthropic-version", anthropicVersionHeader)
	req.Header.Set("User-Agent", "onclaw")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseProviderError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return parseAnthropicModels(body, p.ValidEfforts(), p.SupportsTemperature())
}

func parseAnthropicModels(body []byte, efforts []string, supportsTemp bool) ([]domain.Model, error) {
	var respBody struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &respBody); err == nil && len(respBody.Data) > 0 {
		models := make([]domain.Model, 0, len(respBody.Data))
		for _, item := range respBody.Data {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(item.DisplayName)
			if name == "" {
				name = strings.TrimSpace(item.Name)
			}
			if name == "" {
				name = id
			}
			models = append(models, domain.Model{
				ID:                  id,
				Name:                name,
				Efforts:             efforts,
				SupportsTemperature: supportsTemp,
			})
		}
		return models, nil
	}

	// Defensive fallback for direct array: [{"id": "...", "display_name": "..."}]
	var rawList []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Name        string `json:"name"`
	}
	if err := json.Unmarshal(body, &rawList); err == nil && len(rawList) > 0 {
		models := make([]domain.Model, 0, len(rawList))
		for _, item := range rawList {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(item.DisplayName)
			if name == "" {
				name = strings.TrimSpace(item.Name)
			}
			if name == "" {
				name = id
			}
			models = append(models, domain.Model{
				ID:                  id,
				Name:                name,
				Efforts:             efforts,
				SupportsTemperature: supportsTemp,
			})
		}
		return models, nil
	}

	return []domain.Model{}, nil
}
