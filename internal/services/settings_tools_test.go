package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// TestSettingsTools_ListingCoversCatalog proves the listing always covers the full
// runtime catalog, defaulting unstored tools to enabled.
func TestSettingsTools_ListingCoversCatalog(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	tools, err := svc.Tools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, len(agents.CatalogTools()))
	for i, tool := range tools {
		require.Equal(t, agents.CatalogTools()[i].Name, tool.Name)
		require.True(t, tool.Enabled)
	}
}

// TestSettingsTools_ToggleRoundTrip toggles a tool and observes the listing flip,
// proving persistence through the store.
func TestSettingsTools_ToggleRoundTrip(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	require.NoError(t, svc.SetToolEnabled(ctx, agents.ToolHTTPGet, false))

	tools, err := svc.Tools(ctx)
	require.NoError(t, err)
	var got ToolEntry
	for _, tool := range tools {
		if tool.Name == agents.ToolHTTPGet {
			got = tool
		}
	}
	require.False(t, got.Enabled)

	// Re-enable: the stored row flips back.
	require.NoError(t, svc.SetToolEnabled(ctx, agents.ToolHTTPGet, true))
	tools, err = svc.Tools(ctx)
	require.NoError(t, err)
	for _, tool := range tools {
		if tool.Name == agents.ToolHTTPGet {
			require.True(t, tool.Enabled)
		}
	}
}

// TestSettingsTools_RejectsUnknownTool keeps the pane from inventing tool rows.
func TestSettingsTools_RejectsUnknownTool(t *testing.T) {
	svc := NewSettingsService(fake.New())
	require.ErrorContains(t,
		svc.SetToolEnabled(context.Background(), "shell", false), "unknown tool")
}
