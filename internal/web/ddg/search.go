package ddg

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/web"
)

func init() {
	web.RegisterSearcher("duckduckgo", func(cfg web.Config, resolver secrets.SecretResolver) (web.Searcher, error) {
		return &ddgSearcher{cfg: cfg}, nil
	})
}

type ddgSearcher struct {
	cfg web.Config
}

func (s *ddgSearcher) Search(ctx context.Context, query string, limit int) ([]web.SearchResult, error) {
	// Build request
	reqURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	userAgent := s.cfg.UserAgent
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch duckduckgo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo returned status %d", resp.StatusCode)
	}

	// Limit reading response to prevent excessive memory usage
	limitReader := io.LimitReader(resp.Body, s.cfg.MaxBytes)
	bodyBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	results := parseDDGResults(string(bodyBytes))
	if len(results) > limit && limit > 0 {
		results = results[:limit]
	}
	return results, nil
}

func cleanDDGURL(rawURL string) string {
	if strings.Contains(rawURL, "uddg=") {
		u, err := url.Parse(rawURL)
		if err == nil {
			uddg := u.Query().Get("uddg")
			if uddg != "" {
				return uddg
			}
		}
	}
	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	} else if strings.HasPrefix(rawURL, "/") {
		rawURL = "https://duckduckgo.com" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err == nil {
		uddg := u.Query().Get("uddg")
		if uddg != "" {
			return uddg
		}
	}
	return rawURL
}

func stripTags(s string) string {
	var builder strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			builder.WriteRune(r)
		}
	}
	res := builder.String()
	res = strings.ReplaceAll(res, "&amp;", "&")
	res = strings.ReplaceAll(res, "&lt;", "<")
	res = strings.ReplaceAll(res, "&gt;", ">")
	res = strings.ReplaceAll(res, "&quot;", "\"")
	res = strings.ReplaceAll(res, "&#x27;", "'")
	res = strings.ReplaceAll(res, "&#39;", "'")
	res = strings.ReplaceAll(res, "&nbsp;", " ")
	return strings.TrimSpace(res)
}

func parseDDGResults(html string) []web.SearchResult {
	var results []web.SearchResult
	parts := strings.Split(html, "result__body")
	if len(parts) <= 1 {
		parts = strings.Split(html, "web-result")
	}
	if len(parts) <= 1 {
		return results
	}

	for _, part := range parts[1:] {
		// Extract URL
		hrefIdx := strings.Index(part, "href=\"")
		if hrefIdx == -1 {
			continue
		}
		partAfterHref := part[hrefIdx+6:]
		endQuoteIdx := strings.Index(partAfterHref, "\"")
		if endQuoteIdx == -1 {
			continue
		}
		rawURL := partAfterHref[:endQuoteIdx]

		// Extract Title
		resultAIdx := strings.Index(part, "result__a")
		if resultAIdx == -1 {
			continue
		}
		partAfterA := part[resultAIdx:]
		openTagEnd := strings.Index(partAfterA, ">")
		if openTagEnd == -1 {
			continue
		}
		closeTag := strings.Index(partAfterA, "</a>")
		if closeTag == -1 {
			continue
		}
		rawTitle := partAfterA[openTagEnd+1 : closeTag]
		title := stripTags(rawTitle)

		// Extract Snippet
		snippetIdx := strings.Index(part, "result__snippet")
		var snippet string
		if snippetIdx != -1 {
			partAfterSnippet := part[snippetIdx:]
			openSnippetEnd := strings.Index(partAfterSnippet, ">")
			if openSnippetEnd != -1 {
				closeSnippet := strings.Index(partAfterSnippet[openSnippetEnd+1:], "</")
				if closeSnippet != -1 {
					rawSnippet := partAfterSnippet[openSnippetEnd+1 : openSnippetEnd+1+closeSnippet]
					snippet = stripTags(rawSnippet)
				}
			}
		}

		cleanedURL := cleanDDGURL(rawURL)
		results = append(results, web.SearchResult{
			Title:   title,
			URL:     cleanedURL,
			Snippet: snippet,
		})
	}
	return results
}
