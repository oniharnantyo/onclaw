package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
)

// SearchProviderKind describes the credential kind a search provider requires.
type SearchProviderKind string

const (
	// SearchCredentialAPIKey marks providers that require an API key.
	SearchCredentialAPIKey SearchProviderKind = "api_key"
	// SearchCredentialBaseURL marks providers that require an instance base URL.
	SearchCredentialBaseURL SearchProviderKind = "base_url"
)

// SearchProviderInfo describes one registered search provider.
type SearchProviderInfo struct {
	// ID is the provider identifier used in workspace tool settings and env.
	ID string
	// Label is the human-readable name shown in settings.
	Label string
	// Credential is the kind of credential the provider requires.
	Credential SearchProviderKind
}

// searchProviderRegistry is the first-cut provider table (design.md D7):
// each entry is a thin request/parse pair modeled on the tavily client.
var searchProviderRegistry = []SearchProviderInfo{
	{ID: SearchProviderTavily, Label: "Tavily", Credential: SearchCredentialAPIKey},
	{ID: SearchProviderBrave, Label: "Brave", Credential: SearchCredentialAPIKey},
	{ID: SearchProviderExa, Label: "Exa", Credential: SearchCredentialAPIKey},
	{ID: SearchProviderPerplexity, Label: "Perplexity", Credential: SearchCredentialAPIKey},
	{ID: SearchProviderFirecrawl, Label: "Firecrawl", Credential: SearchCredentialAPIKey},
	{ID: SearchProviderSearXNG, Label: "SearXNG", Credential: SearchCredentialBaseURL},
}

// Search provider identifiers used in registry lookups and config values.
const (
	SearchProviderBrave      = "brave"
	SearchProviderExa        = "exa"
	SearchProviderPerplexity = "perplexity"
	SearchProviderFirecrawl  = "firecrawl"
	SearchProviderSearXNG    = "searxng"
)

// SearchProviders returns the registered search providers ordered by ID.
func SearchProviders() []SearchProviderInfo {
	out := make([]SearchProviderInfo, len(searchProviderRegistry))
	copy(out, searchProviderRegistry)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SearchProviderInfoFor returns registry metadata for a provider ID.
func SearchProviderInfoFor(id string) (SearchProviderInfo, bool) {
	for _, info := range searchProviderRegistry {
		if info.ID == id {
			return info, true
		}
	}
	return SearchProviderInfo{}, false
}

// NewSearchProviderByCredential constructs the named provider from its
// credential (API key or base URL). Providers whose credential kind is
// unsatisfied return an error naming the provider — callers surface this as
// the "configured tool failed to construct" path. The client defaults to
// http.DefaultClient when nil; callers pass a client with a Timeout to bound
// each attempt (design D7).
func NewSearchProviderByCredential(id, credential string, client HTTPClient) (SearchProvider, error) {
	info, ok := SearchProviderInfoFor(id)
	if !ok {
		return nil, fmt.Errorf("web.search: unknown search provider %q", id)
	}
	c := orDefaultClient(client)
	switch info.Credential {
	case SearchCredentialAPIKey:
		if credential == "" {
			return nil, fmt.Errorf("web.search: %s provider requires an API key", id)
		}
		return newSearchProviderConstructor(id)(c, credential)
	case SearchCredentialBaseURL:
		if credential == "" {
			return nil, fmt.Errorf("web.search: %s provider requires a base URL", id)
		}
		return newSearchProviderConstructor(id)(c, credential)
	default:
		return nil, fmt.Errorf("web.search: provider %q has an unknown credential kind", id)
	}
}

// newSearchProviderConstructor maps a provider ID to its thin client
// constructor; the credential is the API key or base URL per registry kind.
func newSearchProviderConstructor(id string) func(HTTPClient, string) (SearchProvider, error) {
	switch id {
	case SearchProviderTavily:
		return func(c HTTPClient, credential string) (SearchProvider, error) {
			return &tavilyProvider{client: c, apiKey: credential}, nil
		}
	case SearchProviderBrave:
		return func(c HTTPClient, credential string) (SearchProvider, error) {
			return &braveProvider{client: c, apiKey: credential}, nil
		}
	case SearchProviderExa:
		return func(c HTTPClient, credential string) (SearchProvider, error) {
			return &exaProvider{client: c, apiKey: credential}, nil
		}
	case SearchProviderPerplexity:
		return func(c HTTPClient, credential string) (SearchProvider, error) {
			return &perplexityProvider{client: c, apiKey: credential}, nil
		}
	case SearchProviderFirecrawl:
		return func(c HTTPClient, credential string) (SearchProvider, error) {
			return &firecrawlProvider{client: c, apiKey: credential}, nil
		}
	case SearchProviderSearXNG:
		return func(c HTTPClient, credential string) (SearchProvider, error) {
			return &searxngProvider{client: c, baseURL: credential}, nil
		}
	default:
		return nil
	}
}

// braveProvider calls Brave Search's web search API.
type braveProvider struct {
	client HTTPClient
	apiKey string
}

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

func (p *braveProvider) Search(ctx context.Context, query string, num int) ([]SearchResult, error) {
	endpoint := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d", url.QueryEscape(query), num)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req.Header.Set("X-Subscription-Token", p.apiKey)
	req.Header.Set("Accept", "application/json")

	var body braveResponse
	if _, err := doJSONSearch(ctx, p.client, req, &body, "brave"); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(body.Web.Results))
	for _, r := range body.Web.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Description})
	}
	return out, nil
}

