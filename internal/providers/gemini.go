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
	geminiCanonicalOrigin = "https://generativelanguage.googleapis.com/v1beta"
	geminiProbePath       = "/models"
)

// GeminiProvider implements connection verification for Google Gemini.
type GeminiProvider struct {
	client *http.Client
}

// NewGeminiProvider creates a new Gemini provider instance.
func NewGeminiProvider(client ...*http.Client) *GeminiProvider {
	var c *http.Client
	if len(client) > 0 {
		c = client[0]
	}
	return &GeminiProvider{client: httpClient(c)}
}

func (p *GeminiProvider) Type() string {
	return TypeGemini
}

func (p *GeminiProvider) RequiresBaseURL() bool {
	return false
}

func (p *GeminiProvider) RequiresAPIKey() bool {
	return true
}

func (p *GeminiProvider) CanonicalOrigin() string {
	return geminiCanonicalOrigin
}

func (p *GeminiProvider) RequiresMaxTokens() bool {
	return false
}

func (p *GeminiProvider) ValidEfforts() []string {
	return []string{}
}

func (p *GeminiProvider) SupportsTemperature() bool {
	return true
}

func (p *GeminiProvider) Verify(ctx context.Context, cred Credential) error {
	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey == "" {
		return fmt.Errorf("%w: API key is required for gemini provider", domain.ErrInvalid)
	}

	probeURL, err := buildProbeURL(cred.BaseURL, geminiCanonicalOrigin, geminiProbePath, false)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("x-goog-api-key", apiKey)
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

func (p *GeminiProvider) ListModels(ctx context.Context, cred Credential) ([]domain.Model, error) {
	apiKey := strings.TrimSpace(cred.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%w: API key is required for gemini provider", domain.ErrInvalid)
	}

	probeURL, err := buildProbeURL(cred.BaseURL, geminiCanonicalOrigin, geminiProbePath, false)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("x-goog-api-key", apiKey)
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

	return parseGeminiModels(body, p.ValidEfforts(), p.SupportsTemperature())
}

func parseGeminiModels(body []byte, efforts []string, supportsTemp bool) ([]domain.Model, error) {
	var respBody struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}

	if err := json.Unmarshal(body, &respBody); err == nil && len(respBody.Models) > 0 {
		models := make([]domain.Model, 0, len(respBody.Models))
		for _, m := range respBody.Models {
			id := strings.TrimPrefix(strings.TrimSpace(m.Name), "models/")
			if id == "" {
				continue
			}
			name := strings.TrimSpace(m.DisplayName)
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
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(body, &rawList); err == nil && len(rawList) > 0 {
		models := make([]domain.Model, 0, len(rawList))
		for _, m := range rawList {
			id := strings.TrimPrefix(strings.TrimSpace(m.Name), "models/")
			if id == "" {
				continue
			}
			name := strings.TrimSpace(m.DisplayName)
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
