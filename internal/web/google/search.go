package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/web"
)

func init() {
	web.RegisterSearcher("google", func(cfg web.Config, resolver secrets.SecretResolver) (web.Searcher, error) {
		return &googleSearcher{cfg: cfg, resolver: resolver}, nil
	})
}

type googleSearcher struct {
	cfg      web.Config
	resolver secrets.SecretResolver
	client   *http.Client
}

type googleItem struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

type googleResponse struct {
	Items []googleItem `json:"items"`
}

func (s *googleSearcher) Search(ctx context.Context, query string, limit int) ([]web.SearchResult, error) {
	if s.resolver == nil {
		return nil, errors.New("secret resolver is not set")
	}

	apiKey, err := s.resolver.Resolve(ctx, "ONCLAW_WEB_GOOGLE_API_KEY", "web.google")
	if err != nil {
		return nil, fmt.Errorf("resolve Google API key: %w", err)
	}
	if apiKey == "" {
		return nil, errors.New("Google API key is empty")
	}

	cx := s.cfg.GoogleCX
	if cx == "" {
		return nil, errors.New("Google Custom Search Engine ID (google_cx) is not configured")
	}

	// Build URL
	baseURL := "https://customsearch.googleapis.com/customsearch/v1"
	params := url.Values{}
	params.Set("q", query)
	params.Set("key", apiKey)
	params.Set("cx", cx)
	if limit > 0 {
		if limit > 10 {
			limit = 10 // Google Custom Search max limit per request is 10
		}
		params.Set("num", strconv.Itoa(limit))
	}

	reqURL := baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

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
		return nil, fmt.Errorf("Google search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Google returned status %d: %s", resp.StatusCode, string(body))
	}

	limitReader := io.LimitReader(resp.Body, s.cfg.MaxBytes)
	respBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var searchResp googleResponse
	if err := json.Unmarshal(respBytes, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse Google response: %w", err)
	}

	var results []web.SearchResult
	for _, r := range searchResp.Items {
		results = append(results, web.SearchResult{
			Title:   r.Title,
			URL:     r.Link,
			Snippet: r.Snippet,
		})
	}

	return results, nil
}