// exaProvider calls Exa's /search API.
type exaProvider struct {
	client HTTPClient
	apiKey string
}

type exaRequest struct {
	Query      string `json:"query"`
	NumResults int    `json:"numResults"`
}

type exaResponse struct {
	Results []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Text  string `json:"text"`
	} `json:"results"`
}

func (p *exaProvider) Search(ctx context.Context, query string, num int) ([]SearchResult, error) {
	payload, err := json.Marshal(exaRequest{Query: query, NumResults: num})
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.exa.ai/search", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	var body exaResponse
	if _, err := doJSONSearch(ctx, p.client, req, &body, "exa"); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(body.Results))
	for _, r := range body.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Text})
	}
	return out, nil
}

// perplexityProvider calls Perplexity's chat completions API. The answer text
// becomes one result and each citation URL an additional entry.
type perplexityProvider struct {
	client HTTPClient
	apiKey string
}

type perplexityRequest struct {
	Model    string                   `json:"model"`
	Messages []map[string]interface{} `json:"messages"`
}

type perplexityResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Citations []string `json:"citations"`
}

func (p *perplexityProvider) Search(ctx context.Context, query string, num int) ([]SearchResult, error) {
	payload, err := json.Marshal(perplexityRequest{
		Model:    "sonar",
		Messages: []map[string]interface{}{{"role": "user", "content": query}},
	})
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.perplexity.ai/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	var body perplexityResponse
	if _, err := doJSONSearch(ctx, p.client, req, &body, "perplexity"); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, 1+len(body.Citations))
	for _, choice := range body.Choices {
		out = append(out, SearchResult{Title: query, Snippet: choice.Message.Content})
	}
	for _, citation := range body.Citations {
		out = append(out, SearchResult{Title: citation, URL: citation})
	}
	return out, nil
}

// firecrawlProvider calls Firecrawl's /v1/search API.
type firecrawlProvider struct {
	client HTTPClient
	apiKey string
}

type firecrawlRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type firecrawlResponse struct {
	Data []struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Description string `json:"description"`
	} `json:"data"`
}

func (p *firecrawlProvider) Search(ctx context.Context, query string, num int) ([]SearchResult, error) {
	payload, err := json.Marshal(firecrawlRequest{Query: query, Limit: num})
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.firecrawl.dev/v1/search", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	var body firecrawlResponse
	if _, err := doJSONSearch(ctx, p.client, req, &body, "firecrawl"); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(body.Data))
	for _, r := range body.Data {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Description})
	}
	return out, nil
}

// searxngProvider queries a self-hosted SearXNG instance's JSON endpoint.
type searxngProvider struct {
	client  HTTPClient
	baseURL string
}

type searxngResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func (p *searxngProvider) Search(ctx context.Context, query string, num int) ([]SearchResult, error) {
	endpoint := fmt.Sprintf("%s/search?q=%s&format=json", p.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	var body searxngResponse
	if _, err := doJSONSearch(ctx, p.client, req, &body, "searxng"); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(body.Results))
	for _, r := range body.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Content})
	}
	if len(out) > num {
		out = out[:num]
	}
	return out, nil
}

// doJSONSearch executes a JSON API request and decodes the response body.
// Non-200 statuses return a *providerStatusError (rendered as
// "web.search: <name> returned status <code>") so the chain provider can
// re-name the error after the failing entry; decode failures return a
// "web.search:"-prefixed error.
func doJSONSearch(ctx context.Context, client HTTPClient, req *http.Request, body any, name string) (any, error) {
	c := client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web.search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &providerStatusError{name: name, code: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(body); err != nil {
		return nil, fmt.Errorf("web.search: decode %s response: %w", name, err)
	}
	return body, nil
}
