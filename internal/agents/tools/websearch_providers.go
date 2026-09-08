package tools

import (
	"context"
	"net/http"
)

// SearchResult is one organic search hit, provider-independent.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchProvider resolves a query to organic results. Implementations are the
// web.search backend seam: credential-backed API providers registered in
// websearch_registry.go, composed into failover chains by websearch_chain.go.
type SearchProvider interface {
	Search(ctx context.Context, query string, num int) ([]SearchResult, error)
}

// HTTPClient is the seam for the web tools so tests can inject a fake. It
// defaults to http.DefaultClient.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}
