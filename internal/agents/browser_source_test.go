package agents

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/tools"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// TestStoreBrowserConfigSource covers the tool_settings adapter (spec
// add-browser-tool D9): a missing row is the defaults, a stored row decodes
// with per-key defaults for missing keys (an explicit false stays false),
// and decode failures surface.
func TestStoreBrowserConfigSource(t *testing.T) {
	ctx := t.Context()
	fs := fake.New()
	src := &storeBrowserConfigSource{settings: fs.ToolSettings()}

	t.Run("missing row is the defaults", func(t *testing.T) {
		cfg, err := src.BrowserConfig(ctx)
		require.NoError(t, err)
		require.Equal(t, tools.DefaultBrowserConfig(), cfg)
	})

	t.Run("stored keys win, missing keys keep defaults", func(t *testing.T) {
		require.NoError(t, fs.ToolSettings().Set(ctx, tools.NameBrowser,
			`{"headless":false,"defaultProfile":"work"}`))
		cfg, err := src.BrowserConfig(ctx)
		require.NoError(t, err)
		require.Equal(t, "work", cfg.DefaultProfile)
		require.False(t, cfg.Headless)
		require.Equal(t, tools.DefaultBrowserConfig().IdleTimeoutSeconds, cfg.IdleTimeoutSeconds)
		require.Equal(t, tools.DefaultBrowserConfig().ActionTimeoutSeconds, cfg.ActionTimeoutSeconds)
		require.False(t, cfg.AllowPrivate)
	})

	t.Run("full row decodes every key", func(t *testing.T) {
		require.NoError(t, fs.ToolSettings().Set(ctx, tools.NameBrowser, `{
			"defaultProfile": "research",
			"headless": false,
			"idleTimeoutSeconds": 300,
			"actionTimeoutSeconds": 45,
			"allowPrivate": true
		}`))
		cfg, err := src.BrowserConfig(ctx)
		require.NoError(t, err)
		require.Equal(t, tools.BrowserConfig{
			DefaultProfile:       "research",
			Headless:             false,
			IdleTimeoutSeconds:   300,
			ActionTimeoutSeconds: 45,
			AllowPrivate:         true,
		}, cfg)
	})

	t.Run("malformed row fails", func(t *testing.T) {
		require.NoError(t, fs.ToolSettings().Set(ctx, tools.NameBrowser, `{not json`))
		_, err := src.BrowserConfig(ctx)
		require.ErrorContains(t, err, "decode browser settings")
	})
}
