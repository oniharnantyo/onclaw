// Package tools holds the OnClaw built-in tool implementations wired into the
// agent execution Engine via the ToolRegistry. web.search is composed from
// API-backed providers (Tavily, Brave, Exa, Perplexity, Firecrawl, SearXNG)
// configured per workspace; there is no credential-free default — the tool
// always builds, and a not-configured / unknown-provider / missing-credential
// error surfaces at invocation as an error result while the run completes.
// The browser tools opt into a local browser.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Search provider names selected by ONCLAW_SEARCH_PROVIDER.
const (
	// SearchProviderDuckDuckGo is retained only so configuration handling can
	// recognize the removed DuckDuckGo backend (treated as unset); no provider
	// is registered under it anymore.
	SearchProviderDuckDuckGo = "duckduckgo"
	SearchProviderTavily     = "tavily"
)

// EnvSearchProvider optionally names a fallback search provider (env seeding;
// there is no default backend).
const EnvSearchProvider = "ONCLAW_SEARCH_PROVIDER"

// EnvTavilyAPIKey carries the Tavily API key when the tavily provider is selected.
const EnvTavilyAPIKey = "ONCLAW_TAVILY_API_KEY"

// tavilyAPIEndpoint is Tavily's search API.
const tavilyAPIEndpoint = "https://api.tavily.com/search"

// WebSearchOption configures the web.search tool.
type WebSearchOption func(*webSearchTool)

// WithHTTPClient overrides the default HTTP client (testing seam).
func WithHTTPClient(c HTTPClient) WebSearchOption {
	return func(w *webSearchTool) { w.client = c }
}

// WithSearchProvider overrides the search backend (testing seam). When set it
// wins outright — the lazy resolver never runs.
func WithSearchProvider(p SearchProvider) WebSearchOption {
	return func(w *webSearchTool) { w.provider = p }
}

// WithSearchProviderResolver defers backend resolution to first invocation
// (design.md D1): the resolver runs once on the first InvokableRun and the
// outcome is memoized, so a configuration error surfaces as an invocation
// error result while the run completes.
func WithSearchProviderResolver(fn func() (SearchProvider, error)) WebSearchOption {
	return func(w *webSearchTool) { w.resolver = fn }
}

// webSearchTool implements the web.search built-in on top of a SearchProvider.
// The backend binds eagerly (WithSearchProvider, the test seam) or lazily
// through the resolver on first invocation; the resolution outcome — including
// a failed resolution — is memoized on the instance (design.md D2: one tool
// instance per execution, so repeat invocations return the identical error
// cheaply).
type webSearchTool struct {
	provider SearchProvider
	client   HTTPClient

	resolver   func() (SearchProvider, error)
	once       sync.Once
	resolved   SearchProvider
	resolveErr error
}

// Name is the dotted capability name registered in the tool registry.
const Name = "web.search"

// NewWebSearch constructs the web.search built-in. The backend binds either
// eagerly with WithSearchProvider (testing seam) or lazily through
// WithSearchProviderResolver (production: the workspace provider chain
// resolves on first invocation); with neither, construction fails — there is
// no credential-free default backend.
func NewWebSearch(opts ...WebSearchOption) (tool.BaseTool, error) {
	t := &webSearchTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.provider == nil && t.resolver == nil {
		return nil, errors.New("web.search: no search provider configured")
	}
	return t, nil
}

func orDefaultClient(c HTTPClient) HTTPClient {
	if c == nil {
		return http.DefaultClient
	}
	return c
}

// Info returns the tool schema surfaced to agentic models.
func (t *webSearchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: Name,
		Desc: "Search the web and return the top organic results. Use this for current events, facts, or when the user's request requires up-to-date information from the public internet.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query (one to five sentences of keywords).",
				Required: true,
			},
			"num": {
				Type: schema.Integer,
				Desc: "Number of results to return (1-10). Defaults to 5.",
			},
		}),
	}, nil
}

// queryArgs is the deserialized tool-call argument shape.
type queryArgs struct {
	Query string `json:"query"`
	Num   *int   `json:"num,omitempty"`
}

// InvokableRun satisfies tool.InvokableTool.
func (t *webSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args queryArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("web.search: %w", err)
	}
	if len(args.Query) == 0 {
		return "", errors.New("web.search: query is required")
	}
	num := 5
	if args.Num != nil && *args.Num > 0 {
		num = *args.Num
	}
	if num > 10 {
		num = 10
	}

	provider, err := t.searchProvider()
	if err != nil {
		return "", err
	}

	results, err := provider.Search(ctx, args.Query, num)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return `{"results": []}`, nil
	}
	out, err := json.Marshal(map[string][]SearchResult{"results": results})
	if err != nil {
		return "", fmt.Errorf("web.search: encode results: %w", err)
	}
	return string(out), nil
}

// searchProvider returns the tool's backend: the eager provider when set,
// otherwise the resolver run once on first invocation with the outcome —
// including the error — memoized (design.md D2).
func (t *webSearchTool) searchProvider() (SearchProvider, error) {
	if t.provider != nil {
		return t.provider, nil
	}
	t.once.Do(func() {
		t.resolved, t.resolveErr = t.resolver()
	})
	return t.resolved, t.resolveErr
}

// Timeout returns an option setting a per-call HTTP timeout.
func Timeout(d time.Duration) WebSearchOption {
	return func(w *webSearchTool) {
		if w.client == nil {
			w.client = http.DefaultClient
		}
		if c, ok := w.client.(*http.Client); ok {
			c.Timeout = d
		}
	}
}

// tavilyProvider calls Tavily's search API.
type tavilyProvider struct {
	client HTTPClient
	apiKey string
	// endpoint overrides tavilyAPIEndpoint when set (httptest seam).
	endpoint string
}

type tavilyRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

type tavilyResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

// Search implements SearchProvider against api.tavily.com.
func (p *tavilyProvider) Search(ctx context.Context, query string, num int) ([]SearchResult, error) {
	payload, err := json.Marshal(tavilyRequest{Query: query, MaxResults: num})
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	endpoint := p.endpoint
	if endpoint == "" {
		endpoint = tavilyAPIEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &providerStatusError{name: "tavily", code: resp.StatusCode}
	}

	var body tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("web.search: decode tavily response: %w", err)
	}

	out := make([]SearchResult, 0, len(body.Results))
	for _, r := range body.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Content})
	}
	return out, nil
}
