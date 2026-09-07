// Package tools holds the OnClaw built-in tool implementations wired into the
// agent execution Engine via the ToolRegistry. Built-ins ship zero external
// credentials: web.search defaults to DuckDuckGo scraping; Tavily and the
// browser tools opt into credentials or a local browser respectively.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Search provider names selected by ONCLAW_SEARCH_PROVIDER.
const (
	SearchProviderDuckDuckGo = "duckduckgo"
	SearchProviderTavily     = "tavily"
)

// EnvSearchProvider selects the web.search backend ("duckduckgo" default).
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

// WithSearchProvider overrides the search backend (testing seam).
func WithSearchProvider(p SearchProvider) WebSearchOption {
	return func(w *webSearchTool) { w.provider = p }
}

// WithUserAgent overrides the default user-agent header used by the default
// DuckDuckGo provider.
func WithUserAgent(ua string) WebSearchOption {
	return func(w *webSearchTool) {
		if ddg, ok := w.provider.(*duckduckgoProvider); ok {
			ddg.userAgent = ua
		}
	}
}

// webSearchTool implements the web.search built-in on top of a SearchProvider.
type webSearchTool struct {
	provider SearchProvider
	client   HTTPClient
}

// Name is the dotted capability name registered in the tool registry.
const Name = "web.search"

// NewWebSearch constructs the web.search built-in with the DuckDuckGo provider
// (zero credentials). Pass WithSearchProvider to select a different backend.
func NewWebSearch(opts ...WebSearchOption) (tool.BaseTool, error) {
	t := &webSearchTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.provider == nil {
		t.provider = &duckduckgoProvider{
			client:    orDefaultClient(t.client),
			userAgent: defaultUserAgent,
		}
	}
	return t, nil
}

// NewSearchProvider resolves a provider by instance configuration name:
// "tavily" requires an API key, "duckduckgo" (the default and the zero-value)
// requires nothing. Unknown names are an error.
func NewSearchProvider(name, apiKey string, client HTTPClient) (SearchProvider, error) {
	c := orDefaultClient(client)
	switch name {
	case "", SearchProviderDuckDuckGo:
		return &duckduckgoProvider{client: c, userAgent: defaultUserAgent}, nil
	case SearchProviderTavily:
		if apiKey == "" {
			return nil, errors.New("web.search: tavily provider requires ONCLAW_TAVILY_API_KEY")
		}
		return &tavilyProvider{client: c, apiKey: apiKey}, nil
	default:
		return nil, fmt.Errorf("web.search: unknown search provider %q (want duckduckgo or tavily)", name)
	}
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

	results, err := t.provider.Search(ctx, args.Query, num)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tavilyAPIEndpoint, bytes.NewReader(payload))
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
		return nil, fmt.Errorf("web.search: tavily returned status %d", resp.StatusCode)
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
