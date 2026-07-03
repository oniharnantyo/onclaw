package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/web"
)

func init() {
	web.RegisterFetcher("http", func(cfg web.Config, resolver secrets.SecretResolver) (web.Fetcher, error) {
		return &httpFetcher{cfg: cfg}, nil
	})
}

type httpFetcher struct {
	cfg web.Config
}

func (f *httpFetcher) Fetch(ctx context.Context, urlStr string, headers map[string]string) (web.FetchResult, error) {
	// 1. SSRF check before requesting
	if err := web.ValidateURLNotInternal(urlStr); err != nil {
		return web.FetchResult{}, fmt.Errorf("SSRF protection blocked URL: %w", err)
	}

	// 2. Build Request
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return web.FetchResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Add default/config User-Agent if not explicitly set
	if req.Header.Get("User-Agent") == "" {
		ua := f.cfg.UserAgent
		if ua == "" {
			ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
		}
		req.Header.Set("User-Agent", ua)
	}

	// 3. Client setup with Redirect SSRF Guard
	timeout := time.Duration(f.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			// SSRF check on redirect target
			if err := web.ValidateURLNotInternal(r.URL.String()); err != nil {
				return fmt.Errorf("SSRF validation failed on redirect: %w", err)
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return web.FetchResult{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return web.FetchResult{}, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	// 4. Limit response size
	limitReader := io.LimitReader(resp.Body, f.cfg.MaxBytes)
	bodyBytes, err := io.ReadAll(limitReader)
	if err != nil {
		return web.FetchResult{}, fmt.Errorf("failed to read body: %w", err)
	}

	// 5. Convert HTML to text content
	content := stripHTML(string(bodyBytes))
	return web.FetchResult{Content: content}, nil
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

func stripHTML(html string) string {
	// Strip style blocks
	html = stripBlock(html, "<style", "</style>")
	// Strip script blocks
	html = stripBlock(html, "<script", "</script>")
	return stripTags(html)
}

func stripBlock(html, startTag, endTag string) string {
	for {
		startIdx := strings.Index(strings.ToLower(html), startTag)
		if startIdx == -1 {
			break
		}
		endIdx := strings.Index(strings.ToLower(html[startIdx:]), endTag)
		if endIdx == -1 {
			break
		}
		html = html[:startIdx] + html[startIdx+endIdx+len(endTag):]
	}
	return html
}
