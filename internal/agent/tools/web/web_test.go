package web_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	_ "github.com/oniharnantyo/onclaw/internal/agent/tools/web"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	sysweb "github.com/oniharnantyo/onclaw/internal/web"
)

type dummyToolGroupCfg struct {
	config string
}

func (d *dummyToolGroupCfg) GetConfig(ctx context.Context, category string) (string, error) {
	return d.config, nil
}

func TestWebToolsRegistration(t *testing.T) {
	if !tools.IsConfigurable("Web") {
		t.Error("expected Web category to be configurable")
	}

	entry, ok := tools.GetConfigEntry("Web")
	if !ok {
		t.Fatal("expected Web config entry to exist")
	}
	if entry.JSONSchema == "" {
		t.Error("expected non-empty JSON schema for Web category")
	}

	registeredTools := tools.GetRegistry()
	var webTools []tools.Tool
	for _, tl := range registeredTools {
		if tl.Category() == "Web" {
			webTools = append(webTools, tl)
		}
	}

	if len(webTools) != 2 {
		t.Errorf("expected 2 web tools registered, got %d", len(webTools))
	}

	names := map[string]bool{
		"web_search": true,
		"web_fetch":  true,
	}
	for _, tl := range webTools {
		delete(names, tl.Name())
	}
	if len(names) > 0 {
		t.Errorf("missing web tools: %v", names)
	}
}

func TestWebSearchTool_Fallback(t *testing.T) {
	// Register mock searchers
	origTavily, _ := sysweb.LookupSearcher("tavily")
	origDDG, _ := sysweb.LookupSearcher("duckduckgo")

	sysweb.RegisterSearcher("tavily", func(cfg sysweb.Config, resolver secrets.SecretResolver) (sysweb.Searcher, error) {
		return nil, errors.New("tavily build error")
	})

	ddgCalled := false
	sysweb.RegisterSearcher("duckduckgo", func(cfg sysweb.Config, resolver secrets.SecretResolver) (sysweb.Searcher, error) {
		return roundTripSearcher(func(ctx context.Context, query string, limit int) ([]sysweb.SearchResult, error) {
			ddgCalled = true
			return []sysweb.SearchResult{
				{Title: "DDG Title", URL: "http://ddg.com", Snippet: "DDG Snippet"},
			}, nil
		}), nil
	})

	defer func() {
		if origTavily != nil {
			sysweb.RegisterSearcher("tavily", origTavily)
		}
		if origDDG != nil {
			sysweb.RegisterSearcher("duckduckgo", origDDG)
		}
	}()

	var searchTool tools.Tool
	for _, tl := range tools.GetRegistry() {
		if tl.Name() == "web_search" {
			searchTool = tl
			break
		}
	}
	if searchTool == nil {
		t.Fatal("web_search tool not registered")
	}

	scope := &tools.Scope{
		Workspace: "test_ws",
		ToolGroupCfg: &dummyToolGroupCfg{
			config: `{"search_provider":"tavily"}`,
		},
	}

	invokable := searchTool.Build(scope)
	res, err := invokable.InvokableRun(context.Background(), `{"query": "golang"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ddgCalled {
		t.Error("expected duckduckgo fallback to be called")
	}

	if !strings.Contains(res, "Note: Falling back to DuckDuckGo search") {
		t.Errorf("expected output to contain fallback notice, got %q", res)
	}

	if !strings.Contains(res, "Title: DDG Title") {
		t.Errorf("expected output to contain search result, got %q", res)
	}
}

func TestWebFetchTool_Fallback(t *testing.T) {
	origLP, _ := sysweb.LookupFetcher("lightpanda")
	origHTTP, _ := sysweb.LookupFetcher("http")

	sysweb.RegisterFetcher("lightpanda", func(cfg sysweb.Config, resolver secrets.SecretResolver) (sysweb.Fetcher, error) {
		return nil, errors.New("lightpanda build error")
	})

	httpCalled := false
	sysweb.RegisterFetcher("http", func(cfg sysweb.Config, resolver secrets.SecretResolver) (sysweb.Fetcher, error) {
		return roundTripFetcher(func(ctx context.Context, url string, headers map[string]string) (sysweb.FetchResult, error) {
			httpCalled = true
			return sysweb.FetchResult{Content: "HTTP Content"}, nil
		}), nil
	})

	defer func() {
		if origLP != nil {
			sysweb.RegisterFetcher("lightpanda", origLP)
		}
		if origHTTP != nil {
			sysweb.RegisterFetcher("http", origHTTP)
		}
	}()

	var fetchTool tools.Tool
	for _, tl := range tools.GetRegistry() {
		if tl.Name() == "web_fetch" {
			fetchTool = tl
			break
		}
	}
	if fetchTool == nil {
		t.Fatal("web_fetch tool not registered")
	}

	scope := &tools.Scope{
		Workspace: "test_ws",
		ToolGroupCfg: &dummyToolGroupCfg{
			config: `{"fetch_provider":"lightpanda"}`,
		},
	}

	invokable := fetchTool.Build(scope)
	res, err := invokable.InvokableRun(context.Background(), `{"url": "http://example.com"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !httpCalled {
		t.Error("expected http fallback to be called")
	}

	if !strings.Contains(res, "Note: Falling back to standard HTTP fetch") {
		t.Errorf("expected output to contain fallback notice, got %q", res)
	}

	if !strings.Contains(res, "HTTP Content") {
		t.Errorf("expected output to contain content, got %q", res)
	}
}

type roundTripSearcher func(ctx context.Context, query string, limit int) ([]sysweb.SearchResult, error)

func (f roundTripSearcher) Search(ctx context.Context, query string, limit int) ([]sysweb.SearchResult, error) {
	return f(ctx, query, limit)
}

type roundTripFetcher func(ctx context.Context, url string, headers map[string]string) (sysweb.FetchResult, error)

func (f roundTripFetcher) Fetch(ctx context.Context, url string, headers map[string]string) (sysweb.FetchResult, error) {
	return f(ctx, url, headers)
}
