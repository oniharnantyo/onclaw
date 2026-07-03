package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	sysweb "github.com/oniharnantyo/onclaw/internal/web"
)

func init() {
	tools.Register(&webSearchTool{})
}

type webSearchTool struct{}

func (t *webSearchTool) Name() string {
	return "web_search"
}

func (t *webSearchTool) Desc() string {
	return "Search the web for queries and return titles, URLs, and snippets of results"
}

func (t *webSearchTool) Category() string {
	return "Web"
}

type SearchInput struct {
	Query string `json:"query" jsonschema_description:"The search query string"`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Optional maximum number of results to return"`
}

func (t *webSearchTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *SearchInput) (string, error) {
		var rawCfg string
		var err error
		if scope.ToolGroupCfg != nil {
			rawCfg, err = scope.ToolGroupCfg.GetConfig(ctx, "Web")
		}

		cfg, err := sysweb.ParseConfig(rawCfg)
		if err != nil {
			// Degrading gracefully to defaults
			cfg, _ = sysweb.ParseConfig("")
		}

		var results []sysweb.SearchResult
		var originalErr error
		var notice string

		// Attempt preferred search provider
		factory, ok := sysweb.LookupSearcher(cfg.SearchProvider)
		if !ok {
			originalErr = fmt.Errorf("search provider %q is not registered", cfg.SearchProvider)
		} else {
			searcher, buildErr := factory(cfg, scope.SecretResolver)
			if buildErr != nil {
				originalErr = fmt.Errorf("failed to build provider %q: %w", cfg.SearchProvider, buildErr)
			} else {
				results, err = searcher.Search(ctx, input.Query, input.Limit)
				if err != nil {
					originalErr = fmt.Errorf("provider %q search failed: %w", cfg.SearchProvider, err)
				}
			}
		}

		// Fallback to DuckDuckGo if preferred failed
		if originalErr != nil && cfg.SearchProvider != "duckduckgo" {
			notice = fmt.Sprintf("Note: Falling back to DuckDuckGo search because configured provider %q failed or was unavailable: %v\n\n", cfg.SearchProvider, originalErr)

			fallbackFactory, _ := sysweb.LookupSearcher("duckduckgo")
			fallbackSearcher, _ := fallbackFactory(cfg, scope.SecretResolver)
			results, err = fallbackSearcher.Search(ctx, input.Query, input.Limit)
			if err != nil {
				return "", fmt.Errorf("fallback DuckDuckGo search also failed: %w", err)
			}
		} else if originalErr != nil {
			// DuckDuckGo was preferred and failed
			return "", originalErr
		}

		var sb strings.Builder
		if notice != "" {
			sb.WriteString(notice)
		}

		if len(results) == 0 {
			sb.WriteString("No results found.")
		} else {
			for i, r := range results {
				if i > 0 {
					sb.WriteString("\n---\n\n")
				}
				sb.WriteString(fmt.Sprintf("Title: %s\nURL: %s\nSnippet: %s\n", r.Title, r.URL, r.Snippet))
			}
		}

		return sb.String(), nil
	})

	if err != nil {
		panic(err)
	}
	return inv
}
