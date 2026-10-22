package agents

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/tools"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// TestStoreSearchConfigSource covers the tool_settings adapter: a missing
// row is the unconfigured state (empty config — the chain builder raises the
// not-configured error result at invocation), a stored row decodes into the
// chain config, and decode failures surface.
func TestStoreSearchConfigSource(t *testing.T) {
	ctx := t.Context()
	fs := fake.New()
	src := &storeSearchConfigSource{settings: fs.ToolSettings()}

	t.Run("missing row is unconfigured", func(t *testing.T) {
		cfg, err := src.WebSearchConfig(ctx)
		require.NoError(t, err)
		require.Empty(t, cfg.Entries)
		_, err = tools.NewSearchProviderFromConfig(cfg, nil)
		require.ErrorContains(t, err, "not configured")
	})

	t.Run("stored row decodes", func(t *testing.T) {
		require.NoError(t, fs.ToolSettings().Set(ctx, tools.NameWebSearch, `{
			"entries": [
				{"name": "brave first", "provider": "brave", "api_key": "k1"},
				{"provider": "searxng", "base_url": "https://search.example"}
			],
			"request_timeout_seconds": 15
		}`))
		cfg, err := src.WebSearchConfig(ctx)
		require.NoError(t, err)
		require.Len(t, cfg.Entries, 2)
		require.Equal(t, "brave", cfg.Entries[0].Provider)
		require.Equal(t, "k1", cfg.Entries[0].APIKey)
		require.Equal(t, "https://search.example", cfg.Entries[1].BaseURL)
		require.Equal(t, 15, cfg.RequestTimeoutSeconds)

		provider, err := tools.NewSearchProviderFromConfig(cfg, nil)
		require.NoError(t, err)
		require.NotNil(t, provider)
	})

	t.Run("malformed row fails", func(t *testing.T) {
		require.NoError(t, fs.ToolSettings().Set(ctx, tools.NameWebSearch, `{not json`))
		_, err := src.WebSearchConfig(ctx)
		require.ErrorContains(t, err, "decode web_search settings")
	})
}
