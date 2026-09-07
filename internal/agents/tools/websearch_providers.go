package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// SearchResult is one organic search hit, provider-independent.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchProvider resolves a query to organic results. Implementations are the
// web.search backend seam: duckduckgo (zero-credential) and tavily (API key).
type SearchProvider interface {
	Search(ctx context.Context, query string, num int) ([]SearchResult, error)
}

// HTTPClient is the seam for the web tools so tests can inject a fake. It
// defaults to http.DefaultClient.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

const defaultUserAgent = "OnClaw/1.0 (agent-runtime; web.search)"

// duckduckgoProvider scrapes DuckDuckGo's HTML endpoint. No credentials.
type duckduckgoProvider struct {
	client    HTTPClient
	userAgent string
}

// Search implements SearchProvider against https://html.duckduckgo.com/html.
func (p *duckduckgoProvider) Search(ctx context.Context, query string, num int) ([]SearchResult, error) {
	endpoint := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web.search: duckduckgo returned status %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("web.search: parse results: %w", err)
	}

	return extractResults(doc, num), nil
}

var resultStrip = regexp.MustCompile(`\s+`)

// extractResults walks the DuckDuckGo HTML result nodes.
func extractResults(n *html.Node, limit int) []SearchResult {
	var results []SearchResult
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "div" && hasClass(node, "result") {
			r := parseResult(node)
			if r.Title != "" {
				results = append(results, r)
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)

	// de-dup by URL, cap at limit
	seen := make(map[string]struct{}, len(results))
	out := make([]SearchResult, 0, limit)
	for _, r := range results {
		if _, ok := seen[r.URL]; ok {
			continue
		}
		seen[r.URL] = struct{}{}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func parseResult(result *html.Node) SearchResult {
	var r SearchResult
	for c := result.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "a" {
			href, ok := getAttr(c, "href")
			if ok {
				r.URL = href
			}
			r.Title = stripText(c)
			break
		}
	}
	if snippetNode := findByClass(result, "result__snippet"); snippetNode != nil {
		r.Snippet = stripText(snippetNode)
	}
	return r
}

func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			fields := strings.Fields(a.Val)
			for _, f := range fields {
				if f == class {
					return true
				}
			}
		}
	}
	return false
}

func findByClass(n *html.Node, class string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && hasClass(n, class) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findByClass(c, class); found != nil {
			return found
		}
	}
	return nil
}

func getAttr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

// stripText returns the text content of a node, collapsing whitespace.
func stripText(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return resultStrip.ReplaceAllString(strings.TrimSpace(sb.String()), " ")
}
