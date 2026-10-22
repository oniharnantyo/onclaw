package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// NameWebSearch is the dotted capability name registered in the tool catalog.
const NameWebSearch = "web_search"

// WebSearchOption configures the web_search tool.
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

// WithSearchProviderResolver defers backend resolution to first invocation:
// the resolver runs once on the first InvokableRun and the outcome is
// memoized, so a configuration error surfaces as an invocation error result
// while the turn completes.
func WithSearchProviderResolver(fn func(ctx context.Context) (SearchProvider, error)) WebSearchOption {
	return func(w *webSearchTool) { w.resolver = fn }
}

// WithSearchConfigSource defers chain construction to first invocation: the
// source is consulted once for the persisted web_search chain config (design
// D8; the tool_settings store adapter is wired by the composition root), the
// chain is built from it, and the outcome — including a failed lookup or an
// unconfigured chain — is memoized on the instance.
func WithSearchConfigSource(src SearchConfigSource) WebSearchOption {
	return func(w *webSearchTool) {
		w.resolver = func(ctx context.Context) (SearchProvider, error) {
			cfg, err := src.WebSearchConfig(ctx)
			if err != nil {
				return nil, fmt.Errorf("web_search: load chain config: %w", err)
			}
			return NewSearchProviderFromConfig(cfg, w.client)
		}
	}
}

// webSearchTool implements the web_search built-in on top of a SearchProvider.
// The backend binds eagerly (WithSearchProvider, the test seam) or lazily
// through the resolver on first invocation; the resolution outcome — including
// a failed resolution — is memoized on the instance (one tool instance per
// turn, so repeat invocations return the identical error cheaply).
type webSearchTool struct {
	provider SearchProvider
	client   HTTPClient

	resolver   func(ctx context.Context) (SearchProvider, error)
	once       sync.Once
	resolved   SearchProvider
	resolveErr error
}

// NewWebSearch constructs the web_search built-in. The backend binds either
// eagerly with WithSearchProvider (testing seam) or lazily through
// WithSearchProviderResolver / WithSearchConfigSource (production: the
// persisted provider chain resolves on first invocation); with neither,
// construction fails — there is no credential-free default backend.
func NewWebSearch(opts ...WebSearchOption) (tool.BaseTool, error) {
	t := &webSearchTool{}
	for _, opt := range opts {
		opt(t)
	}
	if t.provider == nil && t.resolver == nil {
		return nil, errors.New("web_search: no search provider configured")
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
		Name: NameWebSearch,
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

// InvokableRun satisfies tool.InvokableTool. Errors — including the
// not-configured error — return as error results so the turn continues.
func (t *webSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args queryArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}
	if len(args.Query) == 0 {
		return "", errors.New("web_search: query is required")
	}
	num := 5
	if args.Num != nil && *args.Num > 0 {
		num = *args.Num
	}
	if num > 10 {
		num = 10
	}

	provider, err := t.searchProvider(ctx)
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
		return "", fmt.Errorf("web_search: encode results: %w", err)
	}
	return string(out), nil
}

// searchProvider returns the tool's backend: the eager provider when set,
// otherwise the resolver run once on first invocation with the outcome —
// including the error — memoized.
func (t *webSearchTool) searchProvider(ctx context.Context) (SearchProvider, error) {
	if t.provider != nil {
		return t.provider, nil
	}
	t.once.Do(func() {
		t.resolved, t.resolveErr = t.resolver(ctx)
	})
	return t.resolved, t.resolveErr
}
