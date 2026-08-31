package providers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

const (
	geminiCanonicalOrigin = "https://generativelanguage.googleapis.com"
	geminiProbePath       = "/v1beta/models"
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

func (p *GeminiProvider) CanonicalOrigin() string {
	return geminiCanonicalOrigin
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
