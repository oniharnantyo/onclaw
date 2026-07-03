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
	tools.Register(&webFetchTool{})
}

type webFetchTool struct{}

func (t *webFetchTool) Name() string {
	return "web_fetch"
}

func (t *webFetchTool) Desc() string {
	return "Fetch the page content of a public URL (supports custom headers)"
}

func (t *webFetchTool) Category() string {
	return "Web"
}

type FetchInput struct {
	URL     string            `json:"url" jsonschema_description:"The http(s) URL to fetch"`
	Headers map[string]string `json:"headers,omitempty" jsonschema_description:"Optional HTTP headers to send"`
}

func (t *webFetchTool) Build(scope *tools.Scope) tool.InvokableTool {
	inv, err := utils.InferTool(t.Name(), t.Desc(), func(ctx context.Context, input *FetchInput) (string, error) {
		var rawCfg string
		var err error
		if scope.ToolGroupCfg != nil {
			rawCfg, err = scope.ToolGroupCfg.GetConfig(ctx, "Web")
		}

		cfg, err := sysweb.ParseConfig(rawCfg)
		if err != nil {
			cfg, _ = sysweb.ParseConfig("")
		}

		var result sysweb.FetchResult
		var originalErr error
		var notice string

		// Attempt preferred fetch provider
		factory, ok := sysweb.LookupFetcher(cfg.FetchProvider)
		if !ok {
			originalErr = fmt.Errorf("fetch provider %q is not registered", cfg.FetchProvider)
		} else {
			fetcher, buildErr := factory(cfg, scope.SecretResolver)
			if buildErr != nil {
				originalErr = fmt.Errorf("failed to build provider %q: %w", cfg.FetchProvider, buildErr)
			} else {
				result, err = fetcher.Fetch(ctx, input.URL, input.Headers)
				if err != nil {
					originalErr = fmt.Errorf("provider %q fetch failed: %w", cfg.FetchProvider, err)
				}
			}
		}

		// Fallback to http if preferred failed
		if originalErr != nil && cfg.FetchProvider != "http" {
			notice = fmt.Sprintf("Note: Falling back to standard HTTP fetch because configured provider %q failed or was unavailable: %v\n\n", cfg.FetchProvider, originalErr)

			fallbackFactory, _ := sysweb.LookupFetcher("http")
			fallbackFetcher, _ := fallbackFactory(cfg, scope.SecretResolver)
			result, err = fallbackFetcher.Fetch(ctx, input.URL, input.Headers)
			if err != nil {
				return "", fmt.Errorf("fallback HTTP fetch also failed: %w", err)
			}
		} else if originalErr != nil {
			return "", originalErr
		}

		var sb strings.Builder
		if notice != "" {
			sb.WriteString(notice)
		}
		sb.WriteString(result.Content)

		return sb.String(), nil
	})

	if err != nil {
		panic(err)
	}
	return inv
}
