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
	openAICanonicalOrigin = "https://api.openai.com/v1"
	openAIProbePath       = "/models"
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

func (p *OpenAIProvider) RequiresAPIKey() bool {
	return true
}

func (p *OpenAIProvider) CanonicalOrigin() string {
	return openAICanonicalOrigin
}

func (p *OpenAIProvider) RequiresMaxTokens() bool {
	return false
}

func (p *OpenAIProvider) ValidEfforts() []string {
	return []string{"low", "medium", "high"}
}

func (p *OpenAIProvider) SupportsTemperature() bool {
	return true
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

func (p *OpenAIProvider) ListModels(ctx context.Context, cred Credential) ([]domain.Model, error) {
	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%w: API key is required for openai provider", domain.ErrInvalid)
	}

	probeURL, err := buildProbeURL(cred.BaseURL, openAICanonicalOrigin, openAIProbePath, false)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
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

	return parseOpenAIModels(body, p.ValidEfforts(), p.SupportsTemperature())
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

func (p *OpenAICompatibleProvider) RequiresAPIKey() bool {
	return false
}

func (p *OpenAICompatibleProvider) CanonicalOrigin() string {
	return ""
}

func (p *OpenAICompatibleProvider) RequiresMaxTokens() bool {
	return false
}

func (p *OpenAICompatibleProvider) ValidEfforts() []string {
	return []string{"low", "medium", "high"}
}

func (p *OpenAICompatibleProvider) SupportsTemperature() bool {
	return true
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

func (p *OpenAICompatibleProvider) ListModels(ctx context.Context, cred Credential) ([]domain.Model, error) {
	probeURL, err := buildProbeURL(cred.BaseURL, "", openAIProbePath, true)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
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

	return parseOpenAIModels(body, p.ValidEfforts(), p.SupportsTemperature())
}

func parseOpenAIModels(body []byte, efforts []string, supportsTemp bool) ([]domain.Model, error) {
	var respBody struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &respBody); err == nil && len(respBody.Data) > 0 {
		models := make([]domain.Model, 0, len(respBody.Data))
		for _, item := range respBody.Data {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(item.Name)
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

	// Defensive fallback for direct array: [{"id": "gpt-4o", ...}]
	var rawList []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &rawList); err == nil && len(rawList) > 0 {
		models := make([]domain.Model, 0, len(rawList))
		for _, item := range rawList {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(item.Name)
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
