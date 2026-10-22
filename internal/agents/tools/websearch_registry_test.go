package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSearchProviders_Catalog(t *testing.T) {
	providers := SearchProviders()
	require.Len(t, providers, 5)

	ids := make([]string, 0, len(providers))
	for _, p := range providers {
		ids = append(ids, p.ID)
	}
	require.Equal(t, []string{"brave", "exa", "firecrawl", "perplexity", "searxng"}, ids,
		"registry must be the five specced providers ordered by ID")
}

func TestSearchProviderInfoFor(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		known      bool
		credential SearchProviderKind
	}{
		{name: "brave needs api key", id: "brave", known: true, credential: SearchCredentialAPIKey},
		{name: "exa needs api key", id: "exa", known: true, credential: SearchCredentialAPIKey},
		{name: "perplexity needs api key", id: "perplexity", known: true, credential: SearchCredentialAPIKey},
		{name: "firecrawl needs api key", id: "firecrawl", known: true, credential: SearchCredentialAPIKey},
		{name: "searxng needs base url", id: "searxng", known: true, credential: SearchCredentialBaseURL},
		{name: "unknown provider", id: "bogus", known: false},
		{name: "removed tavily", id: "tavily", known: false},
		{name: "removed duckduckgo", id: "duckduckgo", known: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := SearchProviderInfoFor(tt.id)
			require.Equal(t, tt.known, ok)
			if tt.known {
				require.Equal(t, tt.credential, info.Credential)
			}
		})
	}
}

func TestNewSearchProviderByCredential(t *testing.T) {
	t.Run("nil client falls back to internal default", func(t *testing.T) {
		p, err := NewSearchProviderByCredential("brave", "bk-key", nil)
		require.NoError(t, err)
		require.NotNil(t, p)
	})
	t.Run("missing api key", func(t *testing.T) {
		_, err := NewSearchProviderByCredential("brave", "", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "brave provider requires an API key")
	})
	t.Run("missing base url", func(t *testing.T) {
		_, err := NewSearchProviderByCredential("searxng", "", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "searxng provider requires a base URL")
	})
	t.Run("unknown provider", func(t *testing.T) {
		_, err := NewSearchProviderByCredential("bogus", "k", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), `unknown search provider "bogus"`)
	})
	t.Run("removed providers are unregistered", func(t *testing.T) {
		for _, id := range []string{"tavily", "duckduckgo"} {
			_, err := NewSearchProviderByCredential(id, "k", nil)
			require.Error(t, err, "%s must not be registered", id)
		}
	})
}

func TestNewSearchProviderFromConfig_NotConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  WebSearchConfig
	}{
		{name: "zero config", cfg: WebSearchConfig{}},
		{name: "empty entries", cfg: WebSearchConfig{Entries: []WebSearchChainEntry{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSearchProviderFromConfig(tt.cfg, nil)
			require.ErrorIs(t, err, errWebSearchNotConfigured)
		})
	}
}

func TestNewSearchProviderFromConfig_EntryErrors(t *testing.T) {
	t.Run("unknown provider", func(t *testing.T) {
		cfg := WebSearchConfig{Entries: []WebSearchChainEntry{{Name: "Bogus", Provider: "bogus", APIKey: "k"}}}
		_, err := NewSearchProviderFromConfig(cfg, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), `unknown search provider "bogus"`)
	})
	t.Run("api key provider without key", func(t *testing.T) {
		cfg := WebSearchConfig{Entries: []WebSearchChainEntry{{Name: "Brave", Provider: SearchProviderBrave}}}
		_, err := NewSearchProviderFromConfig(cfg, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "brave provider requires an API key")
	})
	t.Run("base url provider without url", func(t *testing.T) {
		cfg := WebSearchConfig{Entries: []WebSearchChainEntry{{Name: "SearXNG", Provider: SearchProviderSearXNG}}}
		_, err := NewSearchProviderFromConfig(cfg, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "searxng provider requires a base URL")
	})
}

func TestNewSearchProviderFromConfig_ServesEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"title":"Config","url":"https://cfg.example","content":"via config"}]}`)
	}))
	defer srv.Close()

	cfg := WebSearchConfig{
		Entries: []WebSearchChainEntry{
			{Name: "Local SearXNG", Provider: SearchProviderSearXNG, BaseURL: srv.URL},
		},
		RequestTimeoutSeconds: 5,
	}
	chain, err := NewSearchProviderFromConfig(cfg, nil)
	require.NoError(t, err)

	results, err := chain.Search(context.Background(), "q", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "https://cfg.example", results[0].URL)
}

func TestNewSearchProviderFromConfig_WindowCutAtThree(t *testing.T) {
	// Every entry fails with 500; the chain must stop at the third entry, so
	// the last error names entry 3 — entry 4 never serves (failover window).
	failing := &searchFakeClient{status: http.StatusInternalServerError, body: `{}`}
	cfg := WebSearchConfig{Entries: []WebSearchChainEntry{
		{Name: "One", Provider: SearchProviderBrave, APIKey: "k"},
		{Name: "Two", Provider: SearchProviderExa, APIKey: "k"},
		{Name: "Three", Provider: SearchProviderPerplexity, APIKey: "k"},
		{Name: "Four", Provider: SearchProviderFirecrawl, APIKey: "k"},
	}}
	chain, err := NewSearchProviderFromConfig(cfg, failing)
	require.NoError(t, err)

	_, err = chain.Search(context.Background(), "q", 5)
	require.Error(t, err)
	require.EqualError(t, err, "web_search: Three returned status 500")
}

func TestNewSearchProviderFromConfig_UnnamedEntryFallsBackToProviderID(t *testing.T) {
	failing := &searchFakeClient{status: http.StatusForbidden, body: `{}`}
	cfg := WebSearchConfig{Entries: []WebSearchChainEntry{
		{Provider: SearchProviderBrave, APIKey: "k"},
	}}
	chain, err := NewSearchProviderFromConfig(cfg, failing)
	require.NoError(t, err)

	_, err = chain.Search(context.Background(), "q", 5)
	require.Error(t, err)
	require.EqualError(t, err, "web_search: brave returned status 403")
}

func TestWebSearchAttemptTimeout(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "zero falls back to default", seconds: 0, want: 10 * time.Second},
		{name: "negative falls back to default", seconds: -5, want: 10 * time.Second},
		{name: "in-range passes through", seconds: 30, want: 30 * time.Second},
		{name: "above max clamps", seconds: 120, want: 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, webSearchAttemptTimeout(tt.seconds))
		})
	}
}

func TestWebSearchConfig_JSONRoundTrip(t *testing.T) {
	raw := `{"entries":[{"id":"ab12cd34","name":"Brave 1","provider":"brave","api_key":"bk"},` +
		`{"name":"Local","provider":"searxng","base_url":"http://searxng:8080"}],"request_timeout_seconds":15}`
	var cfg WebSearchConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
	require.Len(t, cfg.Entries, 2)
	require.Equal(t, "brave", cfg.Entries[0].Provider)
	require.Equal(t, "bk", cfg.Entries[0].APIKey)
	require.Equal(t, "searxng", cfg.Entries[1].Provider)
	require.Equal(t, "http://searxng:8080", cfg.Entries[1].BaseURL)
	require.Equal(t, 15, cfg.RequestTimeoutSeconds)

	encoded, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"request_timeout_seconds":15`)
	require.Contains(t, string(encoded), `"api_key":"bk"`)
	require.Contains(t, string(encoded), `"base_url":"http://searxng:8080"`)
}
