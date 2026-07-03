package exa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/web"
)

func init() {
	web.RegisterSearcher("exa", func(cfg web.Config, resolver secrets.SecretResolver) (web.Searcher, error) {
		return &exaSearcher{cfg: cfg, resolver: resolver}, nil
	})
}

type exaSearcher struct {
	cfg      web.Config
	resolver secrets.SecretResolver
	client   *http.Client
}

type searchRequest struct {
	Query      string `json:"query"`
	NumResults int    `json:"numResults,omitempty"`
}

type exaResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Text  string `json:"text"`
}

type searchResponse struct {
	Results []exaResult `json:"results"`
}

func (s *exaSearcher) Search(ctx context.Context, query string, limit int) ([]web.SearchResult, error) {
	if s.resolver == nil {
		return nil, errors.New("secret resolver is not set")
	}

	apiKey, err := s.resolver.Resolve(ctx, "ONCLAW_WEB_EXA_API_KEY", "web.exa")
	if err != nil {
		return nil, fmt.Errorf("resolve Exa API key: %w", err)
	}
	if apiKey == "" {
		return nil, errors.New("Exa API key is empty")
	}

	reqBody := searchRequest{
		Query: query,
	}
	if limit > 0 {
		reqBody.NumResults = limit
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.exa.ai/search", bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	client := s.client
	if client == nil {
		timeout := time.Duration(s.cfg.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Exa search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Exa returned status %d: %s", resp.StatusCode, string(body))
	}

	limitReader := io.LimitReader(resp.Body, s.cfg.MaxBytes)
	respBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var searchResp searchResponse
	if err := json.Unmarshal(respBytes, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse Exa response: %w", err)
	}

	var results []web.SearchResult
	for _, r := range searchResp.Results {
		results = append(results, web.SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Text,
		})
	}

	return results, nil
}
