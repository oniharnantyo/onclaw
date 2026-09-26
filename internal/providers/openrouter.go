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
	openRouterCanonicalOrigin = "https://openrouter.ai/api/v1"
	openRouterProbePath       = "/key"
	openRouterModelsPath      = "/models"
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

func (p *OpenRouterProvider) RequiresAPIKey() bool {
	return true
}

func (p *OpenRouterProvider) CanonicalOrigin() string {
	return openRouterCanonicalOrigin
}

func (p *OpenRouterProvider) RequiresMaxTokens() bool {
	return false
}

func (p *OpenRouterProvider) ValidEfforts() []string {
	return []string{}
}

func (p *OpenRouterProvider) SupportsTemperature() bool {
	return true
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

func (p *OpenRouterProvider) ListModels(ctx context.Context, cred Credential) ([]domain.Model, error) {
	probeURL, err := buildProbeURL(cred.BaseURL, openRouterCanonicalOrigin, openRouterModelsPath, false)
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

	return parseOpenRouterModels(body, p.ValidEfforts(), p.SupportsTemperature())
}

func parseOpenRouterModels(body []byte, efforts []string, supportsTemp bool) ([]domain.Model, error) {
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

	// Defensive fallback for raw list
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
