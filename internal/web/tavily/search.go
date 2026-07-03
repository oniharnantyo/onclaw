package tavily

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
	web.RegisterSearcher("tavily", func(cfg web.Config, resolver secrets.SecretResolver) (web.Searcher, error) {
		return &tavilySearcher{cfg: cfg, resolver: resolver}, nil
	})
}

type tavilySearcher struct {
	cfg      web.Config
	resolver secrets.SecretResolver
	client   *http.Client
}

type searchRequest struct {
	Query      string `json:"query"`
	APIKey     string `json:"api_key"`
	MaxResults int    `json:"max_results,omitempty"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

type searchResponse struct {
	Results []tavilyResult `json:"results"`
}

func (s *tavilySearcher) Search(ctx context.Context, query string, limit int) ([]web.SearchResult, error) {
	if s.resolver == nil {
		return nil, errors.New("secret resolver is not set")
	}

	apiKey, err := s.resolver.Resolve(ctx, "ONCLAW_WEB_TAVILY_API_KEY", "web.tavily")
	if err != nil {
		return nil, fmt.Errorf("resolve Tavily API key: %w", err)
	}
	if apiKey == "" {
		return nil, errors.New("Tavily API key is empty")
	}

	reqBody := searchRequest{
		Query:  query,
		APIKey: apiKey,
	}
	if limit > 0 {
		reqBody.MaxResults = limit
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
		return nil, fmt.Errorf("Tavily search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Tavily returned status %d: %s", resp.StatusCode, string(body))
	}

	limitReader := io.LimitReader(resp.Body, s.cfg.MaxBytes)
	respBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var searchResp searchResponse
	if err := json.Unmarshal(respBytes, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse Tavily response: %w", err)
	}

	var results []web.SearchResult
	for _, r := range searchResp.Results {
		results = append(results, web.SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
		})
	}

	return results, nil
}
